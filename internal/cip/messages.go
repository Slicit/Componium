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
//
// 0.3 is a node carrying several devices rather than being one. See ADR 0007:
// hello is a list, cues name an instrument, curve frames are bundled and carry
// an index, and a node can be told what is attached to it.
const Version = "0.3"

// Version02 is what a node built before ADR 0007 speaks.
//
// Accepted on the way in, because a firmware upgrade should not be the price of
// a conductor upgrade, and because the version field exists precisely so that
// this can be a decision rather than a break.
const Version02 = "0.2"

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
	// TypeConfigure tells a node what is attached to which pin. Acknowledged,
	// and refused outright by a node without authentication: a stranger who
	// can write this can move a relay onto a pin nobody intended, or declare a
	// latency of zero and corrupt every cue after it.
	TypeConfigure Type = "configure"
)

// Message is one control datagram.
type Message struct {
	Version string `json:"v"`
	Type    Type   `json:"t"`
	Seq     uint32 `json:"seq,omitempty"`
	// N is a monotonic counter, used to reject replayed control messages. It
	// is only meaningful when a secret is configured: without one, an attacker
	// can forge messages outright and a counter buys nothing.
	N uint64 `json:"n,omitempty"`

	// Hello
	Manifest *Manifest `json:"manifest,omitempty"`

	// Cue
	Instrument string             `json:"instrument,omitempty"`
	Action     string             `json:"action,omitempty"`
	Params     map[string]float64 `json:"params,omitempty"`
	// HoldMS is how long the effect should last. A node that receives one
	// must end the effect itself when it expires, without waiting to be
	// told.
	//
	// This duplicates the stop the conductor will also send, on purpose. The
	// stop is a UDP datagram and can be lost, and the conductor is a process
	// and can crash. An instrument that only stops when told is one dropped
	// packet away from running until somebody pulls a plug.
	HoldMS Millis `json:"hold_ms,omitempty"`
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

// Instrument is one device on a node, as the node describes it.
//
// Index is what curve frames address, and is only meaningful for the session
// that announced it: configuration is editable, so index 2 can be a different
// device after a reboot. A node that restarts says hello again and every index
// is re-read. Anything holding an old one is holding a way to drive the wrong
// output with nothing in the room to show for it.
type Instrument struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Kind  string `json:"kind"`

	LatencyMS   float64 `json:"latency_ms"`
	RampUpMS    float64 `json:"ramp_up_ms,omitempty"`
	RampDownMS  float64 `json:"ramp_down_ms,omitempty"`
	MaxContinMS float64 `json:"max_continuous_ms,omitempty"`
	DutyCycle   float64 `json:"duty_cycle,omitempty"`

	SafeState map[string]float64 `json:"safe_state,omitempty"`
	Channels  []Channel          `json:"channels,omitempty"`
}

// NodeInfo describes the board itself, for logs and for a person looking at a
// list of them. Not to be confused with Node, which is a software node: this is
// what a node says about itself, not the thing saying it.
type NodeInfo struct {
	Name     string `json:"name,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	Chip     string `json:"chip,omitempty"`
}

// Device is one entry in a configuration: what is attached, and where.
//
// The type is what a firmware build contains; the device is what a
// configuration says is plugged into it. The physical facts travel with it,
// which is the point of the whole message: latency_ms stops being a #define
// and becomes something a person who has measured their fan can set.
type Device struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	GPIO int    `json:"gpio"`
	Kind string `json:"kind"`

	// pwm
	FreqHz int `json:"freq_hz,omitempty"`
	// ws28xx
	Pixels int    `json:"pixels,omitempty"`
	Order  string `json:"order,omitempty"`
	// relay
	Active string `json:"active,omitempty"`

	LatencyMS  float64 `json:"latency_ms,omitempty"`
	RampUpMS   float64 `json:"ramp_up_ms,omitempty"`
	RampDownMS float64 `json:"ramp_down_ms,omitempty"`
	Safe       float64 `json:"safe,omitempty"`
}

// Device types a build may contain. Three, which is what an ESP32 usefully
// drives; see ADR 0007 for why there is no builder to select between them yet.
const (
	DevicePWM    = "pwm"
	DeviceWS28xx = "ws28xx"
	DeviceRelay  = "relay"
)

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
