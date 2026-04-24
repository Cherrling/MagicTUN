package wire

import (
	"encoding/binary"
	"fmt"
	"net"
)

// TCPForwardHeader is sent on a new QUIC stream for TCP relay.
type TCPForwardHeader struct {
	Flags      uint8 // bit 0: 1=new stream, 0=data
	StreamID   uint32
	TargetAddr net.IP
	TargetPort uint16
}

const (
	TCPFlagNewStream = 0x01
	TCPFlagData      = 0x00
)

// EncodeTCPForwardHeader encodes a TCP forward header.
func EncodeTCPForwardHeader(hdr *TCPForwardHeader) []byte {
	// type(1) + flags(1) + streamID(4) + addrLen(1) + addr(4 or 16) + port(2)
	addrLen := 4
	if hdr.TargetAddr.To4() == nil {
		addrLen = 16
	}
	size := 1 + 1 + 4 + 1 + addrLen + 2
	buf := make([]byte, size)
	off := 0

	buf[off] = MsgTCPForward
	off++
	buf[off] = hdr.Flags
	off++
	binary.BigEndian.PutUint32(buf[off:], hdr.StreamID)
	off += 4
	buf[off] = byte(addrLen)
	off++
	if addrLen == 4 {
		copy(buf[off:], hdr.TargetAddr.To4())
	} else {
		copy(buf[off:], hdr.TargetAddr.To16())
	}
	off += addrLen
	binary.BigEndian.PutUint16(buf[off:], hdr.TargetPort)
	return buf
}

// DecodeTCPForwardHeader decodes a TCP forward header.
// Returns the header, the number of bytes consumed, and any error.
func DecodeTCPForwardHeader(data []byte) (*TCPForwardHeader, int, error) {
	if len(data) < 1+1+4+1+4+2 {
		return nil, 0, fmt.Errorf("TCP forward header too short: %d", len(data))
	}
	off := 0
	if data[off] != MsgTCPForward {
		return nil, 0, fmt.Errorf("unexpected message type: 0x%02x", data[off])
	}
	off++

	hdr := &TCPForwardHeader{}
	hdr.Flags = data[off]
	off++
	hdr.StreamID = binary.BigEndian.Uint32(data[off:])
	off += 4
	addrLen := int(data[off])
	off++
	if addrLen != 4 && addrLen != 16 {
		return nil, 0, fmt.Errorf("invalid address length: %d", addrLen)
	}
	if off+addrLen+2 > len(data) {
		return nil, 0, fmt.Errorf("TCP forward header truncated at address")
	}
	hdr.TargetAddr = make(net.IP, addrLen)
	copy(hdr.TargetAddr, data[off:off+addrLen])
	off += addrLen
	hdr.TargetPort = binary.BigEndian.Uint16(data[off:])
	off += 2
	return hdr, off, nil
}

// EncodeTCPClose encodes a TCP stream close notification.
func EncodeTCPClose(streamID uint32) []byte {
	buf := make([]byte, 1+4)
	buf[0] = MsgTCPClose
	binary.BigEndian.PutUint32(buf[1:], streamID)
	return buf
}

// DecodeTCPClose decodes a TCP close notification.
func DecodeTCPClose(data []byte) (uint32, error) {
	if len(data) < 5 {
		return 0, fmt.Errorf("TCP close too short")
	}
	return binary.BigEndian.Uint32(data[1:]), nil
}
