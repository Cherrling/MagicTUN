package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// NodeID is a 16-byte identifier: SHA256(pubkey)[:16].
type NodeID [16]byte

// Identity holds the long-lived Ed25519 keypair and derived NodeID.
type Identity struct {
	PubKey  ed25519.PublicKey
	PrivKey ed25519.PrivateKey
	ID      NodeID
}

// New generates a fresh Ed25519 keypair and derives the NodeID.
func New() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	return fromKeypair(pub, priv), nil
}

// FromSeed creates an Identity from a 32-byte Ed25519 seed.
func FromSeed(seed []byte) *Identity {
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return fromKeypair(pub, priv)
}

func fromKeypair(pub ed25519.PublicKey, priv ed25519.PrivateKey) *Identity {
	sum := sha256.Sum256(pub)
	var id NodeID
	copy(id[:], sum[:16])
	return &Identity{
		PubKey:  pub,
		PrivKey: priv,
		ID:      id,
	}
}

// Sign signs a message with the private key.
func (id *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(id.PrivKey, msg)
}

// Verify checks a signature against this identity's public key.
func (id *Identity) Verify(msg, sig []byte) bool {
	return ed25519.Verify(id.PubKey, msg, sig)
}

// Seed returns the 32-byte seed from the private key.
func (id *Identity) Seed() []byte {
	return id.PrivKey.Seed()
}

// String returns the hex-encoded NodeID.
func (nid NodeID) String() string {
	return hex.EncodeToString(nid[:])
}

// ParseNodeID parses a hex string into a NodeID.
func ParseNodeID(s string) (NodeID, error) {
	var nid NodeID
	b, err := hex.DecodeString(s)
	if err != nil {
		return nid, err
	}
	if len(b) != 16 {
		return nid, errors.New("node id must be 16 bytes (32 hex chars)")
	}
	copy(nid[:], b)
	return nid, nil
}

// Equal reports whether two NodeIDs are equal.
func (nid NodeID) Equal(other NodeID) bool {
	return nid == other
}

// IsZero reports whether the NodeID is all zeros.
func (nid NodeID) IsZero() bool {
	return nid == NodeID{}
}
