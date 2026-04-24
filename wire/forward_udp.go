package wire

import (
	"encoding/binary"
	"fmt"
	"net"
)

// UDPForwardHeader is sent as a QUIC datagram for UDP relay.
type UDPForwardHeader struct {
	SessionID uint32
}

// EncodeUDPForward wraps a UDP payload with the forward header.
func EncodeUDPForward(sessionID uint32, payload []byte) []byte {
	buf := make([]byte, 1+4+len(payload))
	buf[0] = MsgUDPForward
	binary.BigEndian.PutUint32(buf[1:], sessionID)
	copy(buf[5:], payload)
	return buf
}

// DecodeUDPForward decodes a UDP forward datagram.
func DecodeUDPForward(data []byte) (*UDPForwardHeader, []byte, error) {
	if len(data) < 5 {
		return nil, nil, fmt.Errorf("UDP forward too short: %d", len(data))
	}
	if data[0] != MsgUDPForward {
		return nil, nil, fmt.Errorf("unexpected message type: 0x%02x", data[0])
	}
	hdr := &UDPForwardHeader{
		SessionID: binary.BigEndian.Uint32(data[1:5]),
	}
	return hdr, data[5:], nil
}

// UDPSessionCreateMessage is sent to establish a UDP session.
type UDPSessionCreateMessage struct {
	RequestID  uint32
	TargetAddr net.IP
	TargetPort uint16
}

// EncodeUDPSessionCreate encodes a UDP session create message.
func EncodeUDPSessionCreate(msg *UDPSessionCreateMessage) []byte {
	addrLen := 4
	if msg.TargetAddr.To4() == nil {
		addrLen = 16
	}
	buf := make([]byte, 1+4+1+addrLen+2)
	off := 0
	buf[off] = MsgUDPSessionCreate
	off++
	binary.BigEndian.PutUint32(buf[off:], msg.RequestID)
	off += 4
	buf[off] = byte(addrLen)
	off++
	if addrLen == 4 {
		copy(buf[off:], msg.TargetAddr.To4())
	} else {
		copy(buf[off:], msg.TargetAddr.To16())
	}
	off += addrLen
	binary.BigEndian.PutUint16(buf[off:], msg.TargetPort)
	return buf
}

// DecodeUDPSessionCreate decodes a UDP session create message.
func DecodeUDPSessionCreate(data []byte) (*UDPSessionCreateMessage, error) {
	if len(data) < 1+4+1+4+2 {
		return nil, fmt.Errorf("UDP session create too short")
	}
	off := 0
	if data[off] != MsgUDPSessionCreate {
		return nil, fmt.Errorf("unexpected message type: 0x%02x", data[off])
	}
	off++
	msg := &UDPSessionCreateMessage{}
	msg.RequestID = binary.BigEndian.Uint32(data[off:])
	off += 4
	addrLen := int(data[off])
	off++
	if addrLen != 4 && addrLen != 16 {
		return nil, fmt.Errorf("invalid address length: %d", addrLen)
	}
	msg.TargetAddr = make(net.IP, addrLen)
	copy(msg.TargetAddr, data[off:off+addrLen])
	off += addrLen
	msg.TargetPort = binary.BigEndian.Uint16(data[off:])
	return msg, nil
}

// UDPSessionCreateAck is the response to a session create.
type UDPSessionCreateAck struct {
	RequestID uint32
	SessionID uint32
}

// EncodeUDPSessionCreateAck encodes a session create ack.
func EncodeUDPSessionCreateAck(msg *UDPSessionCreateAck) []byte {
	buf := make([]byte, 1+4+4)
	buf[0] = MsgUDPSessionCreate
	binary.BigEndian.PutUint32(buf[1:], msg.RequestID)
	binary.BigEndian.PutUint32(buf[5:], msg.SessionID)
	return buf
}

// EncodeUDPSessionClose encodes a session close notification.
func EncodeUDPSessionClose(sessionID uint32) []byte {
	buf := make([]byte, 1+4)
	buf[0] = MsgUDPSessionClose
	binary.BigEndian.PutUint32(buf[1:], sessionID)
	return buf
}

// DecodeUDPSessionClose decodes a session close notification.
func DecodeUDPSessionClose(data []byte) (uint32, error) {
	if len(data) < 5 {
		return 0, fmt.Errorf("UDP session close too short")
	}
	return binary.BigEndian.Uint32(data[1:]), nil
}
