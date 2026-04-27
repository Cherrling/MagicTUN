package transport

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame format over TCP:
//
//	[1 byte: type] [4 bytes: stream ID] [4 bytes: length] [length bytes: payload]
//
// Stream ID 0 is reserved for the control channel.
// Types: MsgData (0x00), MsgClose (0x01).

const (
	FrameData  byte = 0x00
	FrameClose byte = 0x01

	ControlStreamID uint32 = 0

	frameHeaderSize = 1 + 4 + 4 // type + streamID + length
	maxFrameSize    = 1 << 20   // 1 MB max payload
)

// Frame is a single multiplexed frame.
type Frame struct {
	Type     byte
	StreamID uint32
	Payload  []byte
}

func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// WriteFrame writes a frame to a writer.
func WriteFrame(w io.Writer, f *Frame) error {
	if len(f.Payload) > maxFrameSize {
		return fmt.Errorf("frame payload too large: %d", len(f.Payload))
	}
	header := make([]byte, frameHeaderSize)
	header[0] = f.Type
	binary.BigEndian.PutUint32(header[1:], f.StreamID)
	binary.BigEndian.PutUint32(header[5:], uint32(len(f.Payload)))

	if err := writeFull(w, header); err != nil {
		return err
	}
	if err := writeFull(w, f.Payload); err != nil {
		return err
	}
	return nil
}

// ReadFrame reads a frame from a reader.
func ReadFrame(r io.Reader) (*Frame, error) {
	header := make([]byte, frameHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	f := &Frame{
		Type:     header[0],
		StreamID: binary.BigEndian.Uint32(header[1:5]),
	}
	length := binary.BigEndian.Uint32(header[5:9])
	if length > maxFrameSize {
		return nil, fmt.Errorf("frame payload too large: %d", length)
	}
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return nil, err
		}
	}
	return f, nil
}
