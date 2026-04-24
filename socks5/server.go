package socks5

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/example/magictun/identity"
	"github.com/example/magictun/route"
)

// Server is a SOCKS5 proxy server.
type Server struct {
	addr   string
	routes *route.RoutingTable
	selfID identity.NodeID

	// Forwarding callbacks
	dialTCPFunc  func(target string) (net.Conn, error)
	dialPeerFunc func(peerID identity.NodeID, target string) (net.Conn, error)
	udpFunc      func(clientAddr, targetAddr *net.UDPAddr) (identity.NodeID, bool) // returns nextHop

	ln     net.Listener
	ctx    context.Context
	cancel context.CancelFunc
}

// NewServer creates a new SOCKS5 server.
func NewServer(addr string, routes *route.RoutingTable, selfID identity.NodeID) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		addr:   addr,
		routes: routes,
		selfID: selfID,
		ctx:    ctx,
		cancel: cancel,
	}
}

// SetDialTCP sets the function for direct TCP connections.
func (s *Server) SetDialTCP(fn func(target string) (net.Conn, error)) {
	s.dialTCPFunc = fn
}

// SetDialPeer sets the function for TCP connections via a peer.
func (s *Server) SetDialPeer(fn func(peerID identity.NodeID, target string) (net.Conn, error)) {
	s.dialPeerFunc = fn
}

// SetUDPFunc sets the function for UDP routing.
func (s *Server) SetUDPFunc(fn func(clientAddr, targetAddr *net.UDPAddr) (identity.NodeID, bool)) {
	s.udpFunc = fn
}

// Start begins listening for SOCKS5 connections.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("socks5 listen: %w", err)
	}
	s.ln = ln
	log.Printf("socks5: listening on %s", s.addr)

	go s.acceptLoop()
	return nil
}

// Stop shuts down the SOCKS5 server.
func (s *Server) Stop() error {
	s.cancel()
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Printf("socks5: accept error: %v", err)
				continue
			}
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// SOCKS5 handshake
	targetAddr, err := s.handleHandshake(conn)
	if err != nil {
		log.Printf("socks5: handshake error: %v", err)
		return
	}

	// Route lookup
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		log.Printf("socks5: invalid target %s: %v", targetAddr, err)
		return
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// DNS resolution (simple: try to resolve)
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			log.Printf("socks5: dns resolve %s failed: %v", host, err)
			return
		}
		ip = ips[0]
		targetAddr = net.JoinHostPort(ip.String(), port)
	}

	route, found := s.routes.Lookup(ip)
	if !found {
		log.Printf("socks5: no route to %s", ip)
		SendErrorReply(conn, repNetUnreachable)
		return
	}

	// Direct connection (target is in our direct network)
	if route.NextHop.IsZero() {
		if s.dialTCPFunc != nil {
			targetConn, err := s.dialTCPFunc(targetAddr)
			if err != nil {
				log.Printf("socks5: direct dial %s failed: %v", targetAddr, err)
				SendErrorReply(conn, repHostUnreachable)
				return
			}
			defer targetConn.Close()
			SendSuccessReply(conn)
			relay(conn, targetConn)
		}
		return
	}

	// Forward via peer
	if s.dialPeerFunc != nil {
		peerConn, err := s.dialPeerFunc(route.NextHop, targetAddr)
		if err != nil {
			log.Printf("socks5: peer dial %s via %s failed: %v", targetAddr, route.NextHop, err)
			SendErrorReply(conn, repHostUnreachable)
			return
		}
		defer peerConn.Close()
		SendSuccessReply(conn)
		relay(conn, peerConn)
	}
}

func relay(a, b net.Conn) {
	errCh := make(chan error, 2)
	go func() {
		_, err := ioCopy(a, b)
		errCh <- err
	}()
	go func() {
		_, err := ioCopy(b, a)
		errCh <- err
	}()
	<-errCh
}

func ioCopy(dst, src net.Conn) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			nw, we := dst.Write(buf[:n])
			total += int64(nw)
			if we != nil {
				return total, we
			}
		}
		if err != nil {
			return total, err
		}
	}
}
