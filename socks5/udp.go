package socks5

import (
	"encoding/binary"
	"log"
	"net"
	"sync"
	"time"

	"github.com/example/magictun/identity"
)

// UDPForwardFunc forwards a UDP payload to a peer via the relay.
type UDPForwardFunc func(nextHop identity.NodeID, sessionID uint32, targetAddr *net.UDPAddr, payload []byte) error

// UDPDeliverFunc delivers a UDP payload to a local SOCKS5 client.
type UDPDeliverFunc func(sessionID uint32, payload []byte)

// UDPSessionInfo is the metadata for a UDP relay session from SOCKS5 side.
type UDPSessionInfo struct {
	ID         uint32
	ClientAddr *net.UDPAddr
	TargetAddr *net.UDPAddr
	NextHop    identity.NodeID
}

// CreateSessionFunc creates a new UDP session.
type CreateSessionFunc func(clientAddr, targetAddr *net.UDPAddr, nextHop identity.NodeID) *UDPSessionInfo

// LookupSessionFunc looks up a session by ID.
type LookupSessionFunc func(id uint32) (*UDPSessionInfo, bool)

// udpAssociate holds state for a single UDP ASSOCIATE client.
type udpAssociate struct {
	conn       *net.UDPConn
	clientAddr *net.UDPAddr // the client that made the ASSOCIATE request
	sessions   map[uint32]*udpClientSession
	mu         sync.Mutex
	quit       chan struct{}
}

type udpClientSession struct {
	sessionID  uint32
	clientAddr *net.UDPAddr
	targetAddr *net.UDPAddr
}

// handleUDPAssociate processes a SOCKS5 UDP ASSOCIATE request.
func (s *Server) handleUDPAssociate(tcpConn net.Conn, req *SocksRequest) {
	// Allocate a local UDP socket
	udpLn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		log.Printf("socks5: UDP associate listen failed: %v", err)
		SendErrorReply(tcpConn, repGeneralFailure)
		return
	}
	defer udpLn.Close()

	bindAddr := udpLn.LocalAddr().(*net.UDPAddr)

	// Send success reply with bind address
	sendReply(tcpConn, repSuccess, bindAddr.IP, uint16(bindAddr.Port))

	ua := &udpAssociate{
		conn:     udpLn,
		sessions: make(map[uint32]*udpClientSession),
		quit:     make(chan struct{}),
	}

	// Keep TCP connection alive to detect client disconnect
	go func() {
		buf := make([]byte, 1)
		tcpConn.Read(buf)
		close(ua.quit)
		udpLn.Close()
	}()

	// Read UDP datagrams from client
	buf := make([]byte, 65536)
	for {
		n, clientAddr, err := udpLn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ua.quit:
				return
			default:
				log.Printf("socks5: UDP read error: %v", err)
				return
			}
		}

		ua.clientAddr = clientAddr
		s.handleUDPDatagram(ua, buf[:n])
	}
}

func (s *Server) handleUDPDatagram(ua *udpAssociate, data []byte) {
	// SOCKS5 UDP header: RSV(2) + FRAG(1) + ATYP(1) + DST.ADDR(v) + DST.PORT(2) + DATA
	if len(data) < 10 {
		return
	}
	if data[0] != 0 || data[1] != 0 {
		return // RSV must be 0
	}
	if data[2] != 0 {
		return // fragmentation not supported
	}

	var targetHost string
	var targetPort uint16
	var headerLen int

	switch data[3] {
	case addrTypeIPv4:
		if len(data) < 10 {
			return
		}
		targetHost = net.IP(data[4:8]).String()
		targetPort = binary.BigEndian.Uint16(data[8:10])
		headerLen = 10
	case addrTypeDomain:
		if len(data) < 7 {
			return
		}
		domainLen := int(data[4])
		if len(data) < 5+domainLen+2 {
			return
		}
		targetHost = string(data[5 : 5+domainLen])
		targetPort = binary.BigEndian.Uint16(data[5+domainLen:])
		headerLen = 5 + domainLen + 2
	case addrTypeIPv6:
		if len(data) < 22 {
			return
		}
		targetHost = net.IP(data[4:20]).String()
		targetPort = binary.BigEndian.Uint16(data[20:22])
		headerLen = 22
	default:
		return
	}

	payload := data[headerLen:]

	// Resolve domain if needed
	ip := net.ParseIP(targetHost)
	if ip == nil {
		ips, err := net.LookupIP(targetHost)
		if err != nil || len(ips) == 0 {
			log.Printf("socks5: UDP DNS resolve %s failed", targetHost)
			return
		}
		ip = ips[0]
	}

	targetUDPAddr := &net.UDPAddr{IP: ip, Port: int(targetPort)}

	// Route lookup
	route, found := s.routes.Lookup(ip)
	if !found {
		log.Printf("socks5: UDP no route to %s", ip)
		return
	}

	// Direct delivery (target is in our network)
	if route.NextHop.IsZero() {
		if s.udpDirectFunc != nil {
			s.udpDirectFunc(targetUDPAddr, payload)
		}
		return
	}

	// Forward via relay
	if s.udpForwardFunc != nil {
		// Get or create a session for this client->target flow
		sessionID := ua.getSessionID(ua.clientAddr, targetUDPAddr)
		s.udpForwardFunc(route.NextHop, sessionID, targetUDPAddr, payload)
	}
}

// DeliverUDP delivers a UDP response back to the local client.
func (s *Server) DeliverUDP(sessionID uint32, payload []byte) {
	// We need to track which udpAssociate has this session
	// For now, this is handled through the forward layer
}

func (ua *udpAssociate) getSessionID(clientAddr, targetAddr *net.UDPAddr) uint32 {
	ua.mu.Lock()
	defer ua.mu.Unlock()

	key := clientAddr.String() + "->" + targetAddr.String()
	for _, s := range ua.sessions {
		if s.clientAddr.String() == clientAddr.String() && s.targetAddr.String() == targetAddr.String() {
			return s.sessionID
		}
	}

	// Create a new local session ID
	var id uint32
	for {
		id = uint32(time.Now().UnixNano() & 0xFFFFFFFF)
		if id == 0 {
			continue
		}
		if _, ok := ua.sessions[id]; !ok {
			break
		}
	}
	ua.sessions[id] = &udpClientSession{
		sessionID:  id,
		clientAddr: clientAddr,
		targetAddr: targetAddr,
	}
	_ = key
	return id
}
