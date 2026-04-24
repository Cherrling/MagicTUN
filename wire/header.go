package wire

// Message type constants.
const (
	MsgRouteUpdate      uint8 = 0x01
	MsgRouteWithdraw    uint8 = 0x02
	MsgGossipPush       uint8 = 0x03
	MsgGossipPull       uint8 = 0x04
	MsgGossipAck        uint8 = 0x05
	MsgPing             uint8 = 0x06
	MsgPong             uint8 = 0x07
	MsgTCPForward       uint8 = 0x08
	MsgTCPClose         uint8 = 0x09
	MsgUDPForward       uint8 = 0x0A
	MsgUDPSessionCreate uint8 = 0x0B
	MsgUDPSessionClose  uint8 = 0x0C
)

// NodeIDSize is the size of a node identifier in bytes.
const NodeIDSize = 16
