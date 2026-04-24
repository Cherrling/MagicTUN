package route

import (
	"github.com/example/magictun/identity"
)

// ASPath is an ordered list of node IDs representing the path to a destination.
// Used for loop detection (like BGP AS-Path).
type ASPath struct {
	Hops []identity.NodeID
}

// NewASPath creates a new AS path.
func NewASPath() *ASPath {
	return &ASPath{}
}

// AddHop prepends a node ID to the path.
func (p *ASPath) AddHop(id identity.NodeID) {
	p.Hops = append([]identity.NodeID{id}, p.Hops...)
}

// Contains returns true if the path contains the given node ID.
func (p *ASPath) Contains(id identity.NodeID) bool {
	for _, hop := range p.Hops {
		if hop.Equal(id) {
			return true
		}
	}
	return false
}

// Len returns the path length (number of hops).
func (p *ASPath) Len() int {
	return len(p.Hops)
}

// Copy returns a deep copy of the path.
func (p *ASPath) Copy() *ASPath {
	newPath := &ASPath{Hops: make([]identity.NodeID, len(p.Hops))}
	copy(newPath.Hops, p.Hops)
	return newPath
}

// CloneWithHop returns a new path with the given ID prepended.
func (p *ASPath) CloneWithHop(id identity.NodeID) *ASPath {
	newPath := &ASPath{Hops: make([]identity.NodeID, len(p.Hops)+1)}
	newPath.Hops[0] = id
	copy(newPath.Hops[1:], p.Hops)
	return newPath
}

// ToWire converts to wire format (fixed-size arrays).
func (p *ASPath) ToWire() [][16]byte {
	result := make([][16]byte, len(p.Hops))
	for i, hop := range p.Hops {
		copy(result[i][:], hop[:])
	}
	return result
}

// ASPathFromWire converts from wire format.
func ASPathFromWire(wire [][16]byte) *ASPath {
	path := &ASPath{Hops: make([]identity.NodeID, len(wire))}
	for i, w := range wire {
		copy(path.Hops[i][:], w[:])
	}
	return path
}
