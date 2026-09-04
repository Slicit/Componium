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

	"github.com/Slicit/componium/internal/instrument"
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

	// Hello. Instruments since 0.3; Manifest is what a 0.2 node sends and is
	// still read, because a firmware upgrade should not be the price of a
	// conductor upgrade.
	Instruments []Instrument `json:"instruments,omitempty"`
	Node        NodeInfo     `json:"node,omitempty"`
	Manifest    *Manifest    `json:"manifest,omitempty"`

	// Configure
	Devices []Device `json:"devices,omitempty"`

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

	// How it is wired. Announced because the board is the only thing that
	// knows: a studio reading a manifest without these has to invent them, and
	// what it invents looks exactly like a board that forgot its configuration.
	//
	// Absent on a node that was built before ADR 0007 or configured from a
	// compiled-in manifest, which is why they are all omitempty: unknown and
	// zero are different answers, and a GPIO of 0 is a real pin.
	Type   string  `json:"type,omitempty"`
	GPIO   int     `json:"gpio,omitempty"`
	FreqHz int     `json:"freq_hz,omitempty"`
	Pixels int     `json:"pixels,omitempty"`
	Active string  `json:"active,omitempty"`
	Order  string  `json:"order,omitempty"`
	Safe   float64 `json:"safe,omitempty"`
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

// toInstrument converts a 0.3 instrument entry into the conductor's shape.
func (i Instrument) toInstrument() instrument.Manifest {
	return instrument.Manifest{
		ID:      i.ID,
		Kind:    i.Kind,
		Latency: time.Duration(i.LatencyMS) * time.Millisecond,
		Ramp: instrument.Ramp{
			Up:   time.Duration(i.RampUpMS) * time.Millisecond,
			Down: time.Duration(i.RampDownMS) * time.Millisecond,
		},
		MaxContinuous: time.Duration(i.MaxContinMS) * time.Millisecond,
		DutyCycle:     i.DutyCycle,
		SafeState:     i.SafeState,
	}
}

// toAnnouncement is how a node describes one of its devices in a hello.
//
// The mirror of toInstrument, and it lives here beside it so that the two
// cannot drift: a field added to one and forgotten in the other is a field that
// silently stops crossing the wire.
func (m Manifest) toAnnouncement(index int) Instrument {
	return Instrument{
		Index:       index,
		ID:          m.ID,
		Kind:        m.Kind,
		LatencyMS:   float64(m.LatencyMS),
		RampUpMS:    float64(m.RampUpMS),
		RampDownMS:  float64(m.RampDownMS),
		MaxContinMS: float64(m.MaxContinuous),
		DutyCycle:   m.DutyCycle,
		SafeState:   m.SafeState,
		Channels:    m.Channels,
		Type:        m.Type,
		GPIO:        m.GPIO,
		FreqHz:      m.FreqHz,
		Pixels:      m.Pixels,
		Active:      m.Active,
		Order:       m.Order,
		Safe:        m.Safe,
	}
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

	// How it is wired, from the board's own configuration. Omitted by a node
	// that has none, so that unknown stays distinguishable from zero.
	Type   string  `json:"type,omitempty"`
	GPIO   int     `json:"gpio,omitempty"`
	FreqHz int     `json:"freq_hz,omitempty"`
	Pixels int     `json:"pixels,omitempty"`
	Active string  `json:"active,omitempty"`
	Order  string  `json:"order,omitempty"`
	Safe   float64 `json:"safe,omitempty"`

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

// toManifest is what a configured device announces itself as.
//
// The physical facts travel from the configuration into the manifest unchanged,
// which is the whole point of ADR 0007: latency stops being a number compiled
// into firmware and becomes one that whoever measured their fan can set.
func (d Device) toManifest() Manifest {
	m := Manifest{
		ID:         d.ID,
		Kind:       d.Kind,
		LatencyMS:  Millis(d.LatencyMS),
		RampUpMS:   Millis(d.RampUpMS),
		RampDownMS: Millis(d.RampDownMS),
		Type:       d.Type,
		GPIO:       d.GPIO,
		FreqHz:     d.FreqHz,
		Pixels:     d.Pixels,
		Active:     d.Active,
		Order:      d.Order,
		Safe:       d.Safe,
	}
	// Channels follow from the type, because they are what the hardware can be
	// told rather than something a person should have to write out.
	switch d.Type {
	case DeviceWS28xx:
		m.Channels = []Channel{
			{Name: "r", Unit: "normalised", Range: [2]float64{0, 1}},
			{Name: "g", Unit: "normalised", Range: [2]float64{0, 1}},
			{Name: "b", Unit: "normalised", Range: [2]float64{0, 1}},
		}
		m.SafeState = map[string]float64{"r": 0, "g": 0, "b": 0}
	default:
		// pwm and relay are both one number, and a relay is a pwm that has
		// made up its mind.
		m.Channels = []Channel{
			{Name: "intensity", Unit: "normalised", Range: [2]float64{0, 1}},
		}
		m.SafeState = map[string]float64{"intensity": d.Safe}
	}
	return m
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
