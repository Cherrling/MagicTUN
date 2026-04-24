package forward

import (
	"log"
	"net"

	"github.com/example/magictun/identity"
	"github.com/example/magictun/wire"
)

// UDPRelay handles UDP datagram forwarding over the overlay network.
type UDPRelay struct {
	sessions *SessionTable

	// Function to send a UDP datagram to a peer
	sendUDP func(peerID identity.NodeID, data []byte) error

	// Function to deliver a datagram to a local SOCKS5 client
	deliverLocal func(session *UDPSession, data []byte)
}

// NewUDPRelay creates a new UDP relay.
func NewUDPRelay(sessions *SessionTable) *UDPRelay {
	return &UDPRelay{sessions: sessions}
}

// SetSendFunc sets the function to send UDP to a peer.
func (r *UDPRelay) SetSendFunc(fn func(peerID identity.NodeID, data []byte) error) {
	r.sendUDP = fn
}

// SetDeliverFunc sets the function to deliver to local clients.
func (r *UDPRelay) SetDeliverFunc(fn func(session *UDPSession, data []byte)) {
	r.deliverLocal = fn
}

// ForwardToPeer forwards a UDP datagram from a local client to a remote peer.
func (r *UDPRelay) ForwardToPeer(nextHop identity.NodeID, session *UDPSession, payload []byte) error {
	if r.sendUDP == nil {
		return nil
	}
	data := wire.EncodeUDPForward(session.ID, payload)
	return r.sendUDP(nextHop, data)
}

// HandleIncoming processes an incoming UDP datagram from a peer.
func (r *UDPRelay) HandleIncoming(fromPeer identity.NodeID, data []byte) {
	hdr, payload, err := wire.DecodeUDPForward(data)
	if err != nil {
		log.Printf("udp relay: decode error: %v", err)
		return
	}

	session, ok := r.sessions.Lookup(hdr.SessionID)
	if !ok {
		log.Printf("udp relay: unknown session %d", hdr.SessionID)
		return
	}

	r.sessions.Touch(hdr.SessionID)

	// Check direction
	if fromPeer.Equal(session.NextHop) {
		// Response from target side: deliver to local client
		if r.deliverLocal != nil {
			r.deliverLocal(session, payload)
		}
	} else {
		// From local client: forward to next hop
		r.ForwardToPeer(session.NextHop, session, payload)
	}
}

// CreateSession creates a UDP session for a client->target flow.
func (r *UDPRelay) CreateSession(clientAddr, targetAddr *net.UDPAddr, nextHop identity.NodeID) *UDPSession {
	return r.sessions.Create(clientAddr, targetAddr, nextHop)
}

// GetSession looks up a session by ID.
func (r *UDPRelay) GetSession(id uint32) (*UDPSession, bool) {
	return r.sessions.Lookup(id)
}
