package forward

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

func TestSessionTableCreate(t *testing.T) {
	st := NewSessionTable(120 * time.Second)
	s := st.Create(nil, nil, testNodeID("peer-0000000001"))

	if s.ID == 0 {
		t.Error("session ID should not be zero")
	}
	if !s.NextHop.Equal(testNodeID("peer-0000000001")) {
		t.Error("wrong next hop")
	}
	if s.CreatedAt.IsZero() {
		t.Error("created at should be set")
	}
}

func TestSessionTableLookup(t *testing.T) {
	st := NewSessionTable(120 * time.Second)
	s1 := st.Create(nil, nil, testNodeID("peer-0000000001"))

	s2, ok := st.Lookup(s1.ID)
	if !ok {
		t.Fatal("session not found")
	}
	if s2.ID != s1.ID {
		t.Errorf("ID mismatch: got %d, want %d", s2.ID, s1.ID)
	}

	_, ok = st.Lookup(99999)
	if ok {
		t.Error("nonexistent session should not be found")
	}
}

func TestSessionTableCreateWithID(t *testing.T) {
	st := NewSessionTable(120 * time.Second)

	s := st.CreateWithID(42, nil, nil, testNodeID("peer-0000000001"))
	if s.ID != 42 {
		t.Errorf("expected ID 42, got %d", s.ID)
	}

	s2, ok := st.Lookup(42)
	if !ok {
		t.Fatal("session not found")
	}
	if s2.ID != 42 {
		t.Errorf("expected ID 42, got %d", s2.ID)
	}
}

func TestSessionTableTouch(t *testing.T) {
	st := NewSessionTable(120 * time.Second)
	s := st.Create(nil, nil, testNodeID("peer-0000000001"))
	oldLastUsed := s.LastUsed

	time.Sleep(10 * time.Millisecond)
	st.Touch(s.ID)

	s2, _ := st.Lookup(s.ID)
	if !s2.LastUsed.After(oldLastUsed) {
		t.Error("Touch should update LastUsed")
	}
}

func TestSessionTableRemove(t *testing.T) {
	st := NewSessionTable(120 * time.Second)
	s := st.Create(nil, nil, testNodeID("peer-0000000001"))

	st.Remove(s.ID)
	_, ok := st.Lookup(s.ID)
	if ok {
		t.Error("session should have been removed")
	}
}

func TestSessionTableGC(t *testing.T) {
	st := NewSessionTable(50 * time.Millisecond)

	st.Create(nil, nil, testNodeID("peer-0000000001"))

	time.Sleep(100 * time.Millisecond)

	st.GC()

	// All sessions should be expired (TTL 50ms, waited 100ms)
	count := 0
	st.mu.RLock()
	count = len(st.sessions)
	st.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 sessions after GC, got %d", count)
	}
}

func TestSessionTableGCActiveSession(t *testing.T) {
	st := NewSessionTable(100 * time.Millisecond)

	s := st.Create(nil, nil, testNodeID("peer-0000000001"))

	time.Sleep(50 * time.Millisecond)
	st.Touch(s.ID) // refresh

	time.Sleep(30 * time.Millisecond) // total 80ms since create, 30ms since touch

	st.GC()

	_, ok := st.Lookup(s.ID)
	if !ok {
		t.Error("active session should not be GC'd")
	}
}

func TestUDPSessionFields(t *testing.T) {
	clientAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	targetAddr := &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 53}

	st := NewSessionTable(120 * time.Second)
	s := st.Create(clientAddr, targetAddr, testNodeID("peer-0000000001"))

	if s.ClientAddr.String() != clientAddr.String() {
		t.Errorf("client addr mismatch: %s != %s", s.ClientAddr, clientAddr)
	}
	if s.TargetAddr.String() != targetAddr.String() {
		t.Errorf("target addr mismatch: %s != %s", s.TargetAddr, targetAddr)
	}
}
