package route

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/example/magictun/identity"
	"github.com/example/magictun/wire"
)

// Propagator handles route advertisement and withdrawal.
type Propagator struct {
	table  *RoutingTable
	selfID identity.NodeID

	// Function to send a message to a specific peer.
	sendFunc func(peerID identity.NodeID, msg []byte) error
	// Function to broadcast to all directly connected peers.
	broadcastFunc func(msg []byte)
	// Function to get all alive peers.
	getPeersFunc func() []identity.NodeID

	interval time.Duration

	mu       sync.Mutex
	stopCh   chan struct{}
}

// NewPropagator creates a new route propagator.
func NewPropagator(table *RoutingTable, selfID identity.NodeID, interval time.Duration) *Propagator {
	return &Propagator{
		table:    table,
		selfID:   selfID,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// SetSender sets the function to send messages to peers.
func (p *Propagator) SetSender(sendFunc func(peerID identity.NodeID, msg []byte) error) {
	p.sendFunc = sendFunc
}

// SetBroadcaster sets the broadcast function.
func (p *Propagator) SetBroadcaster(broadcastFunc func(msg []byte)) {
	p.broadcastFunc = broadcastFunc
}

// SetPeersFunc sets the function to get alive peers.
func (p *Propagator) SetPeersFunc(fn func() []identity.NodeID) {
	p.getPeersFunc = fn
}

// Start begins periodic route advertisement.
func (p *Propagator) Start() {
	go p.advertiseLoop()
}

// Stop stops the propagator.
func (p *Propagator) Stop() {
	close(p.stopCh)
}

// HandleRouteUpdate processes an incoming route update from a peer.
func (p *Propagator) HandleRouteUpdate(fromPeer identity.NodeID, data []byte) {
	msg, err := wire.DecodeRouteMessage(data)
	if err != nil {
		log.Printf("route: decode update from %s: %v", fromPeer, err)
		return
	}

	var origin identity.NodeID
	copy(origin[:], msg.Origin[:])

	// Withdrawal
	if msg.Flags&wire.RouteFlagWithdraw != 0 {
		p.table.RemoveRoute(msg.Prefix, origin)
		p.propagateExcept(data, fromPeer)
		return
	}

	// Build ASPath from wire
	asPath := ASPathFromWire(msg.ASPath)

	// Loop detection: check the received path, not our modified version
	if asPath.Contains(p.selfID) {
		return
	}

	// Check max path length
	if asPath.Len() >= 32 {
		return
	}

	route := &Route{
		Destination: msg.Prefix,
		NextHop:     fromPeer,
		ASPath:      asPath, // original path, self will be added for re-advertisement
		Cost:        msg.Cost + 1,
		Origin:      origin,
		LocalPref:   msg.LocalPref,
	}

	if p.table.AddRoute(route) {
		// Re-advertise to other peers with self in the path
		reencoded := p.encodeRouteWithSelf(route)
		p.propagateExcept(reencoded, fromPeer)
	}
}

// AdvertiseDirectNetworks sends routes for our directly connected networks.
func (p *Propagator) AdvertiseDirectNetworks() {
	nets := p.table.GetDirectNetworks()
	for _, nw := range nets {
		route := &Route{
			Destination: nw,
			NextHop:     p.selfID,
			ASPath:      &ASPath{}, // empty path for locally originated routes
			Cost:        1,
			Origin:      p.selfID,
			LocalPref:   100,
		}
		p.table.AddRoute(route)
		msg := p.encodeRouteWithSelf(route)
		if p.broadcastFunc != nil {
			p.broadcastFunc(msg)
		}
	}
}

// WithdrawAllFromPeer removes all routes from a peer that went down.
func (p *Propagator) WithdrawAllFromPeer(peerID identity.NodeID) {
	routes := p.table.GetAllRoutes()
	p.table.RemoveAllFromOrigin(peerID)

	// Broadcast withdrawals for routes that went through this peer
	for _, r := range routes {
		if r.NextHop.Equal(peerID) || r.Origin.Equal(peerID) {
			msg := p.encodeWithdrawal(r)
			if p.broadcastFunc != nil {
				p.broadcastFunc(msg)
			}
		}
	}
}

func (p *Propagator) advertiseLoop() {
	// Initial advertisement
	time.Sleep(2 * time.Second)
	p.AdvertiseDirectNetworks()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.AdvertiseDirectNetworks()
			// Advertise best routes
			routes := p.table.GetAllRoutes()
			for _, r := range routes {
				msg := p.encodeRouteWithSelf(r)
				if p.broadcastFunc != nil {
					p.broadcastFunc(msg)
				}
			}
		}
	}
}

func (p *Propagator) propagateExcept(msg []byte, except identity.NodeID) {
	if p.getPeersFunc == nil || p.sendFunc == nil {
		return
	}
	for _, peerID := range p.getPeersFunc() {
		if peerID.Equal(except) {
			continue
		}
		if err := p.sendFunc(peerID, msg); err != nil {
			log.Printf("route: send to %s failed: %v", peerID, err)
		}
	}
}

func (p *Propagator) encodeRoute(r *Route) []byte {
	return wire.EncodeRouteMessage(&wire.RouteMessage{
		Flags:     wire.RouteFlagUpdate,
		Prefix:    r.Destination,
		Cost:      r.Cost,
		Origin:    toWireID(r.Origin),
		LocalPref: r.LocalPref,
		ASPath:    r.ASPath.ToWire(),
	})
}

// encodeRouteWithSelf encodes a route for advertisement, prepending self to the AS-Path.
func (p *Propagator) encodeRouteWithSelf(r *Route) []byte {
	pathWithSelf := r.ASPath.CloneWithHop(p.selfID)
	return wire.EncodeRouteMessage(&wire.RouteMessage{
		Flags:     wire.RouteFlagUpdate,
		Prefix:    r.Destination,
		Cost:      r.Cost,
		Origin:    toWireID(r.Origin),
		LocalPref: r.LocalPref,
		ASPath:    pathWithSelf.ToWire(),
	})
}

func (p *Propagator) encodeWithdrawal(r *Route) []byte {
	return wire.EncodeRouteMessage(&wire.RouteMessage{
		Flags:     wire.RouteFlagWithdraw,
		Prefix:    r.Destination,
		Cost:      r.Cost,
		Origin:    toWireID(r.Origin),
		LocalPref: r.LocalPref,
		ASPath:    r.ASPath.ToWire(),
	})
}

func toWireID(id identity.NodeID) [16]byte {
	var w [16]byte
	copy(w[:], id[:])
	return w
}

// ToNodeID converts a wire-format node ID to identity.NodeID.
func ToNodeID(w [16]byte) identity.NodeID {
	var id identity.NodeID
	copy(id[:], w[:])
	return id
}

// ParseCIDR parses a CIDR string into net.IPNet.
func ParseCIDR(s string) (net.IPNet, error) {
	_, nw, err := net.ParseCIDR(s)
	if err != nil {
		return net.IPNet{}, err
	}
	return *nw, nil
}
