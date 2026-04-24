package gossip

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/example/magictun/identity"
	"github.com/example/magictun/transport"
	"github.com/example/magictun/wire"
)

// Engine drives the SWIM-like gossip protocol.
type Engine struct {
	selfID   identity.NodeID
	selfAddr string
	pubKey   [32]byte
	peers    *PeerManager
	config   Config

	// Peer connections (control stream per peer)
	conns   map[string]*transport.Conn
	connsMu sync.Mutex

	// Callback when a new peer is discovered
	onPeerDiscovered func(peer *Peer)
	// Callback when a peer goes dead
	onPeerDead func(id identity.NodeID)

	// SignFunc signs outgoing messages with our private key.
	signFunc func([]byte) []byte

	ctx    context.Context
	cancel context.CancelFunc
}

// Config holds gossip engine configuration.
type Config struct {
	PushInterval  time.Duration
	ProbeInterval time.Duration
	PeerTimeout   time.Duration
	Fanout        int
}

// DefaultConfig returns sensible gossip defaults.
func DefaultConfig() Config {
	return Config{
		PushInterval:  5 * time.Second,
		ProbeInterval: 3 * time.Second,
		PeerTimeout:   15 * time.Second,
		Fanout:        3,
	}
}

// NewEngine creates a new gossip engine.
func NewEngine(selfID identity.NodeID, selfAddr string, pubKey [32]byte, cfg Config) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		selfID:   selfID,
		selfAddr: selfAddr,
		pubKey:   pubKey,
		peers:    NewPeerManager(selfID),
		config:   cfg,
		conns:    make(map[string]*transport.Conn),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the gossip loops.
func (e *Engine) Start() {
	go e.pushLoop()
	go e.probeLoop()
}

// Stop shuts down the gossip engine.
func (e *Engine) Stop() {
	e.cancel()
}

// PeerManager returns the underlying peer manager.
func (e *Engine) PeerManager() *PeerManager {
	return e.peers
}

// OnPeerDiscovered sets the callback for new peer discovery.
func (e *Engine) OnPeerDiscovered(fn func(peer *Peer)) {
	e.onPeerDiscovered = fn
}

// OnPeerDead sets the callback for peer death.
func (e *Engine) OnPeerDead(fn func(id identity.NodeID)) {
	e.onPeerDead = fn
}

// SetSignFunc sets the function to sign outgoing messages.
func (e *Engine) SetSignFunc(fn func([]byte) []byte) {
	e.signFunc = fn
}

// AddDirectConnection registers a direct transport connection to a peer.
// Sends an immediate gossip push to the new peer.
func (e *Engine) AddDirectConnection(id identity.NodeID, conn *transport.Conn) {
	e.connsMu.Lock()
	e.conns[id.String()] = conn
	e.connsMu.Unlock()

	// Register the peer
	e.peers.AddOrUpdate(&Peer{
		ID:    id,
		Addr:  conn.RemoteAddr().String(),
		State: PeerAlive,
	})

	// Send immediate gossip push to the new peer
	msg := e.buildGossipPush()
	msg = e.signMsg(msg)
	cs := conn.ControlStream()
	cs.Write(msg)
}

// HandleMessage processes a control message from a peer.
// Returns true if the message was handled.
func (e *Engine) HandleMessage(from identity.NodeID, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	switch data[0] {
	case wire.MsgGossipPush:
		msg, err := wire.DecodeGossipPush(data)
		if err != nil {
			log.Printf("gossip: decode push from %s failed: %v", from, err)
			return true
		}
		e.handlePush(msg)
		return true

	case wire.MsgGossipPull:
		push := e.buildGossipPush()
		e.SendTo(from, push)
		return true

	case wire.MsgPing:
		e.SendTo(from, wire.EncodePong())
		return true

	case wire.MsgPong:
		e.peers.MarkAlive(from)
		return true
	}
	return false
}

// RemoveDirectConnection removes a peer's connection.
func (e *Engine) RemoveDirectConnection(id identity.NodeID) {
	e.connsMu.Lock()
	delete(e.conns, id.String())
	e.connsMu.Unlock()
}

// Broadcast sends a message to all directly connected peers.
func (e *Engine) Broadcast(msg []byte) {
	msg = e.signMsg(msg)
	e.connsMu.Lock()
	defer e.connsMu.Unlock()
	for id, conn := range e.conns {
		cs := conn.ControlStream()
		if _, err := cs.Write(msg); err != nil {
			log.Printf("gossip: broadcast to %s failed: %v", id, err)
		}
	}
}

// SendTo sends a message to a specific peer.
func (e *Engine) SendTo(id identity.NodeID, msg []byte) error {
	e.connsMu.Lock()
	conn, ok := e.conns[id.String()]
	e.connsMu.Unlock()
	if !ok {
		return nil // peer not directly connected
	}
	msg = e.signMsg(msg)
	cs := conn.ControlStream()
	_, err := cs.Write(msg)
	return err
}

func (e *Engine) pushLoop() {
	ticker := time.NewTicker(e.config.PushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.pushToRandom()
		}
	}
}

func (e *Engine) pushToRandom() {
	targets := e.peers.GetRandom(e.config.Fanout, identity.NodeID{})
	for _, target := range targets {
		e.connsMu.Lock()
		_, directlyConnected := e.conns[target.ID.String()]
		e.connsMu.Unlock()

		if directlyConnected {
			msg := e.buildGossipPush()
			if err := e.SendTo(target.ID, msg); err != nil {
				log.Printf("gossip: push to %s failed: %v", target.ID, err)
			}
		}
	}
}

func (e *Engine) probeLoop() {
	ticker := time.NewTicker(e.config.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.probeRandomPeer()
		}
	}
}

func (e *Engine) probeRandomPeer() {
	alive := e.peers.GetAlive()
	if len(alive) == 0 {
		return
	}

	// Pick a random peer to probe
	idx := int(fastRand() % uint32(len(alive)))
	target := alive[idx]

	e.connsMu.Lock()
	conn, directlyConnected := e.conns[target.ID.String()]
	e.connsMu.Unlock()

	if !directlyConnected {
		return
	}

	// Send ping
	cs := conn.ControlStream()
	ping := e.signMsg(wire.EncodePing())
	if _, err := cs.Write(ping); err != nil {
		log.Printf("gossip: ping to %s failed: %v", target.ID, err)
		e.peers.MarkSuspect(target.ID)
		return
	}

	// Wait for pong via readControlLoop (handled asynchronously)
	// If no pong within probe interval * 3, mark as suspect
	go func() {
		timer := time.NewTimer(e.config.ProbeInterval * 3)
		defer timer.Stop()
		select {
		case <-e.ctx.Done():
			return
		case <-timer.C:
			p, ok := e.peers.Get(target.ID)
			if ok && p.State == PeerAlive && time.Since(p.LastSeen) > e.config.PeerTimeout {
				e.peers.MarkSuspect(target.ID)
				log.Printf("gossip: peer %s marked suspect (no response)", target.ID)
			}
		}
	}()
}

func (e *Engine) buildGossipPush() []byte {
	allPeers := e.peers.GetAll()
	entries := make([]wire.PeerEntry, 0, len(allPeers)+1)

	// Include self
	var selfID [16]byte
	copy(selfID[:], e.selfID[:])
	entries = append(entries, wire.PeerEntry{
		ID:      selfID,
		PubKey:  e.pubKey,
		State:   wire.PeerAlive,
		Version: 1,
		Addr:    e.selfAddr,
	})

	for _, p := range allPeers {
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

func (e *Engine) handlePush(msg *wire.GossipPushMessage) {
	for _, entry := range msg.Peers {
		var id identity.NodeID
		copy(id[:], entry.ID[:])

		if id.Equal(e.selfID) {
			continue
		}

		newPeer := &Peer{
			ID:      id,
			Addr:    entry.Addr,
			State:   PeerState(entry.State),
			Version: entry.Version,
		}
		if !isZeroPubKey(entry.PubKey) {
			newPeer.PubKey = make([]byte, 32)
			copy(newPeer.PubKey, entry.PubKey[:])
		}

		isNew := e.peers.AddOrUpdate(newPeer)
		if isNew && e.onPeerDiscovered != nil {
			e.onPeerDiscovered(newPeer)
		}
	}
}

func isZeroPubKey(pk [32]byte) bool {
	for _, b := range pk {
		if b != 0 {
			return false
		}
	}
	return true
}

func (e *Engine) signMsg(msg []byte) []byte {
	if e.signFunc == nil {
		return msg
	}
	return e.signFunc(msg)
}
