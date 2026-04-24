package wire

import (
	"crypto/ed25519"
	"errors"
)

const sigSize = ed25519.SignatureSize // 64 bytes

// SignMessage signs an encoded message and appends the signature.
// Returns the message with signature appended.
func SignMessage(msg []byte, priv ed25519.PrivateKey) []byte {
	sig := ed25519.Sign(priv, msg)
	return append(msg, sig...)
}

// VerifyMessage verifies and strips the signature from a signed message.
// Returns the original message without the signature.
func VerifyMessage(data []byte, pub ed25519.PublicKey) ([]byte, error) {
	if len(data) < sigSize {
		return nil, errors.New("message too short for signature")
	}
	msg := data[:len(data)-sigSize]
	sig := data[len(data)-sigSize:]
	if !ed25519.Verify(pub, msg, sig) {
		return nil, errors.New("signature verification failed")
	}
	return msg, nil
}
