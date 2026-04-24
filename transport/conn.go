package transport

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/example/magictun/identity"
)

// Conn wraps a TCP+TLS connection with frame-based multiplexing.
// A single Conn carries multiple logical streams over one TCP connection.
type Conn struct {
	raw     net.Conn
	frameMu sync.Mutex // protects writes to raw

	streams   map[uint32]*Stream
	streamsMu sync.Mutex
	nextID    uint32

	acceptCh chan *Stream
	closed   bool
	closeMu  sync.Mutex

	done chan struct{}
}

// Stream is a single logical stream within a multiplexed connection.
type Stream struct {
	id     uint32
	conn   *Conn
	readCh chan []byte
	buf    []byte

	closed bool
	mu     sync.Mutex
}

// Dial connects to a remote peer via TCP+TLS.
// If pinStore is non-nil, the server's certificate is verified via TOFU pinning.
func Dial(addr string, tlsCfg *tls.Config, pinStore *identity.PinStore) (*Conn, error) {
	raw, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("tls dial: %w", err)
	}

	// Set TCP keepalive
	if tcpConn, ok := raw.NetConn().(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	if pinStore != nil {
		cs := raw.ConnectionState()
		if len(cs.PeerCertificates) == 0 {
			raw.Close()
			return nil, fmt.Errorf("tls: no peer certificate")
		}
		if ok, err := pinStore.Check(addr, cs.PeerCertificates[0]); err != nil {
			raw.Close()
			return nil, fmt.Errorf("pin check: %w", err)
		} else if !ok {
			raw.Close()
			return nil, fmt.Errorf("pin check failed for %s", addr)
		}
	}

	return newConn(raw), nil
}

// newConn creates a Conn from an established net.Conn.
func newConn(raw net.Conn) *Conn {
	c := &Conn{
		raw:      raw,
		streams:  make(map[uint32]*Stream),
		nextID:   1, // 0 reserved for control
		acceptCh: make(chan *Stream, 64),
		done:     make(chan struct{}),
	}
	// Pre-create the control stream so it's ready before readLoop starts
	cs := &Stream{id: ControlStreamID, conn: c, readCh: make(chan []byte, 64)}
	c.streams[ControlStreamID] = cs
	go c.readLoop()
	return c
}

// OpenStream creates a new outgoing stream.
func (c *Conn) OpenStream() (*Stream, error) {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	if c.closed {
		return nil, net.ErrClosed
	}
	// Find an available outgoing stream ID (odd numbers)
	// Wrap around if we exhaust the ID space
	for attempts := 0; attempts < 1000; attempts++ {
		id := c.nextID
		c.nextID += 2
		if c.nextID < 2 { // overflow check
			c.nextID = 1
		}
		if _, exists := c.streams[id]; !exists {
			s := &Stream{id: id, conn: c, readCh: make(chan []byte, 16)}
			c.streams[id] = s
			return s, nil
		}
	}
	return nil, fmt.Errorf("transport: no available stream IDs")
}

// AcceptStream accepts an incoming stream (from AcceptCh).
// Returns the control stream (ID 0) on first call.
func (c *Conn) AcceptStream() (*Stream, error) {
	select {
	case s, ok := <-c.acceptCh:
		if !ok {
			return nil, net.ErrClosed
		}
		return s, nil
	case <-c.done:
		return nil, net.ErrClosed
	}
}

// Close closes the underlying connection and all streams.
func (c *Conn) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.done)
	return c.raw.Close()
}

// IsClosed returns whether the connection is closed.
func (c *Conn) IsClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closed
}

// RemoteAddr returns the remote address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.raw.RemoteAddr()
}

// SetDeadline sets the read/write deadline on the underlying connection.
func (c *Conn) SetDeadline(t time.Time) error {
	return c.raw.SetDeadline(t)
}

func (c *Conn) readLoop() {
	for {
		f, err := ReadFrame(c.raw)
		if err != nil {
			c.closeAll()
			return
		}

		c.streamsMu.Lock()
		s, ok := c.streams[f.StreamID]
		if !ok {
			// New incoming stream
			s = &Stream{id: f.StreamID, conn: c, readCh: make(chan []byte, 16)}
			c.streams[f.StreamID] = s
			c.streamsMu.Unlock()

			select {
			case c.acceptCh <- s:
			case <-c.done:
				return
			}
		} else {
			c.streamsMu.Unlock()
		}

		if f.Type == FrameClose {
			s.mu.Lock()
			s.closed = true
			s.mu.Unlock()
			select {
			case s.readCh <- nil:
			default:
			}
			c.streamsMu.Lock()
			delete(c.streams, f.StreamID)
			c.streamsMu.Unlock()
			continue
		}

		select {
		case s.readCh <- f.Payload:
		case <-c.done:
			return
		}
	}
}

func (c *Conn) closeAll() {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return
	}
	c.closed = true
	c.closeMu.Unlock()
	close(c.done)

	c.streamsMu.Lock()
	for _, s := range c.streams {
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			select {
			case s.readCh <- nil:
			default:
			}
		}
		s.mu.Unlock()
	}
	c.streamsMu.Unlock()
}

// Read reads data from the stream.
func (s *Stream) Read(b []byte) (int, error) {
	if len(s.buf) > 0 {
		n := copy(b, s.buf)
		s.buf = s.buf[n:]
		return n, nil
	}

	data, ok := <-s.readCh
	if !ok || data == nil {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return 0, fmt.Errorf("stream closed")
		}
		return 0, net.ErrClosed
	}

	n := copy(b, data)
	if n < len(data) {
		s.buf = data[n:]
	}
	return n, nil
}

// Write sends data on the stream.
func (s *Stream) Write(b []byte) (int, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}

	err := s.conn.writeFrame(&Frame{
		Type:     FrameData,
		StreamID: s.id,
		Payload:  b,
	})
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close closes the stream.
func (s *Stream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	return s.conn.writeFrame(&Frame{
		Type:     FrameClose,
		StreamID: s.id,
	})
}

// StreamID returns the stream's identifier.
func (s *Stream) StreamID() uint32 {
	return s.id
}

func (c *Conn) writeFrame(f *Frame) error {
	c.frameMu.Lock()
	defer c.frameMu.Unlock()
	return WriteFrame(c.raw, f)
}

// ControlStream returns the control stream (stream ID 0).
// This stream is pre-created when the connection is established.
func (c *Conn) ControlStream() *Stream {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()
	return c.streams[ControlStreamID]
}
