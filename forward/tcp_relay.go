package forward

import (
	"fmt"
	"io"
	"log"
	"net"

	"github.com/example/magictun/transport"
	"github.com/example/magictun/wire"
)

// TCPRelay handles TCP connection forwarding over the overlay network.
type TCPRelay struct {
	// Function to open a new stream to a peer
	dialStream func(peerID [16]byte) (*transport.Stream, error)
}

// NewTCPRelay creates a new TCP relay.
func NewTCPRelay(dialStream func(peerID [16]byte) (*transport.Stream, error)) *TCPRelay {
	return &TCPRelay{dialStream: dialStream}
}

// RelayToPeer forwards a local TCP connection to a remote peer for final delivery.
func (r *TCPRelay) RelayToPeer(peerID [16]byte, targetAddr string, localConn readWriteCloser) error {
	stream, err := r.dialStream(peerID)
	if err != nil {
		return fmt.Errorf("open stream to peer: %w", err)
	}
	defer stream.Close()

	// Parse target
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("invalid target IP: %s", host)
	}
	portNum := uint16(0)
	if p, err := fmt.Sscanf(port, "%d", &portNum); err != nil || p != 1 {
		return fmt.Errorf("invalid port: %s", port)
	}

	// Send TCP forward header
	hdr := wire.EncodeTCPForwardHeader(&wire.TCPForwardHeader{
		Flags:      wire.TCPFlagNewStream,
		StreamID:   uint32(stream.StreamID()),
		TargetAddr: ip,
		TargetPort: portNum,
	})
	if _, err := stream.Write(hdr); err != nil {
		return fmt.Errorf("write forward header: %w", err)
	}

	// Bidirectional splice
	return splice(stream, localConn)
}

// HandleIncomingRelay handles an incoming TCP relay request from a peer.
// This is called when a remote peer opens a stream to us for TCP forwarding.
func (r *TCPRelay) HandleIncomingRelay(stream *transport.Stream, dialDirect func(addr string) (net.Conn, error)) error {
	// Read the TCP forward header
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil {
		return fmt.Errorf("read forward header: %w", err)
	}

	hdr, err := wire.DecodeTCPForwardHeader(buf[:n])
	if err != nil {
		return fmt.Errorf("decode forward header: %w", err)
	}

	targetAddr := net.JoinHostPort(hdr.TargetAddr.String(), fmt.Sprintf("%d", hdr.TargetPort))

	// Dial the target
	targetConn, err := dialDirect(targetAddr)
	if err != nil {
		return fmt.Errorf("dial target %s: %w", targetAddr, err)
	}
	defer targetConn.Close()

	// Splice
	return splice(stream, targetConn)
}

type readWriteCloser interface {
	io.Reader
	io.Writer
	io.Closer
}

func splice(a, b readWriteCloser) error {
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(a, b)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(b, a)
		errCh <- err
	}()
	err := <-errCh
	if err != nil && err != io.EOF {
		log.Printf("tcp relay: splice error: %v", err)
	}
	return nil
}
