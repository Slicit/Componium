// Package cip implements the Componium Instrument Protocol: how the conductor
// talks to instruments that are not in this process.
//
// Everything is UDP, including cues. docs/cip.md originally specified
// WebSocket for control, which was written before anyone considered what it
// would mean on an ESP32. See ADR 0005: a websocket needs a TCP stack and
// framing on a device that has neither to spare, and TCP would let a stalled
// curve stream delay a cue behind it. UDP with an explicit acknowledgement and
// retry is about twenty lines, has no head of line blocking, and fails in ways
// that are easy to see.
//
// Three kinds of traffic, with different needs:
//
//   - Control (hello, cue, safe) is rare, must arrive, and is acknowledged.
//   - Curve frames are frequent and disposable. A dropped frame is superseded
//     20ms later, so retransmitting one is worse than useless.
//   - Heartbeats are frequent, unacknowledged, and their absence is the
//     message.
package cip

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// Port is the default UDP port a node listens on.
const Port = 5570

// Version is the protocol revision this build speaks.
const Version = "0.2"

// Type identifies a control message.
type Type string

const (
	// TypeHello is a node announcing itself and its manifest.
	TypeHello Type = "hello"
	// TypeWelcome acknowledges a hello.
	TypeWelcome Type = "welcome"
	// TypeCue is one timed event, acknowledged.
	TypeCue Type = "cue"
	// TypeAck acknowledges a cue by sequence number.
	TypeAck Type = "ack"
	// TypeSafe orders an immediate return to the safe state. It bypasses
	// everything.
	TypeSafe Type = "safe"
	// TypeHeartbeat says the conductor is alive. Unacknowledged by design.
	TypeHeartbeat Type = "heartbeat"
)

// Message is one control datagram.
type Message struct {
	Version string `json:"v"`
	Type    Type   `json:"t"`
	Seq     uint32 `json:"seq,omitempty"`

	// Hello
	Manifest *Manifest `json:"manifest,omitempty"`

	// Cue
	Instrument string             `json:"instrument,omitempty"`
	Action     string             `json:"action,omitempty"`
	Params     map[string]float64 `json:"params,omitempty"`
	// DispatchIn is how long from receipt the node should act. The conductor
	// has already subtracted the declared latency, so a node that can time
	// precisely may use this, and a simple node may ignore it and act at once.
	DispatchIn Millis `json:"in,omitempty"`

	// Error carries a refusal, most often a node enforcing its own limits.
	Error string `json:"error,omitempty"`
}

// Manifest is what a node says about itself. It mirrors instrument.Manifest
// but is a separate type on purpose: this one is a wire format and changing it
// breaks other people's firmware.
type Manifest struct {
	ID            string             `json:"id"`
	Kind          string             `json:"kind"`
	LatencyMS     Millis             `json:"latency_ms"`
	RampUpMS      Millis             `json:"ramp_up_ms,omitempty"`
	RampDownMS    Millis             `json:"ramp_down_ms,omitempty"`
	MaxContinuous Millis             `json:"max_continuous_ms,omitempty"`
	DutyCycle     float64            `json:"duty_cycle,omitempty"`
	SafeState     map[string]float64 `json:"safe_state,omitempty"`
	Channels      []Channel          `json:"channels,omitempty"`
}

// Channel documents one value a node accepts.
type Channel struct {
	Name  string     `json:"name"`
	Unit  string     `json:"unit"`
	Range [2]float64 `json:"range"`
}

// Millis is a duration carried on the wire as whole milliseconds, because
// nanoseconds are meaningless to a device with a millisecond tick and
// awkward to parse in C.
type Millis int64

func (m Millis) Duration() time.Duration { return time.Duration(m) * time.Millisecond }

// Ms converts a duration for the wire, rounding to the nearest millisecond.
func Ms(d time.Duration) Millis { return Millis((d + 500*time.Microsecond) / time.Millisecond) }

// Encode renders a control message.
func Encode(m *Message) ([]byte, error) {
	if m.Version == "" {
		m.Version = Version
	}
	return json.Marshal(m)
}

// Decode reads a control message, rejecting anything from a protocol version
// this build does not understand rather than guessing.
func Decode(b []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("cip: %w", err)
	}
	if m.Version != "" && m.Version != Version {
		return nil, fmt.Errorf("cip: protocol version %q, this build speaks %q", m.Version, Version)
	}
	if m.Type == "" {
		return nil, fmt.Errorf("cip: message has no type")
	}
	return &m, nil
}

// CurveFrame is a high rate value update. It is binary rather than JSON
// because at 50Hz per instrument the parsing cost on a microcontroller starts
// to matter, and because the shape is fixed.
//
// Layout: magic 'C','F', version, channel count, then that many float32s in
// big endian order. Channel meaning comes from the manifest, by position.
type CurveFrame struct {
	Values []float32
}

const curveMagic0, curveMagic1 = 'C', 'F'

// MarshalCurve renders a curve frame.
func MarshalCurve(values []float32) []byte {
	b := make([]byte, 4+4*len(values))
	b[0], b[1] = curveMagic0, curveMagic1
	b[2] = 0 // frame version
	b[3] = byte(len(values))
	for i, v := range values {
		binary.BigEndian.PutUint32(b[4+4*i:], mathFloat32bits(v))
	}
	return b
}

// UnmarshalCurve reads a curve frame.
func UnmarshalCurve(b []byte) ([]float32, error) {
	if len(b) < 4 || b[0] != curveMagic0 || b[1] != curveMagic1 {
		return nil, fmt.Errorf("cip: not a curve frame")
	}
	n := int(b[3])
	if len(b) != 4+4*n {
		return nil, fmt.Errorf("cip: curve frame says %d channels but is %d bytes", n, len(b))
	}
	out := make([]float32, n)
	for i := range out {
		out[i] = mathFloat32frombits(binary.BigEndian.Uint32(b[4+4*i:]))
	}
	return out, nil
}
