package forward

import (
	"net"
	"sync"
	"time"

	"github.com/example/magictun/identity"
)

// UDPSession tracks a single UDP "connection" flowing through the relay.
type UDPSession struct {
	ID         uint32
	ClientAddr *net.UDPAddr
	TargetAddr *net.UDPAddr
	NextHop    identity.NodeID
	CreatedAt  time.Time
	LastUsed   time.Time
}

// SessionTable maps session IDs to session metadata.
type SessionTable struct {
	mu       sync.RWMutex
	sessions map[uint32]*UDPSession
	nextID   uint32
	ttl      time.Duration
}

// NewSessionTable creates a new session table.
func NewSessionTable(ttl time.Duration) *SessionTable {
	return &SessionTable{
		sessions: make(map[uint32]*UDPSession),
		nextID:   1,
		ttl:      ttl,
	}
}

// Create creates a new UDP session.
func (st *SessionTable) Create(clientAddr, targetAddr *net.UDPAddr, nextHop identity.NodeID) *UDPSession {
	st.mu.Lock()
	defer st.mu.Unlock()

	id := st.nextID
	st.nextID++

	s := &UDPSession{
		ID:         id,
		ClientAddr: clientAddr,
		TargetAddr: targetAddr,
		NextHop:    nextHop,
		CreatedAt:  time.Now(),
		LastUsed:   time.Now(),
	}
	st.sessions[id] = s
	return s
}

// Lookup finds a session by ID.
func (st *SessionTable) Lookup(id uint32) (*UDPSession, bool) {
	st.mu.RLock()
	s, ok := st.sessions[id]
	st.mu.RUnlock()
	return s, ok
}

// Touch updates the last-used timestamp.
func (st *SessionTable) Touch(id uint32) {
	st.mu.Lock()
	if s, ok := st.sessions[id]; ok {
		s.LastUsed = time.Now()
	}
	st.mu.Unlock()
}

// Remove deletes a session.
func (st *SessionTable) Remove(id uint32) {
	st.mu.Lock()
	delete(st.sessions, id)
	st.mu.Unlock()
}

// GC removes expired sessions.
func (st *SessionTable) GC() {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	for id, s := range st.sessions {
		if now.Sub(s.LastUsed) > st.ttl {
			delete(st.sessions, id)
		}
	}
}
