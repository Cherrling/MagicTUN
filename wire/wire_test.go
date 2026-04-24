package wire

import (
	"bytes"
	"net"
	"testing"
)

func TestRouteMessageRoundTrip(t *testing.T) {
	_, nw, _ := net.ParseCIDR("10.1.0.0/16")
	var origin [16]byte
	copy(origin[:], bytes.Repeat([]byte{1}, 16))
	var hop [16]byte
	copy(hop[:], bytes.Repeat([]byte{2}, 16))

	msg := &RouteMessage{
		Flags:     RouteFlagUpdate,
		Prefix:    *nw,
		Cost:      3,
		Origin:    origin,
		LocalPref: 100,
		ASPath:    [][16]byte{hop},
	}

	encoded := EncodeRouteMessage(msg)
	decoded, err := DecodeRouteMessage(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Flags != msg.Flags {
		t.Errorf("flags: got %d, want %d", decoded.Flags, msg.Flags)
	}
	if decoded.Prefix.String() != msg.Prefix.String() {
		t.Errorf("prefix: got %s, want %s", decoded.Prefix, msg.Prefix)
	}
	if decoded.Cost != msg.Cost {
		t.Errorf("cost: got %d, want %d", decoded.Cost, msg.Cost)
	}
	if decoded.Origin != msg.Origin {
		t.Errorf("origin mismatch")
	}
	if decoded.LocalPref != msg.LocalPref {
		t.Errorf("localpref: got %d, want %d", decoded.LocalPref, msg.LocalPref)
	}
	if len(decoded.ASPath) != 1 || decoded.ASPath[0] != hop {
		t.Errorf("aspath mismatch")
	}
}

func TestRouteMessageIPv6(t *testing.T) {
	_, nw, _ := net.ParseCIDR("2001:db8::/32")
	var origin [16]byte
	copy(origin[:], bytes.Repeat([]byte{0xaa}, 16))

	msg := &RouteMessage{
		Flags:     RouteFlagUpdate,
		Prefix:    *nw,
		Cost:      1,
		Origin:    origin,
		LocalPref: 50,
	}

	encoded := EncodeRouteMessage(msg)
	decoded, err := DecodeRouteMessage(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Prefix.String() != msg.Prefix.String() {
		t.Errorf("prefix: got %s, want %s", decoded.Prefix, msg.Prefix)
	}
}

func TestRouteMessageWithdraw(t *testing.T) {
	_, nw, _ := net.ParseCIDR("192.168.0.0/24")
	var origin [16]byte

	msg := &RouteMessage{
		Flags:  RouteFlagWithdraw,
		Prefix: *nw,
		Origin: origin,
	}

	encoded := EncodeRouteMessage(msg)
	decoded, err := DecodeRouteMessage(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Flags != RouteFlagWithdraw {
		t.Errorf("flags: got %d, want %d", decoded.Flags, RouteFlagWithdraw)
	}
}

func TestRouteMessageDecodeErrors(t *testing.T) {
	// Too short
	_, err := DecodeRouteMessage([]byte{MsgRouteUpdate, 0, 24})
	if err == nil {
		t.Error("expected error for short message")
	}

	// Wrong type
	_, err = DecodeRouteMessage([]byte{0xFF, 0, 24, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestTCPForwardHeaderRoundTrip(t *testing.T) {
	hdr := &TCPForwardHeader{
		Flags:      TCPFlagNewStream,
		StreamID:   42,
		TargetAddr: net.ParseIP("10.0.0.1"),
		TargetPort: 8080,
	}

	encoded := EncodeTCPForwardHeader(hdr)
	decoded, consumed, err := DecodeTCPForwardHeader(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if consumed != len(encoded) {
		t.Errorf("consumed %d, want %d", consumed, len(encoded))
	}
	if decoded.Flags != hdr.Flags {
		t.Errorf("flags mismatch")
	}
	if decoded.StreamID != hdr.StreamID {
		t.Errorf("streamID: got %d, want %d", decoded.StreamID, hdr.StreamID)
	}
	if !decoded.TargetAddr.Equal(hdr.TargetAddr) {
		t.Errorf("addr: got %s, want %s", decoded.TargetAddr, hdr.TargetAddr)
	}
	if decoded.TargetPort != hdr.TargetPort {
		t.Errorf("port: got %d, want %d", decoded.TargetPort, hdr.TargetPort)
	}
}

func TestTCPForwardHeaderIPv6(t *testing.T) {
	hdr := &TCPForwardHeader{
		Flags:      TCPFlagNewStream,
		StreamID:   1,
		TargetAddr: net.ParseIP("::1"),
		TargetPort: 443,
	}

	encoded := EncodeTCPForwardHeader(hdr)
	decoded, _, err := DecodeTCPForwardHeader(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if !decoded.TargetAddr.Equal(hdr.TargetAddr) {
		t.Errorf("addr: got %s, want %s", decoded.TargetAddr, hdr.TargetAddr)
	}
}

func TestTCPCloseRoundTrip(t *testing.T) {
	encoded := EncodeTCPClose(12345)
	id, err := DecodeTCPClose(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if id != 12345 {
		t.Errorf("got %d, want 12345", id)
	}
}

func TestUDPForwardRoundTrip(t *testing.T) {
	payload := []byte("hello udp")
	encoded := EncodeUDPForward(99, payload)
	hdr, decodedPayload, err := DecodeUDPForward(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if hdr.SessionID != 99 {
		t.Errorf("sessionID: got %d, want 99", hdr.SessionID)
	}
	if !bytes.Equal(decodedPayload, payload) {
		t.Errorf("payload mismatch")
	}
}

func TestUDPForwardDecodeErrors(t *testing.T) {
	_, _, err := DecodeUDPForward([]byte{MsgUDPForward})
	if err == nil {
		t.Error("expected error for short message")
	}
	_, _, err = DecodeUDPForward([]byte{0xFF, 0, 0, 0, 0})
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestUDPSessionCreateRoundTrip(t *testing.T) {
	msg := &UDPSessionCreateMessage{
		RequestID:  7,
		TargetAddr: net.ParseIP("172.16.0.1"),
		TargetPort: 53,
	}

	encoded := EncodeUDPSessionCreate(msg)
	decoded, err := DecodeUDPSessionCreate(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != msg.RequestID {
		t.Errorf("requestID: got %d, want %d", decoded.RequestID, msg.RequestID)
	}
	if !decoded.TargetAddr.Equal(msg.TargetAddr) {
		t.Errorf("addr mismatch")
	}
	if decoded.TargetPort != msg.TargetPort {
		t.Errorf("port mismatch")
	}
}

func TestUDPSessionCloseRoundTrip(t *testing.T) {
	encoded := EncodeUDPSessionClose(0xDEAD)
	id, err := DecodeUDPSessionClose(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if id != 0xDEAD {
		t.Errorf("got %d, want %d", id, 0xDEAD)
	}
}

func TestGossipPushRoundTrip(t *testing.T) {
	var id [16]byte
	copy(id[:], bytes.Repeat([]byte{0x11}, 16))
	var pk [32]byte
	copy(pk[:], bytes.Repeat([]byte{0x22}, 32))

	msg := &GossipPushMessage{
		Peers: []PeerEntry{
			{ID: id, PubKey: pk, State: PeerAlive, Version: 5, Addr: "10.0.0.1:9443"},
			{ID: id, PubKey: pk, State: PeerSuspect, Version: 3, Addr: "10.0.0.2:9443"},
		},
	}

	encoded := EncodeGossipPush(msg)
	decoded, err := DecodeGossipPush(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(decoded.Peers))
	}
	for i, p := range decoded.Peers {
		if p.ID != msg.Peers[i].ID {
			t.Errorf("peer %d: ID mismatch", i)
		}
		if p.PubKey != msg.Peers[i].PubKey {
			t.Errorf("peer %d: PubKey mismatch", i)
		}
		if p.State != msg.Peers[i].State {
			t.Errorf("peer %d: state mismatch", i)
		}
		if p.Version != msg.Peers[i].Version {
			t.Errorf("peer %d: version mismatch", i)
		}
		if p.Addr != msg.Peers[i].Addr {
			t.Errorf("peer %d: addr mismatch", i)
		}
	}
}

func TestGossipPushDecodeErrors(t *testing.T) {
	_, err := DecodeGossipPush([]byte{MsgGossipPush})
	if err == nil {
		t.Error("expected error for short message")
	}
	_, err = DecodeGossipPush([]byte{0xFF, 0, 0})
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestGossipControlMessages(t *testing.T) {
	if len(EncodeGossipPull()) != 1 || EncodeGossipPull()[0] != MsgGossipPull {
		t.Error("gossip pull encoding wrong")
	}
	if len(EncodeGossipAck()) != 1 || EncodeGossipAck()[0] != MsgGossipAck {
		t.Error("gossip ack encoding wrong")
	}
	if len(EncodePing()) != 1 || EncodePing()[0] != MsgPing {
		t.Error("ping encoding wrong")
	}
	if len(EncodePong()) != 1 || EncodePong()[0] != MsgPong {
		t.Error("pong encoding wrong")
	}
}

func TestSignMessageRoundTrip(t *testing.T) {
	_, priv, pub, _ := generateTestKey()
	msg := []byte("test control message")

	signed := SignMessage(msg, priv)
	verified, err := VerifyMessage(signed, pub)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if !bytes.Equal(verified, msg) {
		t.Errorf("message mismatch after verify")
	}
}

func TestVerifyMessageBadSignature(t *testing.T) {
	_, priv, pub, _ := generateTestKey()
	msg := SignMessage([]byte("hello"), priv)
	msg[len(msg)-1] ^= 0xFF // corrupt signature

	_, err := VerifyMessage(msg, pub)
	if err == nil {
		t.Error("expected verification failure")
	}
}

func TestVerifyMessageTooShort(t *testing.T) {
	_, _, pub, _ := generateTestKey()
	_, err := VerifyMessage([]byte{0x01}, pub)
	if err == nil {
		t.Error("expected error for short message")
	}
}

func generateTestKey() ([32]byte, []byte, []byte, error) {
	// We can't import crypto/ed25519 here because of the test package,
	// so generate inline using the identity package approach.
	// For wire tests, use hardcoded test vectors.
	pub := [32]byte{
		0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7,
		0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64, 0x07, 0x3a,
		0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25,
		0xaf, 0x02, 0x1a, 0x68, 0xf7, 0x07, 0x51, 0x1a,
	}
	priv := []byte{
		0x9d, 0x61, 0xb1, 0x9d, 0xef, 0xfd, 0x5a, 0x60,
		0xba, 0x84, 0x4a, 0xf4, 0x92, 0xec, 0x2c, 0xc4,
		0x44, 0x49, 0xc5, 0x69, 0x7b, 0x32, 0x69, 0x19,
		0x70, 0x3b, 0xac, 0x03, 0x1c, 0xae, 0x7f, 0x60,
		0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7,
		0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64, 0x07, 0x3a,
		0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25,
		0xaf, 0x02, 0x1a, 0x68, 0xf7, 0x07, 0x51, 0x1a,
	}
	return pub, priv, pub[:], nil
}
