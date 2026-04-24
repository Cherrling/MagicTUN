package gossip

import (
	"testing"

	"github.com/example/magictun/identity"
)

func testNodeID(s string) identity.NodeID {
	var id identity.NodeID
	copy(id[:], s)
	return id
}

func TestPeerManagerAddOrUpdate(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("peer-0000000001")

	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive})

	peer, ok := pm.Get(id)
	if !ok {
		t.Fatal("peer not found")
	}
	if peer.State != PeerAlive {
		t.Errorf("expected Alive, got %d", peer.State)
	}
	if peer.Addr != "10.0.0.1:9443" {
		t.Errorf("wrong addr: %s", peer.Addr)
	}
}

func TestPeerManagerAddOrUpdateExisting(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("peer-0000000001")

	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.2:9443", State: PeerAlive, Version: 2})

	peer, ok := pm.Get(id)
	if !ok {
		t.Fatal("peer not found")
	}
	if peer.Addr != "10.0.0.2:9443" {
		t.Errorf("expected updated addr, got %s", peer.Addr)
	}
}

func TestPeerManagerAddSelf(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("self-0000000000")

	added := pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive})
	if added {
		t.Error("should not add self")
	}
}

func TestPeerManagerGetAlive(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))

	pm.AddOrUpdate(&Peer{ID: testNodeID("peer-0000000001"), Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.AddOrUpdate(&Peer{ID: testNodeID("peer-0000000002"), Addr: "10.0.0.2:9443", State: PeerAlive, Version: 1})
	pm.MarkSuspect(testNodeID("peer-0000000002"))

	alive := pm.GetAlive()
	if len(alive) != 1 {
		t.Errorf("expected 1 alive peer, got %d", len(alive))
	}
	if alive[0].ID != testNodeID("peer-0000000001") {
		t.Errorf("wrong alive peer: %s", alive[0].ID)
	}
}

func TestPeerManagerMarkDead(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("peer-0000000001")

	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.MarkDead(id)

	peer, ok := pm.Get(id)
	if !ok {
		t.Fatal("dead peer should still be in the manager")
	}
	if peer.State != PeerDead {
		t.Errorf("expected Dead, got %d", peer.State)
	}

	alive := pm.GetAlive()
	if len(alive) != 0 {
		t.Errorf("expected 0 alive, got %d", len(alive))
	}
}

func TestPeerManagerMarkAlive(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("peer-0000000001")

	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.MarkSuspect(id)
	pm.MarkAlive(id)

	peer, ok := pm.Get(id)
	if !ok {
		t.Fatal("peer not found")
	}
	if peer.State != PeerAlive {
		t.Errorf("expected Alive, got %d", peer.State)
	}
}

func TestPeerManagerGetAll(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))

	pm.AddOrUpdate(&Peer{ID: testNodeID("peer-0000000001"), Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.AddOrUpdate(&Peer{ID: testNodeID("peer-0000000002"), Addr: "10.0.0.2:9443", State: PeerAlive, Version: 1})

	all := pm.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 peers, got %d", len(all))
	}
}

func TestPeerManagerRemove(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("peer-0000000001")

	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.Remove(id)

	_, ok := pm.Get(id)
	if ok {
		t.Error("peer should have been removed")
	}
}

func TestPeerManagerConcurrentAccess(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			id := testNodeID("peer-" + string(rune('0'+n)) + "000000000")
			for j := 0; j < 100; j++ {
				pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive, Version: uint64(j)})
				pm.Get(id)
				pm.GetAlive()
				pm.GetAll()
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestPeerPubKey(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))
	id := testNodeID("peer-0000000001")
	pubKey := make([]byte, 32)
	for i := range pubKey {
		pubKey[i] = byte(i)
	}

	pm.AddOrUpdate(&Peer{ID: id, Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1, PubKey: pubKey})

	peer, ok := pm.Get(id)
	if !ok {
		t.Fatal("peer not found")
	}
	if len(peer.PubKey) != 32 {
		t.Errorf("expected 32-byte pubkey, got %d", len(peer.PubKey))
	}
}

func TestPeerCount(t *testing.T) {
	pm := NewPeerManager(testNodeID("self-0000000000"))

	pm.AddOrUpdate(&Peer{ID: testNodeID("peer-0000000001"), Addr: "10.0.0.1:9443", State: PeerAlive, Version: 1})
	pm.AddOrUpdate(&Peer{ID: testNodeID("peer-0000000002"), Addr: "10.0.0.2:9443", State: PeerSuspect, Version: 1})

	if c := pm.Count(PeerAlive); c != 1 {
		t.Errorf("expected 1 alive, got %d", c)
	}
	if c := pm.Count(PeerSuspect); c != 1 {
		t.Errorf("expected 1 suspect, got %d", c)
	}
}
