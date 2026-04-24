package gossip

import (
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/example/magictun/identity"
)

// PeerState represents the state of a peer.
type PeerState uint8

const (
	PeerAlive   PeerState = 0
	PeerSuspect PeerState = 1
	PeerDead    PeerState = 2
	PeerLeft    PeerState = 3
)

// Peer represents a known node in the network.
type Peer struct {
	ID         identity.NodeID
	PubKey     ed25519.PublicKey // Ed25519 public key (learned via gossip)
	Addr       string            // host:port for transport connection
	State      PeerState
	Version    uint64 // incarnation number
	LastSeen   time.Time
	DirectNets []string // directly connected networks (CIDRs)
}

// PeerManager manages the set of known peers.
type PeerManager struct {
	mu     sync.RWMutex
	peers  map[string]*Peer // keyed by NodeID string
	selfID identity.NodeID
}

// NewPeerManager creates a new peer manager.
func NewPeerManager(selfID identity.NodeID) *PeerManager {
	return &PeerManager{
		peers:  make(map[string]*Peer),
		selfID: selfID,
	}
}

// AddOrUpdate adds a new peer or updates an existing one.
// Returns true if the peer was newly added or updated.
func (pm *PeerManager) AddOrUpdate(p *Peer) bool {
	if p.ID.Equal(pm.selfID) {
		return false
	}
	key := p.ID.String()
	pm.mu.Lock()
	defer pm.mu.Unlock()
	existing, ok := pm.peers[key]
	if !ok {
		cp := p.copy()
		cp.LastSeen = time.Now()
		cp.Version = 1
		pm.peers[key] = cp
		return true
	}
	if p.Version > existing.Version || (p.Version == existing.Version && p.State == PeerAlive) {
		cp := existing.copy()
		cp.State = p.State
		cp.Version = p.Version
		cp.Addr = p.Addr
		cp.LastSeen = time.Now()
		cp.DirectNets = p.DirectNets
		if len(p.PubKey) > 0 {
			cp.PubKey = p.PubKey
		}
		pm.peers[key] = cp
		return true
	}
	return false
}

// Remove removes a peer from the manager.
func (pm *PeerManager) Remove(id identity.NodeID) {
	pm.mu.Lock()
	delete(pm.peers, id.String())
	pm.mu.Unlock()
}

// Get returns a peer by ID.
func (pm *PeerManager) Get(id identity.NodeID) (*Peer, bool) {
	pm.mu.RLock()
	p, ok := pm.peers[id.String()]
	pm.mu.RUnlock()
	return p, ok
}

// GetAlive returns all alive peers.
func (pm *PeerManager) GetAlive() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var result []*Peer
	for _, p := range pm.peers {
		if p.State == PeerAlive {
			result = append(result, p)
		}
	}
	return result
}

// GetAll returns all known peers.
func (pm *PeerManager) GetAll() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	result := make([]*Peer, 0, len(pm.peers))
	for _, p := range pm.peers {
		result = append(result, p)
	}
	return result
}

// MarkSuspect marks a peer as suspect and increments its version.
func (pm *PeerManager) MarkSuspect(id identity.NodeID) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	p, ok := pm.peers[id.String()]
	if !ok {
		return
	}
	if p.State == PeerAlive {
		cp := p.copy()
		cp.State = PeerSuspect
		cp.Version++
		pm.peers[id.String()] = cp
	}
}

// MarkDead marks a peer as dead.
func (pm *PeerManager) MarkDead(id identity.NodeID) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	p, ok := pm.peers[id.String()]
	if !ok {
		return
	}
	cp := p.copy()
	cp.State = PeerDead
	cp.Version++
	pm.peers[id.String()] = cp
}

// MarkAlive marks a peer as alive (e.g., after receiving a pong).
func (pm *PeerManager) MarkAlive(id identity.NodeID) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	p, ok := pm.peers[id.String()]
	if !ok {
		return
	}
	cp := p.copy()
	cp.State = PeerAlive
	cp.LastSeen = time.Now()
	pm.peers[id.String()] = cp
}

// GetRandom returns n random alive peers, excluding the given ID.
func (pm *PeerManager) GetRandom(n int, exclude identity.NodeID) []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var alive []*Peer
	for _, p := range pm.peers {
		if p.State == PeerAlive && !p.ID.Equal(exclude) {
			alive = append(alive, p)
		}
	}
	if len(alive) <= n {
		return alive
	}
	// Fisher-Yates shuffle and take first n
	result := make([]*Peer, len(alive))
	copy(result, alive)
	for i := len(result) - 1; i > 0; i-- {
		j := int(fastRand() % uint32(i+1))
		result[i], result[j] = result[j], result[i]
	}
	return result[:n]
}

// Count returns the number of peers in a given state.
func (pm *PeerManager) Count(state PeerState) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	count := 0
	for _, p := range pm.peers {
		if p.State == state {
			count++
		}
	}
	return count
}

func (p *Peer) copy() *Peer {
	cp := &Peer{
		ID:       p.ID,
		Addr:     p.Addr,
		State:    p.State,
		Version:  p.Version,
		LastSeen: p.LastSeen,
	}
	if len(p.PubKey) > 0 {
		cp.PubKey = make([]byte, len(p.PubKey))
		copy(cp.PubKey, p.PubKey)
	}
	if len(p.DirectNets) > 0 {
		cp.DirectNets = make([]string, len(p.DirectNets))
		copy(cp.DirectNets, p.DirectNets)
	}
	return cp
}

var (
	rngMu    sync.Mutex
	rngState uint32 = 1
)

func fastRand() uint32 {
	rngMu.Lock()
	rngState ^= rngState << 13
	rngState ^= rngState >> 17
	rngState ^= rngState << 5
	v := rngState
	rngMu.Unlock()
	return v
}
