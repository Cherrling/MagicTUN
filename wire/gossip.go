package wire

import (
	"encoding/binary"
	"fmt"
)

// PeerState represents the state of a peer in gossip.
type PeerState uint8

const (
	PeerAlive   PeerState = 0
	PeerSuspect PeerState = 1
	PeerDead    PeerState = 2
	PeerLeft    PeerState = 3
)

// PeerEntry is a single peer in a gossip push message.
type PeerEntry struct {
	ID      [NodeIDSize]byte
	State   PeerState
	Version uint64
	Addr    string // host:port
}

// GossipPushMessage is a full gossip state push.
type GossipPushMessage struct {
	Peers []PeerEntry
}

// EncodeGossipPush encodes a gossip push message.
func EncodeGossipPush(msg *GossipPushMessage) []byte {
	size := 1 + 2 // type + count
	for _, p := range msg.Peers {
		size += NodeIDSize + 1 + 8 + 1 + len(p.Addr)
	}
	buf := make([]byte, size)
	off := 0

	buf[off] = MsgGossipPush
	off++
	binary.BigEndian.PutUint16(buf[off:], uint16(len(msg.Peers)))
	off += 2

	for _, p := range msg.Peers {
		copy(buf[off:], p.ID[:])
		off += NodeIDSize
		buf[off] = byte(p.State)
		off++
		binary.BigEndian.PutUint64(buf[off:], p.Version)
		off += 8
		buf[off] = byte(len(p.Addr))
		off++
		copy(buf[off:], p.Addr)
		off += len(p.Addr)
	}
	return buf
}

// DecodeGossipPush decodes a gossip push message.
func DecodeGossipPush(data []byte) (*GossipPushMessage, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("gossip push too short")
	}
	off := 0
	if data[off] != MsgGossipPush {
		return nil, fmt.Errorf("unexpected message type: 0x%02x", data[off])
	}
	off++

	count := int(binary.BigEndian.Uint16(data[off:]))
	off += 2

	msg := &GossipPushMessage{Peers: make([]PeerEntry, 0, count)}
	for i := 0; i < count; i++ {
		if off+NodeIDSize+1+8+1 > len(data) {
			return nil, fmt.Errorf("gossip push truncated at peer %d", i)
		}
		var p PeerEntry
		copy(p.ID[:], data[off:off+NodeIDSize])
		off += NodeIDSize
		p.State = PeerState(data[off])
		off++
		p.Version = binary.BigEndian.Uint64(data[off:])
		off += 8
		addrLen := int(data[off])
		off++
		if off+addrLen > len(data) {
			return nil, fmt.Errorf("gossip push addr truncated at peer %d", i)
		}
		p.Addr = string(data[off : off+addrLen])
		off += addrLen
		msg.Peers = append(msg.Peers, p)
	}
	return msg, nil
}

// EncodeGossipPull encodes a gossip pull request (just the type byte).
func EncodeGossipPull() []byte {
	return []byte{MsgGossipPull}
}

// EncodeGossipAck encodes a gossip ack (just the type byte).
func EncodeGossipAck() []byte {
	return []byte{MsgGossipAck}
}

// EncodePing encodes a ping message.
func EncodePing() []byte {
	return []byte{MsgPing}
}

// EncodePong encodes a pong message.
func EncodePong() []byte {
	return []byte{MsgPong}
}
