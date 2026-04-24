package identity

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// PinStore implements Trust-On-First-Use (TOFU) certificate pinning.
// On first connection to a peer, the cert's SPKI fingerprint is stored.
// On subsequent connections, the cert must match the stored fingerprint.
type PinStore struct {
	mu   sync.RWMutex
	pins map[string]string // peerAddr -> hex(sha256(spki))
	path string
}

// NewPinStore creates a new pin store, loading existing pins from path.
func NewPinStore(path string) (*PinStore, error) {
	ps := &PinStore{
		pins: make(map[string]string),
		path: path,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ps, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &ps.pins); err != nil {
		return nil, fmt.Errorf("parse pin store: %w", err)
	}
	return ps, nil
}

// Check verifies a certificate against the stored pin for the given address.
// If no pin exists, the cert fingerprint is stored (TOFU).
// Returns true if the cert is trusted (either new or matched).
func (ps *PinStore) Check(addr string, cert *x509.Certificate) (bool, error) {
	fp := certSPKIHash(cert)

	ps.mu.Lock()
	defer ps.mu.Unlock()

	stored, ok := ps.pins[addr]
	if !ok {
		ps.pins[addr] = fp
		if err := ps.save(); err != nil {
			return false, err
		}
		return true, nil
	}
	if stored != fp {
		return false, fmt.Errorf("cert pin mismatch for %s: expected %s, got %s", addr, stored[:16], fp[:16])
	}
	return true, nil
}

func (ps *PinStore) save() error {
	data, err := json.MarshalIndent(ps.pins, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ps.path, data, 0600)
}

func certSPKIHash(cert *x509.Certificate) string {
	raw, _ := x509.MarshalPKIXPublicKey(cert.PublicKey)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
