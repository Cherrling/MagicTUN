package node

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/example/magictun/config"
	"github.com/example/magictun/forward"
	"github.com/example/magictun/gossip"
	"github.com/example/magictun/identity"
	"github.com/example/magictun/route"
	"github.com/example/magictun/socks5"
	"github.com/example/magictun/transport"
	"github.com/example/magictun/wire"
)

// Node is the top-level orchestrator that wires together all subsystems.
type Node struct {
	cfg      *config.Config
	identity *identity.Identity

	transportLn *transport.Listener
	tlsCfg      *tls.Config

	peers     *gossip.PeerManager
	gossipEng *gossip.Engine

	routes     *route.RoutingTable
	propagator *route.Propagator

	tcpRelay *forward.TCPRelay
	udpRelay *forward.UDPRelay
	sessions *forward.SessionTable

	socks5Srv *socks5.Server

	// UDP relay
	udpLn      *net.UDPConn
	peerUDPAddrs   map[string]*net.UDPAddr // nodeID -> UDP address
	peerUDPAddrsMu sync.Mutex

	// Peer connections: nodeID -> transport.Conn
	peerConns   map[string]*transport.Conn
	peerConnsMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new Node from a configuration.
func New(cfg *config.Config) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Load or create identity
	id, err := identity.LoadOrCreate(cfg.Node.IdentityFile)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("identity: %w", err)
	}
	log.Printf("node: identity loaded, id=%s", id.ID)

	// Routing table
	routes := route.NewRoutingTable(id.ID)
	var directNets []net.IPNet
	for _, nw := range cfg.Routing.DirectNetworks {
		_, parsed, err := net.ParseCIDR(nw)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("parse direct_network %q: %w", nw, err)
		}
		directNets = append(directNets, *parsed)
	}
	routes.SetDirectNetworks(directNets)

	// Gossip engine
	gossipCfg := gossip.Config{
		PushInterval:  cfg.PushInterval(),
		ProbeInterval: cfg.ProbeInterval(),
		PeerTimeout:   cfg.PeerTimeout(),
		Fanout:        cfg.Gossip.Fanout,
	}
	var pubKey [32]byte
	copy(pubKey[:], id.PubKey)
	gossipEng := gossip.NewEngine(id.ID, cfg.Node.ListenAddr, pubKey, gossipCfg)

	// Route propagator
	propagator := route.NewPropagator(routes, id.ID, cfg.RouteAdvertisementInterval())

	// Session table for UDP
	sessions := forward.NewSessionTable(cfg.SessionTTL())

	// TCP relay
	tcpRelay := forward.NewTCPRelay(nil) // set after

	// UDP relay
	udpRelay := forward.NewUDPRelay(sessions)

	// SOCKS5 server
	socks5Srv := socks5.NewServer(cfg.Node.Socks5Addr, routes, id.ID)

	n := &Node{
		cfg:        cfg,
		identity:   id,
		peers:      gossipEng.PeerManager(),
		gossipEng:  gossipEng,
		routes:     routes,
		propagator: propagator,
		tcpRelay:   tcpRelay,
		udpRelay:   udpRelay,
		sessions:   sessions,
		socks5Srv:  socks5Srv,
		peerConns:    make(map[string]*transport.Conn),
		peerUDPAddrs: make(map[string]*net.UDPAddr),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Wire up cross-component callbacks
	n.wireCallbacks()

	return n, nil
}

func (n *Node) wireCallbacks() {
	// Set message signing on gossip engine
	n.gossipEng.SetSignFunc(func(msg []byte) []byte {
		return wire.SignMessage(msg, n.identity.PrivKey)
	})

	// Gossip: when a new peer is discovered, connect to it
	n.gossipEng.OnPeerDiscovered(func(peer *gossip.Peer) {
		go n.connectToPeer(peer.ID, peer.Addr)
	})

	// Gossip: when a peer dies, clean up
	n.gossipEng.OnPeerDead(func(id identity.NodeID) {
		n.propagator.WithdrawAllFromPeer(id)
		n.peerConnsMu.Lock()
		if conn, ok := n.peerConns[id.String()]; ok {
			conn.Close()
			delete(n.peerConns, id.String())
		}
		n.peerConnsMu.Unlock()
	})

	// Route propagator: set sender and broadcaster
	n.propagator.SetSender(func(peerID identity.NodeID, msg []byte) error {
		return n.sendControlMsg(peerID, msg)
	})
	n.propagator.SetBroadcaster(func(msg []byte) {
		n.gossipEng.Broadcast(msg)
	})
	n.propagator.SetPeersFunc(func() []identity.NodeID {
		alive := n.peers.GetAlive()
		ids := make([]identity.NodeID, len(alive))
		for i, p := range alive {
			ids[i] = p.ID
		}
		return ids
	})

	// TCP relay: set dial stream function
	n.tcpRelay = forward.NewTCPRelay(func(peerID [16]byte) (*transport.Stream, error) {
		var id identity.NodeID
		copy(id[:], peerID[:])
		conn := n.getPeerConn(id)
		if conn == nil {
			return nil, fmt.Errorf("no connection to peer %s", id)
		}
		return conn.OpenStream()
	})

	// SOCKS5: set dial functions
	n.socks5Srv.SetDialTCP(func(target string) (net.Conn, error) {
		return net.DialTimeout("tcp", target, 10*time.Second)
	})
	n.socks5Srv.SetDialPeer(func(peerID identity.NodeID, target string) (net.Conn, error) {
		conn := n.getPeerConn(peerID)
		if conn == nil {
			return nil, fmt.Errorf("no connection to peer %s", peerID)
		}
		stream, err := conn.OpenStream()
		if err != nil {
			return nil, err
		}
		// Send TCP forward header
		host, portStr, _ := net.SplitHostPort(target)
		ip := net.ParseIP(host)
		port := uint16(0)
		fmt.Sscanf(portStr, "%d", &port)

		hdr := wire.EncodeTCPForwardHeader(&wire.TCPForwardHeader{
			Flags:      wire.TCPFlagNewStream,
			StreamID:   uint32(stream.StreamID()),
			TargetAddr: ip,
			TargetPort: port,
		})
		if _, err := stream.Write(hdr); err != nil {
			stream.Close()
			return nil, err
		}
		return &streamConn{stream: stream}, nil
	})

	// UDP relay: set send function (uses dedicated UDP socket)
	n.udpRelay.SetSendFunc(func(peerID identity.NodeID, data []byte) error {
		n.peerUDPAddrsMu.Lock()
		addr, ok := n.peerUDPAddrs[peerID.String()]
		n.peerUDPAddrsMu.Unlock()
		if !ok {
			return fmt.Errorf("no UDP address for peer %s", peerID)
		}
		if n.udpLn == nil {
			return nil
		}
		_, err := n.udpLn.WriteTo(data, addr)
		return err
	})

	// UDP relay: set deliver function (to local SOCKS5 client)
	n.udpRelay.SetDeliverFunc(func(session *forward.UDPSession, data []byte) {
		if session.ClientAddr == nil {
			return
		}
		// Deliver back to the SOCKS5 UDP associate socket
		if n.socks5Srv != nil {
			n.socks5Srv.DeliverUDP(session.ID, data)
		}
	})

	// SOCKS5 UDP forward callback
	n.socks5Srv.SetUDPForward(func(nextHop identity.NodeID, sessionID uint32, targetAddr *net.UDPAddr, payload []byte) error {
		session := n.udpRelay.CreateSession(nil, targetAddr, nextHop)
		_ = sessionID
		return n.udpRelay.ForwardToPeer(nextHop, session, payload)
	})

	// SOCKS5 UDP direct callback
	n.socks5Srv.SetUDPDirect(func(targetAddr *net.UDPAddr, payload []byte) {
		conn, err := net.DialUDP("udp", nil, targetAddr)
		if err != nil {
			log.Printf("node: UDP direct dial %s failed: %v", targetAddr, err)
			return
		}
		defer conn.Close()
		conn.Write(payload)
	})
}

// Start begins all background goroutines and listeners.
func (n *Node) Start() error {
	// Generate TLS config
	var err error
	n.tlsCfg, err = transport.GenerateTLSConfig()
	if err != nil {
		return fmt.Errorf("tls config: %w", err)
	}

	// Start transport listener
	ln, err := transport.Listen(n.cfg.Node.ListenAddr, n.tlsCfg)
	if err != nil {
		return fmt.Errorf("transport listen: %w", err)
	}
	n.transportLn = ln
	log.Printf("node: listening on %s", n.cfg.Node.ListenAddr)

	// Start UDP relay listener on the same port
	udpAddr, err := net.ResolveUDPAddr("udp", n.cfg.Node.ListenAddr)
	if err != nil {
		return fmt.Errorf("udp resolve: %w", err)
	}
	udpLn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("udp listen: %w", err)
	}
	n.udpLn = udpLn
	go n.udpReadLoop()

	// Start gossip engine
	n.gossipEng.Start()

	// Start route propagator
	n.propagator.Start()

	// Start SOCKS5 server
	if err := n.socks5Srv.Start(); err != nil {
		return fmt.Errorf("socks5 start: %w", err)
	}

	// Start session GC
	go n.sessionGCLoop()

	// Start route GC
	go n.routeGCLoop()

	// Accept incoming connections
	go n.acceptLoop()

	// Connect to bootstrap peers
	go n.bootstrap()

	return nil
}

// Stop gracefully shuts down all subsystems.
func (n *Node) Stop() error {
	n.cancel()

	n.socks5Srv.Stop()
	n.propagator.Stop()
	n.gossipEng.Stop()

	n.peerConnsMu.Lock()
	for _, conn := range n.peerConns {
		conn.Close()
	}
	n.peerConnsMu.Unlock()

	if n.transportLn != nil {
		n.transportLn.Close()
	}

	if n.udpLn != nil {
		n.udpLn.Close()
	}

	return nil
}

func (n *Node) acceptLoop() {
	for {
		conn, err := n.transportLn.Accept()
		if err != nil {
			select {
			case <-n.ctx.Done():
				return
			default:
				log.Printf("node: accept error: %v", err)
				continue
			}
		}
		go n.handleIncomingConn(conn)
	}
}

func (n *Node) handleIncomingConn(conn *transport.Conn) {
	// The peer's identity will be learned through the control stream
	// For now, register the control stream and wait for gossip/routing messages
	cs := conn.ControlStream()

	// Read first message to identify the peer (should be a gossip push or route update)
	buf := make([]byte, 65536)
	nread, err := cs.Read(buf)
	if err != nil {
		log.Printf("node: read initial message failed: %v", err)
		conn.Close()
		return
	}

	// Try to extract peer identity from the message
	var peerID identity.NodeID
	switch buf[0] {
	case wire.MsgGossipPush:
		msg, err := wire.DecodeGossipPush(buf[:nread])
		if err == nil && len(msg.Peers) > 0 {
			// The first peer entry should be the sender's self
			copy(peerID[:], msg.Peers[0].ID[:])
		}
	}

	if peerID.IsZero() {
		log.Printf("node: could not identify peer from initial message")
		conn.Close()
		return
	}

	log.Printf("node: accepted connection from %s", peerID)

	n.peerConnsMu.Lock()
	n.peerConns[peerID.String()] = conn
	n.peerConnsMu.Unlock()

	n.registerPeerUDPAddr(peerID, conn.RemoteAddr().String())
	n.gossipEng.AddDirectConnection(peerID, conn)

	// Start handling incoming streams for TCP relay
	go n.handleIncomingStreams(peerID, conn)

	n.handleControlMessages(peerID, conn, buf[:nread])
}

func (n *Node) connectToPeer(peerID identity.NodeID, addr string) {
	n.peerConnsMu.Lock()
	_, exists := n.peerConns[peerID.String()]
	n.peerConnsMu.Unlock()
	if exists {
		return
	}

	log.Printf("node: connecting to peer %s at %s", peerID, addr)

	conn, err := transport.Dial(addr, n.tlsCfg, nil) // PinStore disabled: TLS certs are ephemeral
	if err != nil {
		log.Printf("node: dial peer %s failed: %v", peerID, err)
		return
	}

	n.peerConnsMu.Lock()
	n.peerConns[peerID.String()] = conn
	n.peerConnsMu.Unlock()

	n.registerPeerUDPAddr(peerID, addr)

	// Send our gossip push immediately so the remote peer can identify us
	cs := conn.ControlStream()
	cs.Write(n.buildSelfGossipPush())

	n.gossipEng.AddDirectConnection(peerID, conn)

	// Trigger immediate route advertisement to this peer
	n.propagator.AdvertiseDirectNetworks()

	// Start handling incoming streams for TCP relay
	go n.handleIncomingStreams(peerID, conn)

	// Read control messages
	n.handleControlMessages(peerID, conn, nil)
}

func (n *Node) handleControlMessages(peerID identity.NodeID, conn *transport.Conn, initialData []byte) {
	cs := conn.ControlStream()
	buf := make([]byte, 65536)

	if initialData != nil {
		n.dispatchControlMsg(peerID, initialData)
	}

	for {
		nread, err := cs.Read(buf)
		if err != nil {
			log.Printf("node: read from %s failed: %v", peerID, err)
			n.peerConnsMu.Lock()
			delete(n.peerConns, peerID.String())
			n.peerConnsMu.Unlock()
			conn.Close()
			n.peers.MarkDead(peerID)
			n.propagator.WithdrawAllFromPeer(peerID)
			return
		}
		n.dispatchControlMsg(peerID, buf[:nread])
	}
}

func (n *Node) dispatchControlMsg(from identity.NodeID, data []byte) {
	if len(data) == 0 {
		return
	}

	// Verify signature if we know this peer's public key
	if peer, ok := n.peers.Get(from); ok && len(peer.PubKey) > 0 {
		verified, err := wire.VerifyMessage(data, peer.PubKey)
		if err != nil {
			log.Printf("node: signature verification failed from %s: %v", from, err)
			return
		}
		data = verified
	}

	// Try gossip engine first
	if n.gossipEng.HandleMessage(from, data) {
		return
	}

	switch data[0] {
	case wire.MsgRouteUpdate:
		n.propagator.HandleRouteUpdate(from, data)

	case wire.MsgTCPForward:
		// TCP data streams are handled on dedicated transport streams

	case wire.MsgUDPForward:
		n.udpRelay.HandleIncoming(from, data)
	}
}

func (n *Node) handleIncomingStreams(peerID identity.NodeID, conn *transport.Conn) {
	for {
		stream, err := conn.AcceptStream()
		if err != nil {
			return
		}
		go func(s *transport.Stream) {
			if err := n.tcpRelay.HandleIncomingRelay(s, func(addr string) (net.Conn, error) {
				return net.DialTimeout("tcp", addr, 10*time.Second)
			}); err != nil {
				log.Printf("node: TCP relay from %s failed: %v", peerID, err)
			}
		}(stream)
	}
}

func (n *Node) sendControlMsg(peerID identity.NodeID, msg []byte) error {
	conn := n.getPeerConn(peerID)
	if conn == nil {
		return fmt.Errorf("no connection to peer %s", peerID)
	}
	msg = wire.SignMessage(msg, n.identity.PrivKey)
	cs := conn.ControlStream()
	_, err := cs.Write(msg)
	return err
}

func (n *Node) getPeerConn(peerID identity.NodeID) *transport.Conn {
	n.peerConnsMu.Lock()
	defer n.peerConnsMu.Unlock()
	return n.peerConns[peerID.String()]
}

func (n *Node) registerPeerUDPAddr(peerID identity.NodeID, addr string) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	udpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, n.udpPort()))
	if err != nil {
		return
	}
	n.peerUDPAddrsMu.Lock()
	n.peerUDPAddrs[peerID.String()] = udpAddr
	n.peerUDPAddrsMu.Unlock()
}

func (n *Node) udpPort() string {
	_, port, err := net.SplitHostPort(n.cfg.Node.ListenAddr)
	if err != nil {
		return "9443"
	}
	return port
}

func (n *Node) bootstrap() {
	for _, bp := range n.cfg.Gossip.BootstrapPeers {
		addr := bp
		conn, err := transport.Dial(addr, n.tlsCfg, nil) // PinStore disabled: TLS certs are ephemeral
		if err != nil {
			log.Printf("node: bootstrap dial %s failed: %v", addr, err)
			continue
		}

		// Send our gossip push first so the remote can identify us
		cs := conn.ControlStream()
		push := n.buildSelfGossipPush()
		cs.Write(push)

		// Now read the remote's gossip push to learn their identity
		buf := make([]byte, 65536)
		nread, err := cs.Read(buf)
		if err != nil {
			log.Printf("node: bootstrap read from %s failed: %v", addr, err)
			conn.Close()
			continue
		}

		var peerID identity.NodeID
		if buf[0] == wire.MsgGossipPush {
			msg, err := wire.DecodeGossipPush(buf[:nread])
			if err == nil && len(msg.Peers) > 0 {
				copy(peerID[:], msg.Peers[0].ID[:])
			}
		}

		if peerID.IsZero() {
			log.Printf("node: bootstrap could not identify peer at %s", addr)
			conn.Close()
			continue
		}

		log.Printf("node: bootstrap connected to %s", peerID)

		n.peerConnsMu.Lock()
		n.peerConns[peerID.String()] = conn
		n.peerConnsMu.Unlock()

		n.gossipEng.AddDirectConnection(peerID, conn)
		n.registerPeerUDPAddr(peerID, addr)

		// Start handling incoming streams for TCP relay
		go n.handleIncomingStreams(peerID, conn)

		go n.handleControlMessages(peerID, conn, buf[:nread])
	}
}

func (n *Node) buildSelfGossipPush() []byte {
	peers := n.gossipEng.PeerManager().GetAll()
	entries := make([]wire.PeerEntry, 0, len(peers)+1)
	var selfID [16]byte
	copy(selfID[:], n.identity.ID[:])
	var selfPubKey [32]byte
	copy(selfPubKey[:], n.identity.PubKey)
	entries = append(entries, wire.PeerEntry{
		ID:      selfID,
		PubKey:  selfPubKey,
		State:   wire.PeerAlive,
		Version: 1,
		Addr:    n.cfg.Node.ListenAddr,
	})
	for _, p := range peers {
		var id [16]byte
		copy(id[:], p.ID[:])
		var pk [32]byte
		if len(p.PubKey) == 32 {
			copy(pk[:], p.PubKey)
		}
		entries = append(entries, wire.PeerEntry{
			ID:      id,
			PubKey:  pk,
			State:   wire.PeerState(p.State),
			Version: p.Version,
			Addr:    p.Addr,
		})
	}
	return wire.EncodeGossipPush(&wire.GossipPushMessage{Peers: entries})
}

func (n *Node) sessionGCLoop() {
	ticker := time.NewTicker(n.cfg.SessionGCInterval())
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.sessions.GC()
		}
	}
}

func (n *Node) routeGCLoop() {
	// Route TTL is 3x the advertisement interval to allow for missed updates
	routeTTL := n.cfg.RouteAdvertisementInterval() * 3
	ticker := time.NewTicker(n.cfg.RouteAdvertisementInterval())
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			removed := n.routes.GC(routeTTL)
			if removed > 0 {
				log.Printf("node: route GC removed %d stale routes", removed)
			}
		}
	}
}

func (n *Node) udpReadLoop() {
	buf := make([]byte, 65536)
	for {
		nr, remote, err := n.udpLn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-n.ctx.Done():
				return
			default:
				log.Printf("node: UDP read error: %v", err)
				continue
			}
		}

		// Store the peer's UDP address
		n.peerUDPAddrsMu.Lock()
		found := false
		var peerID identity.NodeID
		for id, addr := range n.peerUDPAddrs {
			if addr.IP.Equal(remote.IP) && addr.Port == remote.Port {
				peerID, _ = identity.ParseNodeID(id)
				found = true
				break
			}
		}
		n.peerUDPAddrsMu.Unlock()

		if found {
			n.udpRelay.HandleIncoming(peerID, buf[:nr])
		} else {
			// Try to match by IP (first datagram from this peer)
			n.peerConnsMu.Lock()
			for id, conn := range n.peerConns {
				host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
				if host == remote.IP.String() {
					pid, _ := identity.ParseNodeID(id)
					n.peerUDPAddrsMu.Lock()
					n.peerUDPAddrs[id] = remote
					n.peerUDPAddrsMu.Unlock()
					n.udpRelay.HandleIncoming(pid, buf[:nr])
					found = true
					break
				}
			}
			n.peerConnsMu.Unlock()
			if !found {
				log.Printf("node: UDP datagram from unknown peer %s", remote)
			}
		}
	}
}

// streamConn adapts a transport.Stream to net.Conn for SOCKS5 relay.
type streamConn struct {
	stream *transport.Stream
}

func (s *streamConn) Read(b []byte) (int, error)  { return s.stream.Read(b) }
func (s *streamConn) Write(b []byte) (int, error) { return s.stream.Write(b) }
func (s *streamConn) Close() error                { return s.stream.Close() }
func (s *streamConn) LocalAddr() net.Addr         { return &net.TCPAddr{} }
func (s *streamConn) RemoteAddr() net.Addr        { return &net.TCPAddr{} }
func (s *streamConn) SetDeadline(t time.Time) error {
	return nil
}
func (s *streamConn) SetReadDeadline(t time.Time) error  { return nil }
func (s *streamConn) SetWriteDeadline(t time.Time) error { return nil }
