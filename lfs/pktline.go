package lfs

import (
	"encoding/hex"
	"fmt"
	"io"
)

// PktType classifies a decoded pkt-line packet.
type PktType int

const (
	PktData    PktType = iota // regular data packet
	PktFlush                  // "0000" — flush packet
	PktDelim                  // "0001" — delimiter packet (capability/section boundary)
	PktRespEnd                // "0002" — response end packet
)

// PktPacket is a single decoded pkt-line frame.
type PktPacket struct {
	Type PktType
	Data []byte // non-nil only for PktData
}

// PktEncoder writes pkt-line frames to an underlying writer.
// It is NOT safe for concurrent use.
type PktEncoder struct {
	w io.Writer
}

// NewPktEncoder returns a PktEncoder that writes to w.
func NewPktEncoder(w io.Writer) *PktEncoder { return &PktEncoder{w: w} }

// WritePacket writes a data packet. len(data) must be in [1, 65516].
func (e *PktEncoder) WritePacket(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("pktline: WritePacket called with empty data; use WriteFlush for flush")
	}
	if len(data) > 65516 {
		return fmt.Errorf("pktline: packet too large (%d bytes; max 65516)", len(data))
	}
	// total length = 4-byte prefix + data
	length := len(data) + 4
	header := fmt.Sprintf("%04x", length)
	if _, err := io.WriteString(e.w, header); err != nil {
		return err
	}
	_, err := e.w.Write(data)
	return err
}

// WriteString is a convenience wrapper around WritePacket for string payloads.
func (e *PktEncoder) WriteString(s string) error {
	return e.WritePacket([]byte(s))
}

// WriteFlush writes a flush packet ("0000").
func (e *PktEncoder) WriteFlush() error {
	_, err := io.WriteString(e.w, "0000")
	return err
}

// WriteDelim writes a delimiter packet ("0001").
func (e *PktEncoder) WriteDelim() error {
	_, err := io.WriteString(e.w, "0001")
	return err
}

// WriteRespEnd writes a response-end packet ("0002").
func (e *PktEncoder) WriteRespEnd() error {
	_, err := io.WriteString(e.w, "0002")
	return err
}

// PktScanner reads pkt-line frames from an underlying reader.
// It is NOT safe for concurrent use.
type PktScanner struct {
	r io.Reader
}

// NewPktScanner returns a PktScanner that reads from r.
func NewPktScanner(r io.Reader) *PktScanner { return &PktScanner{r: r} }

// ReadPacket reads one pkt-line frame and returns it.
// Returns (PktPacket{}, io.EOF) when the underlying reader is exhausted before the
// 4-byte length prefix could be read.
func (s *PktScanner) ReadPacket() (PktPacket, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(s.r, header); err != nil {
		return PktPacket{}, err
	}

	// Decode hex length.
	raw := make([]byte, 2)
	if _, err := hex.Decode(raw, header); err != nil {
		return PktPacket{}, fmt.Errorf("pktline: invalid length prefix %q: %w", string(header), err)
	}
	length := int(raw[0])<<8 | int(raw[1])

	switch length {
	case 0:
		return PktPacket{Type: PktFlush}, nil
	case 1:
		return PktPacket{Type: PktDelim}, nil
	case 2:
		return PktPacket{Type: PktRespEnd}, nil
	}

	if length < 4 {
		return PktPacket{}, fmt.Errorf("pktline: invalid length %d (must be 0, 1, 2, or ≥4)", length)
	}

	dataLen := length - 4
	if dataLen == 0 {
		return PktPacket{Type: PktData, Data: []byte{}}, nil
	}

	data := make([]byte, dataLen)
	if _, err := io.ReadFull(s.r, data); err != nil {
		return PktPacket{}, fmt.Errorf("pktline: reading %d data bytes: %w", dataLen, err)
	}
	return PktPacket{Type: PktData, Data: data}, nil
}
