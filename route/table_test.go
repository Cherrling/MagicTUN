package route

import (
	"net"
	"testing"
	"time"

	"github.com/example/magictun/identity"
)

func testNodeID(s string) identity.NodeID {
	var id identity.NodeID
	copy(id[:], s)
	return id
}

func testRoute(dest string, origin string, cost uint32, localPref uint32) *Route {
	_, nw, _ := net.ParseCIDR(dest)
	return &Route{
		Destination: *nw,
		Origin:      testNodeID(origin),
		Cost:        cost,
		LocalPref:   localPref,
		Timestamp:   time.Now(),
		ASPath:      NewASPath(),
	}
}

func TestRoutingTableAddAndLookup(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))
	r := testRoute("10.1.0.0/16", "origin-00000001", 1, 100)
	rt.AddRoute(r)

	found, ok := rt.Lookup(net.ParseIP("10.1.1.1"))
	if !ok {
		t.Fatal("route not found")
	}
	if found.Destination.String() != "10.1.0.0/16" {
		t.Errorf("wrong destination: %s", found.Destination)
	}
	if found.Cost != 1 {
		t.Errorf("wrong cost: %d", found.Cost)
	}
}

func TestRoutingTableLongestPrefixMatch(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))
	rt.AddRoute(testRoute("10.0.0.0/8", "origin-00000001", 1, 100))
	rt.AddRoute(testRoute("10.1.0.0/16", "origin-00000002", 1, 100))
	rt.AddRoute(testRoute("10.1.1.0/24", "origin-00000003", 1, 100))

	found, ok := rt.Lookup(net.ParseIP("10.1.1.50"))
	if !ok {
		t.Fatal("route not found")
	}
	if found.Destination.String() != "10.1.1.0/24" {
		t.Errorf("expected longest prefix 10.1.1.0/24, got %s", found.Destination)
	}
}

func TestRoutingTableBestPath(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	r1 := testRoute("10.0.0.0/8", "origin-00000001", 5, 100)
	rt.AddRoute(r1)

	r2 := testRoute("10.0.0.0/8", "origin-00000002", 1, 100)
	rt.AddRoute(r2)

	found, ok := rt.Lookup(net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatal("route not found")
	}
	if found.Origin != testNodeID("origin-00000002") {
		t.Errorf("expected origin-00000002 (lower cost), got %s", found.Origin)
	}
}

func TestRoutingTableBestPathLocalPref(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	r1 := testRoute("10.0.0.0/8", "origin-00000001", 1, 200)
	rt.AddRoute(r1)

	r2 := testRoute("10.0.0.0/8", "origin-00000002", 1, 100)
	rt.AddRoute(r2)

	found, ok := rt.Lookup(net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatal("route not found")
	}
	if found.Origin != testNodeID("origin-00000001") {
		t.Errorf("expected origin-00000001 (higher localpref), got %s", found.Origin)
	}
}

func TestRoutingTableBestPathASPath(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	r1 := testRoute("10.0.0.0/8", "origin-00000001", 1, 100)
	r1.ASPath.AddHop(testNodeID("hop-aaaaaaaaaa"))
	r1.ASPath.AddHop(testNodeID("hop-bbbbbbbbbb"))
	rt.AddRoute(r1)

	r2 := testRoute("10.0.0.0/8", "origin-00000002", 1, 100)
	r2.ASPath.AddHop(testNodeID("hop-aaaaaaaaaa"))
	rt.AddRoute(r2)

	found, ok := rt.Lookup(net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatal("route not found")
	}
	if found.Origin != testNodeID("origin-00000002") {
		t.Errorf("expected origin-00000002 (shorter AS path), got %s", found.Origin)
	}
}

func TestRoutingTableRemoveRoute(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	_, nw, _ := net.ParseCIDR("10.0.0.0/8")
	r := testRoute("10.0.0.0/8", "origin-00000001", 1, 100)
	rt.AddRoute(r)

	rt.RemoveRoute(*nw, testNodeID("origin-00000001"))

	_, ok := rt.Lookup(net.ParseIP("10.0.0.1"))
	if ok {
		t.Error("route should have been removed")
	}
}

func TestRoutingTableRemoveAllFromOrigin(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	rt.AddRoute(testRoute("10.0.0.0/8", "origin-00000001", 1, 100))
	rt.AddRoute(testRoute("192.168.0.0/16", "origin-00000001", 1, 100))
	rt.AddRoute(testRoute("172.16.0.0/12", "origin-00000002", 1, 100))

	rt.RemoveAllFromOrigin(testNodeID("origin-00000001"))

	if rt.Size() != 1 {
		t.Errorf("expected 1 remaining route, got %d", rt.Size())
	}

	_, ok := rt.Lookup(net.ParseIP("172.16.0.1"))
	if !ok {
		t.Error("route from origin-00000002 should still exist")
	}
}

func TestRoutingTableGC(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	rt.AddRoute(testRoute("10.0.0.0/8", "origin-00000001", 1, 100))
	rt.AddRoute(testRoute("192.168.0.0/16", "origin-00000002", 1, 100))

	// Manually age one route by accessing the internal state
	rt.mu.Lock()
	for _, routes := range rt.routes {
		for _, r := range routes {
			if r.Origin == testNodeID("origin-00000001") {
				r.Timestamp = time.Now().Add(-2 * time.Hour)
			}
		}
	}
	rt.mu.Unlock()

	removed := rt.GC(1 * time.Hour)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if rt.Size() != 1 {
		t.Errorf("expected 1 route after GC, got %d", rt.Size())
	}
}

func TestRoutingTableDirectNetworks(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	_, nw1, _ := net.ParseCIDR("10.1.0.0/24")
	_, nw2, _ := net.ParseCIDR("10.2.0.0/24")
	rt.SetDirectNetworks([]net.IPNet{*nw1, *nw2})

	nets := rt.GetDirectNetworks()
	if len(nets) != 2 {
		t.Fatalf("expected 2 direct networks, got %d", len(nets))
	}

	route, ok := rt.Lookup(net.ParseIP("10.1.0.5"))
	if !ok {
		t.Fatal("direct network not found")
	}
	if !route.NextHop.IsZero() {
		t.Errorf("direct network should have zero NextHop, got %s", route.NextHop)
	}
}

func TestRoutingTableASPathLoopDetection(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	r := testRoute("10.0.0.0/8", "origin-00000001", 1, 100)
	r.ASPath.AddHop(testNodeID("self-0000000000")) // contains self

	added := rt.AddRoute(r)
	if added {
		t.Error("route with self in AS path should be rejected")
	}
}

func TestRoutingTableGetAllRoutes(t *testing.T) {
	rt := NewRoutingTable(testNodeID("self-0000000000"))

	rt.AddRoute(testRoute("10.0.0.0/8", "origin-00000001", 1, 100))
	rt.AddRoute(testRoute("192.168.0.0/16", "origin-00000002", 1, 100))

	routes := rt.GetAllRoutes()
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
}
