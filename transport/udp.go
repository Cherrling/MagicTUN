package transport

import (
	"net"
)

// UDPConn wraps a UDP connection used for relaying UDP datagrams between nodes.
// Separate from the TCP+TLS control channel.
type UDPConn struct {
	conn *net.UDPConn
}

// ListenUDP creates a UDP listener for relay traffic.
func ListenUDP(addr string) (*UDPConn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return &UDPConn{conn: conn}, nil
}

// DialUDP creates a UDP connection to a remote peer for relay.
func DialUDP(addr string) (*UDPConn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}
	return &UDPConn{conn: conn}, nil
}

// ReadFrom reads a datagram from the UDP socket.
func (u *UDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	return u.conn.ReadFrom(b)
}

// WriteTo sends a datagram to the specified address.
func (u *UDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return u.conn.WriteTo(b, addr)
}

// Write sends a datagram on a connected UDP socket.
func (u *UDPConn) Write(b []byte) (int, error) {
	return u.conn.Write(b)
}

// Read reads a datagram from a connected UDP socket.
func (u *UDPConn) Read(b []byte) (int, error) {
	return u.conn.Read(b)
}

// Close closes the UDP socket.
func (u *UDPConn) Close() error {
	return u.conn.Close()
}

// LocalAddr returns the local address.
func (u *UDPConn) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

// RemoteAddr returns the remote address (for connected sockets).
func (u *UDPConn) RemoteAddr() net.Addr {
	return u.conn.RemoteAddr()
}
