package transport

import (
	"crypto/tls"
	"fmt"
	"net"
)

// Listener accepts incoming TCP+TLS connections and wraps them as Conn.
type Listener struct {
	ln      net.Listener
	tlsCfg  *tls.Config
	connCh  chan *Conn
	errCh   chan error
}

// Listen starts listening for incoming connections.
func Listen(addr string, tlsCfg *tls.Config) (*Listener, error) {
	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("tls listen: %w", err)
	}
	l := &Listener{
		ln:     ln,
		tlsCfg: tlsCfg,
		connCh: make(chan *Conn, 8),
		errCh:  make(chan error, 1),
	}
	go l.acceptLoop()
	return l, nil
}

// Accept waits for the next incoming connection.
func (l *Listener) Accept() (*Conn, error) {
	select {
	case conn := <-l.connCh:
		return conn, nil
	case err := <-l.errCh:
		return nil, err
	}
}

// Close shuts down the listener.
func (l *Listener) Close() error {
	return l.ln.Close()
}

// Addr returns the listener's address.
func (l *Listener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *Listener) acceptLoop() {
	for {
		raw, err := l.ln.Accept()
		if err != nil {
			l.errCh <- err
			return
		}
		conn := newConn(raw)
		l.connCh <- conn
	}
}
