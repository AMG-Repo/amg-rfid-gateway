// Package chainway implements the Chainway UR4 fixed reader TCP protocol
// (command work mode) used by the vendor ReaderAPI.
//
// Frame layout:
//
//	A5 5A | LEN(2, big-endian) | CMD(1) | DATA(n) | CHK(1) | 0D 0A
//
// LEN is the whole frame length including header and trailer. CHK is the XOR
// of every byte from LEN through the end of DATA.
package chainway

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Header1  byte = 0xA5
	Header2  byte = 0x5A
	Trailer1 byte = 0x0D
	Trailer2 byte = 0x0A

	// FrameOverhead is header(2) + LEN(2) + CMD(1) + CHK(1) + trailer(2).
	FrameOverhead = 8
	MinFrameLen   = FrameOverhead
	MaxFrameLen   = 2048

	// DefaultMaxBuffer bounds retained stream bytes during TCP reassembly.
	DefaultMaxBuffer = 64 * 1024
)

var (
	ErrHeader      = errors.New("chainway: invalid frame header")
	ErrTrailer     = errors.New("chainway: invalid frame trailer")
	ErrChecksum    = errors.New("chainway: checksum mismatch")
	ErrFrameLength = errors.New("chainway: invalid frame length")
)

// Frame is a decoded Chainway frame.
type Frame struct {
	Cmd  byte
	Data []byte
}

// Checksum returns the XOR of b, which must span LEN through the end of DATA.
func Checksum(b []byte) byte {
	var chk byte
	for _, v := range b {
		chk ^= v
	}
	return chk
}

// Encode builds a complete frame for cmd with the given data.
func Encode(cmd byte, data []byte) ([]byte, error) {
	total := FrameOverhead + len(data)
	if total > MaxFrameLen {
		return nil, fmt.Errorf("%w: %d bytes", ErrFrameLength, total)
	}
	out := make([]byte, total)
	out[0], out[1] = Header1, Header2
	binary.BigEndian.PutUint16(out[2:4], uint16(total))
	out[4] = cmd
	copy(out[5:], data)
	out[total-3] = Checksum(out[2 : total-3])
	out[total-2], out[total-1] = Trailer1, Trailer2
	return out, nil
}

// Decode validates and parses one complete frame.
func Decode(raw []byte) (Frame, error) {
	if len(raw) < MinFrameLen {
		return Frame{}, fmt.Errorf("%w: %d bytes", ErrFrameLength, len(raw))
	}
	if raw[0] != Header1 || raw[1] != Header2 {
		return Frame{}, ErrHeader
	}
	declared := int(binary.BigEndian.Uint16(raw[2:4]))
	if declared != len(raw) || declared > MaxFrameLen {
		return Frame{}, fmt.Errorf("%w: declared %d, have %d", ErrFrameLength, declared, len(raw))
	}
	n := len(raw)
	if raw[n-2] != Trailer1 || raw[n-1] != Trailer2 {
		return Frame{}, ErrTrailer
	}
	if Checksum(raw[2:n-3]) != raw[n-3] {
		return Frame{}, ErrChecksum
	}
	data := make([]byte, n-FrameOverhead)
	copy(data, raw[5:n-3])
	return Frame{Cmd: raw[4], Data: data}, nil
}

// StreamExtractor reassembles complete, valid frames from arbitrary TCP
// chunks. Invalid lengths, trailers, and checksums resync on the next header.
type StreamExtractor struct {
	buffer    []byte
	maxBuffer int
}

// NewStreamExtractor creates an extractor with DefaultMaxBuffer.
func NewStreamExtractor() *StreamExtractor {
	return NewStreamExtractorWithMaxBuffer(DefaultMaxBuffer)
}

// NewStreamExtractorWithMaxBuffer creates an extractor with a custom cap.
func NewStreamExtractorWithMaxBuffer(max int) *StreamExtractor {
	if max <= 0 {
		max = DefaultMaxBuffer
	}
	return &StreamExtractor{maxBuffer: max}
}

// Append adds a chunk and returns every complete valid frame in order.
func (e *StreamExtractor) Append(chunk []byte) [][]byte {
	if len(chunk) > 0 {
		e.buffer = append(e.buffer, chunk...)
	}

	var frames [][]byte
	for {
		start := headerIndex(e.buffer, 0)
		if start < 0 {
			e.keepPossibleHeaderStart()
			break
		}
		e.buffer = e.buffer[start:]

		if len(e.buffer) < 4 {
			break
		}
		size := int(binary.BigEndian.Uint16(e.buffer[2:4]))
		if size < MinFrameLen || size > MaxFrameLen {
			e.buffer = e.buffer[1:]
			continue
		}
		if len(e.buffer) < size {
			break
		}
		if _, err := Decode(e.buffer[:size]); err != nil {
			e.buffer = e.buffer[1:]
			continue
		}
		frame := make([]byte, size)
		copy(frame, e.buffer[:size])
		frames = append(frames, frame)
		e.buffer = e.buffer[size:]
	}
	e.enforceMaxBuffer()
	if len(e.buffer) == 0 {
		e.buffer = nil
	}
	return frames
}

// BufferedLen returns the number of bytes retained for future extraction.
func (e *StreamExtractor) BufferedLen() int {
	return len(e.buffer)
}

// Reset clears any retained partial stream bytes.
func (e *StreamExtractor) Reset() {
	e.buffer = nil
}

// keepPossibleHeaderStart drops garbage but keeps a trailing 0xA5 that may be
// the first half of a header split across chunks.
func (e *StreamExtractor) keepPossibleHeaderStart() {
	if n := len(e.buffer); n > 0 && e.buffer[n-1] == Header1 {
		e.buffer = e.buffer[n-1:]
		return
	}
	e.buffer = nil
}

func (e *StreamExtractor) enforceMaxBuffer() {
	if len(e.buffer) <= e.maxBuffer {
		return
	}
	// Keep the first header within the newest maxBuffer bytes, else drop.
	if j := headerIndex(e.buffer, len(e.buffer)-e.maxBuffer); j >= 0 {
		e.buffer = e.buffer[j:]
		return
	}
	e.keepPossibleHeaderStart()
}

func headerIndex(b []byte, from int) int {
	for i := from; i+1 < len(b); i++ {
		if b[i] == Header1 && b[i+1] == Header2 {
			return i
		}
	}
	return -1
}
