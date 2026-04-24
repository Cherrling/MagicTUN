package noise

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"io"
)

// AuthMessage is exchanged over the control stream after TLS is established
// to verify peer identity using Ed25519 keys.
type AuthMessage struct {
	PubKey    [32]byte
	Signature [64]byte // sign("magictun-auth" || tlsChannelBinding)
}

// AuthHandshake performs mutual authentication over the control stream.
// tlsChannelBinding is a unique value derived from the TLS session
// (e.g. the TLS finished message hash) to bind the auth to the channel.
func AuthHandshake(rw io.ReadWriter, localKey ed25519.PrivateKey, expectedPub ed25519.PublicKey, tlsChannelBinding []byte) ([]byte, error) {
	localPub := localKey.Public().(ed25519.PublicKey)

	// Build local auth message
	localMsg := AuthMessage{}
	copy(localMsg.PubKey[:], localPub)
	toSign := buildAuthPayload(localPub, tlsChannelBinding)
	sig := ed25519.Sign(localKey, toSign)
	copy(localMsg.Signature[:], sig)

	// Exchange: write local, read remote
	errCh := make(chan error, 1)
	go func() {
		_, err := rw.Write(localMsg.PubKey[:])
		if err != nil {
			errCh <- err
			return
		}
		_, err = rw.Write(localMsg.Signature[:])
		errCh <- err
	}()

	var remoteMsg AuthMessage
	if _, err := io.ReadFull(rw, remoteMsg.PubKey[:]); err != nil {
		return nil, fmt.Errorf("read remote pubkey: %w", err)
	}
	if _, err := io.ReadFull(rw, remoteMsg.Signature[:]); err != nil {
		return nil, fmt.Errorf("read remote signature: %w", err)
	}

	if err := <-errCh; err != nil {
		return nil, fmt.Errorf("write auth: %w", err)
	}

	// Verify remote signature
	toVerify := buildAuthPayload(remoteMsg.PubKey[:], tlsChannelBinding)
	if !ed25519.Verify(remoteMsg.PubKey[:], toVerify, remoteMsg.Signature[:]) {
		return nil, fmt.Errorf("remote auth signature verification failed")
	}

	// If we expect a specific peer, verify the public key matches
	if len(expectedPub) > 0 {
		if !bytes.Equal(remoteMsg.PubKey[:], expectedPub) {
			return nil, fmt.Errorf("peer public key mismatch: expected %x, got %x", expectedPub, remoteMsg.PubKey)
		}
	}

	return remoteMsg.PubKey[:], nil
}

func buildAuthPayload(pubKey, channelBinding []byte) []byte {
	h := sha256.New()
	h.Write([]byte("magictun-auth-v1"))
	h.Write(pubKey)
	h.Write(channelBinding)
	return h.Sum(nil)
}

// TLSSessionBinding derives a channel binding value from the TLS connection state.
// Uses the TLS server name and cipher suite info.
func TLSSessionBinding(serverName string, peerCertificates [][]byte) []byte {
	h := sha256.New()
	h.Write([]byte(serverName))
	for _, cert := range peerCertificates {
		h.Write(cert)
	}
	return h.Sum(nil)
}
