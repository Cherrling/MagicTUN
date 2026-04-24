package route

import (
	"net"
	"sync"
	"time"

	"github.com/example/magictun/identity"
)

// Route represents a single entry in the routing table.
type Route struct {
	Destination net.IPNet
	NextHop     identity.NodeID
	ASPath      *ASPath
	Cost        uint32
	Origin      identity.NodeID
	LocalPref   uint32
	Timestamp   time.Time
}

// RoutingTable is the central forwarding information base.
type RoutingTable struct {
	mu     sync.RWMutex
	routes map[string][]*Route // keyed by CIDR string
	selfID identity.NodeID

	// Networks directly attached to this node.
	directNetworks []net.IPNet
	directMu       sync.RWMutex
}

// NewRoutingTable creates a new routing table.
func NewRoutingTable(selfID identity.NodeID) *RoutingTable {
	return &RoutingTable{
		routes: make(map[string][]*Route),
		selfID: selfID,
	}
}

// SetDirectNetworks sets the CIDRs directly reachable from this node.
func (rt *RoutingTable) SetDirectNetworks(nets []net.IPNet) {
	rt.directMu.Lock()
	rt.directNetworks = nets
	rt.directMu.Unlock()
}

// GetDirectNetworks returns the directly connected networks.
func (rt *RoutingTable) GetDirectNetworks() []net.IPNet {
	rt.directMu.RLock()
	defer rt.directMu.RUnlock()
	return rt.directNetworks
}

// AddRoute inserts or updates a route. Returns true if the route was added/updated.
func (rt *RoutingTable) AddRoute(r *Route) bool {
	key := r.Destination.String()

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Check for AS-path loop
	if r.ASPath.Contains(rt.selfID) {
		return false
	}

	r.Timestamp = time.Now()
	existing, ok := rt.routes[key]
	if !ok {
		rt.routes[key] = []*Route{r}
		return true
	}

	// Check if we already have a route from this origin
	for i, er := range existing {
		if er.Origin.Equal(r.Origin) && er.NextHop.Equal(r.NextHop) {
			if er.Cost != r.Cost || er.LocalPref != r.LocalPref {
				existing[i] = r
				return true
			}
			er.Timestamp = time.Now()
			return false
		}
	}

	rt.routes[key] = append(existing, r)
	return true
}

// RemoveRoute removes routes for a prefix from a specific origin.
func (rt *RoutingTable) RemoveRoute(dest net.IPNet, origin identity.NodeID) {
	key := dest.String()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	existing, ok := rt.routes[key]
	if !ok {
		return
	}
	filtered := existing[:0]
	for _, r := range existing {
		if !r.Origin.Equal(origin) {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		delete(rt.routes, key)
	} else {
		rt.routes[key] = filtered
	}
}

// RemoveAllFromOrigin removes all routes advertised by a given peer.
func (rt *RoutingTable) RemoveAllFromOrigin(origin identity.NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	for key, routes := range rt.routes {
		filtered := routes[:0]
		for _, r := range routes {
			if !r.Origin.Equal(origin) && !r.NextHop.Equal(origin) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			delete(rt.routes, key)
		} else {
			rt.routes[key] = filtered
		}
	}
}

// Lookup finds the best route for a given IP address.
func (rt *RoutingTable) Lookup(ip net.IP) (*Route, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// Check direct networks first
	rt.directMu.RLock()
	for _, nw := range rt.directNetworks {
		if nw.Contains(ip) {
			rt.directMu.RUnlock()
			return &Route{
				Destination: nw,
				Cost:        0,
			}, true
		}
	}
	rt.directMu.RUnlock()

	var bestMatch *Route
	var bestMaskLen int

	for _, routes := range rt.routes {
		for _, r := range routes {
			if !r.Destination.Contains(ip) {
				continue
			}
			ones, _ := r.Destination.Mask.Size()
			if ones > bestMaskLen {
				bestMatch = r
				bestMaskLen = ones
			} else if ones == bestMaskLen && bestMatch != nil {
				bestMatch = rt.bestPath(bestMatch, r)
			}
		}
	}

	return bestMatch, bestMatch != nil
}

// GC removes routes older than the given TTL.
func (rt *RoutingTable) GC(ttl time.Duration) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	removed := 0
	now := time.Now()
	for key, routes := range rt.routes {
		filtered := routes[:0]
		for _, r := range routes {
			if now.Sub(r.Timestamp) < ttl {
				filtered = append(filtered, r)
			} else {
				removed++
			}
		}
		if len(filtered) == 0 {
			delete(rt.routes, key)
		} else {
			rt.routes[key] = filtered
		}
	}
	return removed
}

// GetAllRoutes returns all known routes (for advertisement).
func (rt *RoutingTable) GetAllRoutes() []*Route {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var result []*Route
	for _, routes := range rt.routes {
		for _, r := range routes {
			result = append(result, r)
		}
	}
	return result
}

// Size returns the total number of routes in the table.
func (rt *RoutingTable) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	count := 0
	for _, routes := range rt.routes {
		count += len(routes)
	}
	return count
}

// bestPath selects the better of two routes to the same prefix.
// Selection order: LocalPref > shorter ASPath > lower Cost > older route.
func (rt *RoutingTable) bestPath(a, b *Route) *Route {
	// 1. Highest LocalPref wins
	if a.LocalPref > b.LocalPref {
		return a
	}
	if b.LocalPref > a.LocalPref {
		return b
	}
	// 2. Shortest AS-Path wins
	if a.ASPath.Len() < b.ASPath.Len() {
		return a
	}
	if b.ASPath.Len() < a.ASPath.Len() {
		return b
	}
	// 3. Lowest Cost wins
	if a.Cost < b.Cost {
		return a
	}
	if b.Cost < a.Cost {
		return b
	}
	// 4. Oldest route wins (stability)
	if a.Timestamp.Before(b.Timestamp) {
		return a
	}
	return b
}
