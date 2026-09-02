package cip

import (
	"encoding/binary"
	"fmt"
)

// A curve frame carrying every output on a node that is due this tick.
//
// Bundling is not an optimisation, and it is worth being clear about that
// because it looks like one. Four datagrams for four outputs that have to move
// together arrive at four different times, and on wifi occasionally at very
// different ones. One datagram arrives once, and the node applies every output
// in it before returning, so outputs on one board move within microseconds of
// each other rather than milliseconds. It is the only way to make simultaneous
// actually simultaneous over a transport that drops and reorders.
//
// The layout, from docs/cip.md:
//
//	byte 0   'C'
//	byte 1   'F'
//	byte 2   frame version, currently 1
//	byte 3   output count
//	then, that many times:
//	  byte 0   instrument index, as announced in hello
//	  byte 1   channel count
//	  byte 2+  that many big endian float32 values
//
// Version 1 rather than 0, so a node built before ADR 0007 refuses the frame
// rather than reading the output count as a channel count and driving an
// output with somebody else's numbers.

// CurveVersion is the frame version this build writes.
const CurveVersion = 1

// CurveVersion0 is the single output frame a 0.2 node speaks.
const CurveVersion0 = 0

// Outputs is one instrument's values within a frame.
type Outputs struct {
	Index  int
	Values []float32
}

// MarshalBundle renders a frame carrying several outputs.
func MarshalBundle(outs []Outputs) ([]byte, error) {
	if len(outs) > 255 {
		return nil, fmt.Errorf("cip: %d outputs in one frame, limit is 255", len(outs))
	}
	size := 4
	for _, o := range outs {
		if o.Index < 0 || o.Index > 255 {
			return nil, fmt.Errorf("cip: instrument index %d out of range", o.Index)
		}
		if len(o.Values) > 255 {
			return nil, fmt.Errorf("cip: %d channels on one output, limit is 255", len(o.Values))
		}
		size += 2 + 4*len(o.Values)
	}

	b := make([]byte, size)
	b[0], b[1] = curveMagic0, curveMagic1
	b[2] = CurveVersion
	b[3] = byte(len(outs))
	at := 4
	for _, o := range outs {
		b[at] = byte(o.Index)
		b[at+1] = byte(len(o.Values))
		at += 2
		for _, v := range o.Values {
			binary.BigEndian.PutUint32(b[at:], mathFloat32bits(v))
			at += 4
		}
	}
	return b, nil
}

// UnmarshalBundle reads a frame, of either version.
//
// A version 0 frame is one unnamed output, which is what a 0.2 conductor sends
// and what a node with a single device is entitled to keep receiving. It comes
// back as index 0, because that is what it addressed.
func UnmarshalBundle(b []byte) ([]Outputs, error) {
	if len(b) < 4 || b[0] != curveMagic0 || b[1] != curveMagic1 {
		return nil, fmt.Errorf("cip: not a curve frame")
	}
	switch b[2] {
	case CurveVersion0:
		values, err := UnmarshalCurve(b)
		if err != nil {
			return nil, err
		}
		return []Outputs{{Index: 0, Values: values}}, nil
	case CurveVersion:
	default:
		return nil, fmt.Errorf("cip: curve frame version %d", b[2])
	}

	count := int(b[3])
	out := make([]Outputs, 0, count)
	at := 4
	for i := 0; i < count; i++ {
		// Checked before every read. This arrives over UDP from whoever can
		// reach the port, and a length that walks off the end of the datagram
		// is the cheapest possible attack on a device with no memory
		// protection worth the name.
		if at+2 > len(b) {
			return nil, fmt.Errorf("cip: curve frame ends inside output %d", i)
		}
		index := int(b[at])
		channels := int(b[at+1])
		at += 2
		if at+4*channels > len(b) {
			return nil, fmt.Errorf("cip: curve frame ends inside output %d's values", i)
		}
		values := make([]float32, channels)
		for c := 0; c < channels; c++ {
			values[c] = mathFloat32frombits(binary.BigEndian.Uint32(b[at:]))
			at += 4
		}
		out = append(out, Outputs{Index: index, Values: values})
	}
	return out, nil
}
