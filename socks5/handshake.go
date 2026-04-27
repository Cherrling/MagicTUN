package socks5

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

const (
	socks5Version  = 0x05
	cmdConnect     = 0x01
	cmdUDPAssociate = 0x03

	authNone     = 0x00
	authNoAccept = 0xFF

	repSuccess         = 0x00
	repGeneralFailure  = 0x01
	repNotAllowed      = 0x02
	repNetUnreachable  = 0x03
	repHostUnreachable = 0x04
	repRefused         = 0x05
	repTTLExpired      = 0x06
	repCmdNotSupported = 0x07
	repAddrNotSupport  = 0x08

	addrTypeIPv4   = 0x01
	addrTypeDomain = 0x03
	addrTypeIPv6   = 0x04
)

// SocksRequest holds a parsed SOCKS5 request.
type SocksRequest struct {
	Cmd        byte
	TargetAddr string
}

func (s *Server) handleHandshake(conn net.Conn) (*SocksRequest, error) {
	// Read auth methods
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}
	if buf[0] != socks5Version {
		return nil, fmt.Errorf("unsupported version: %d", buf[0])
	}
	nmethods := int(buf[1])
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return nil, fmt.Errorf("read methods: %w", err)
	}

	// Method selection: only accept no-auth if client offered it.
	noAuthOffered := false
	for _, m := range methods {
		if m == authNone {
			noAuthOffered = true
			break
		}
	}
	if !noAuthOffered {
		// No acceptable methods.
		_, _ = conn.Write([]byte{socks5Version, authNoAccept})
		return nil, fmt.Errorf("no acceptable auth methods")
	}
	if _, err := conn.Write([]byte{socks5Version, authNone}); err != nil {
		return nil, fmt.Errorf("write auth response: %w", err)
	}

	// Read request
	buf = make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, fmt.Errorf("read request header: %w", err)
	}
	if buf[0] != socks5Version {
		return nil, fmt.Errorf("unsupported request version: %d", buf[0])
	}
	cmd := buf[1]
	if cmd != cmdConnect && cmd != cmdUDPAssociate {
		sendReply(conn, repCmdNotSupported, nil, 0)
		return nil, fmt.Errorf("unsupported command: %d", cmd)
	}

	// Read target address
	var targetHost string
	switch buf[3] {
	case addrTypeIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return nil, fmt.Errorf("read ipv4 addr: %w", err)
		}
		targetHost = net.IP(addr).String()
	case addrTypeIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return nil, fmt.Errorf("read ipv6 addr: %w", err)
		}
		targetHost = net.IP(addr).String()
	case addrTypeDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, fmt.Errorf("read domain len: %w", err)
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return nil, fmt.Errorf("read domain: %w", err)
		}
		targetHost = string(domain)
	default:
		sendReply(conn, repAddrNotSupport, nil, 0)
		return nil, fmt.Errorf("unsupported address type: %d", buf[3])
	}

	// Read port
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return nil, fmt.Errorf("read port: %w", err)
	}
	port := binary.BigEndian.Uint16(portBuf)
	targetAddr := net.JoinHostPort(targetHost, fmt.Sprintf("%d", port))

	return &SocksRequest{Cmd: cmd, TargetAddr: targetAddr}, nil
}

// SendSuccessReply sends a SOCKS5 success reply to the client.
func SendSuccessReply(conn net.Conn) {
	sendReply(conn, repSuccess, net.ParseIP("0.0.0.0"), 0)
}

// SendErrorReply sends a SOCKS5 error reply.
func SendErrorReply(conn net.Conn, rep byte) {
	sendReply(conn, rep, net.ParseIP("0.0.0.0"), 0)
}

func sendReply(conn net.Conn, rep byte, bindAddr net.IP, bindPort uint16) {
	reply := make([]byte, 10)
	reply[0] = socks5Version
	reply[1] = rep
	reply[2] = 0x00 // reserved
	reply[3] = addrTypeIPv4
	if bindAddr != nil {
		copy(reply[4:8], bindAddr.To4())
	}
	binary.BigEndian.PutUint16(reply[8:10], bindPort)
	conn.Write(reply)
}
