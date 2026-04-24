package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

// LoadOrCreate loads an identity from path, or creates a new one if the file
// does not exist, saving it to path.
func LoadOrCreate(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return createAndSave(path)
	}
	seed, err := hex.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("invalid identity file: %w", err)
	}
	if len(seed) != ed25519SeedSize {
		return nil, fmt.Errorf("invalid seed size: expected %d, got %d", ed25519SeedSize, len(seed))
	}
	return FromSeed(seed), nil
}

const ed25519SeedSize = 32

func createAndSave(path string) (*Identity, error) {
	seed := make([]byte, ed25519SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("failed to generate random seed: %w", err)
	}
	id := FromSeed(seed)
	encoded := hex.EncodeToString(seed)
	if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
		return nil, fmt.Errorf("failed to write identity file: %w", err)
	}
	return id, nil
}
