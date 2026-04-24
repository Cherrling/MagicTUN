package wire

import (
	"encoding/binary"
	"fmt"
	"net"
)

// RouteMessage is a path-vector route advertisement.
type RouteMessage struct {
	Flags     uint8 // bit 0: 0=update, 1=withdraw
	Prefix    net.IPNet
	Cost      uint32
	Origin    [NodeIDSize]byte
	LocalPref uint32
	ASPath    [][NodeIDSize]byte
}

const (
	RouteFlagUpdate   = 0x00
	RouteFlagWithdraw = 0x01
)

// EncodeRouteMessage encodes a route message to bytes.
func EncodeRouteMessage(msg *RouteMessage) []byte {
	ones, _ := msg.Prefix.Mask.Size()
	isV4 := msg.Prefix.IP.To4() != nil
	addrLen := 4
	if !isV4 {
		addrLen = 16
	}
	// type(1) + flags(1) + prefixLen(1) + addrFamily(1) + prefix(4 or 16) + cost(4) +
	// origin(16) + localPref(4) + asPathLen(2) + asPath(N*16)
	size := 1 + 1 + 1 + 1 + addrLen + 4 + 16 + 4 + 2 + len(msg.ASPath)*NodeIDSize
	buf := make([]byte, size)
	off := 0

	buf[off] = MsgRouteUpdate
	off++
	buf[off] = msg.Flags
	off++
	buf[off] = byte(ones)
	off++
	if isV4 {
		buf[off] = 4 // IPv4
		off++
		copy(buf[off:], msg.Prefix.IP.To4())
		off += 4
	} else {
		buf[off] = 6 // IPv6
		off++
		copy(buf[off:], msg.Prefix.IP.To16())
		off += 16
	}

	binary.BigEndian.PutUint32(buf[off:], msg.Cost)
	off += 4
	copy(buf[off:], msg.Origin[:])
	off += 16
	binary.BigEndian.PutUint32(buf[off:], msg.LocalPref)
	off += 4
	binary.BigEndian.PutUint16(buf[off:], uint16(len(msg.ASPath)))
	off += 2
	for _, hop := range msg.ASPath {
		copy(buf[off:], hop[:])
		off += NodeIDSize
	}
	return buf
}

// DecodeRouteMessage decodes a route message from bytes.
func DecodeRouteMessage(data []byte) (*RouteMessage, error) {
	if len(data) < 1+1+1+1+4+4+16+4+2 {
		return nil, fmt.Errorf("route message too short: %d bytes", len(data))
	}
	off := 0

	if data[off] != MsgRouteUpdate {
		return nil, fmt.Errorf("unexpected message type: 0x%02x", data[off])
	}
	off++

	msg := &RouteMessage{}
	msg.Flags = data[off]
	off++

	ones := int(data[off])
	off++

	addrFamily := data[off]
	off++

	var addrLen int
	var ip net.IP
	switch addrFamily {
	case 4:
		addrLen = 4
		if off+4 > len(data) {
			return nil, fmt.Errorf("route message truncated at IPv4 prefix")
		}
		ip = net.IPv4(data[off], data[off+1], data[off+2], data[off+3])
		msg.Prefix = net.IPNet{IP: ip, Mask: net.CIDRMask(ones, 32)}
	case 6:
		addrLen = 16
		if off+16 > len(data) {
			return nil, fmt.Errorf("route message truncated at IPv6 prefix")
		}
		ip = make(net.IP, 16)
		copy(ip, data[off:off+16])
		msg.Prefix = net.IPNet{IP: ip, Mask: net.CIDRMask(ones, 128)}
	default:
		return nil, fmt.Errorf("unknown address family: %d", addrFamily)
	}
	off += addrLen

	msg.Cost = binary.BigEndian.Uint32(data[off:])
	off += 4
	copy(msg.Origin[:], data[off:off+16])
	off += 16
	msg.LocalPref = binary.BigEndian.Uint32(data[off:])
	off += 4

	asPathLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if asPathLen > 32 {
		return nil, fmt.Errorf("AS path too long: %d", asPathLen)
	}
	msg.ASPath = make([][NodeIDSize]byte, asPathLen)
	for i := 0; i < asPathLen; i++ {
		if off+NodeIDSize > len(data) {
			return nil, fmt.Errorf("route message truncated at AS path entry %d", i)
		}
		copy(msg.ASPath[i][:], data[off:off+NodeIDSize])
		off += NodeIDSize
	}
	return msg, nil
}
