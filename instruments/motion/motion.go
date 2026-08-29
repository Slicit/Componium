// Package motion drives a motion platform by emitting 6DOF pose.
//
// Per ADR 0003 Componium does not write actuator control loops. The DIY sim
// racing community has spent years on actuators, controller boards and rig
// software, and reimplementing that would be both arrogant and worse. This
// package emits where the platform should be, in metres and degrees, and an
// adapter or an existing rig tool turns that into actuator lengths.
//
// Note what is deliberately absent: there is no washout filter. In sim racing
// motion is derived from physics telemetry and must be washed out to fit
// limited travel. In cinema the motion is authored, by a person working within
// the rig's declared limits, so there is nothing to wash out. Generated motion
// is different, and that filter belongs in the composer, offline.
package motion

import (
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// Pose is where the platform should be.
//
// Surge, sway and heave are metres: forward, right, up. Roll, pitch and yaw
// are degrees. These are the conventional axes and the conventional signs, and
// departing from them would guarantee somebody's rig moves backwards.
type Pose struct {
	Surge, Sway, Heave float64
	Roll, Pitch, Yaw   float64
}

// Limits is the platform's declared travel. Nothing is ever sent outside it.
//
// This is the one place in Componium where clamping is a safety measure rather
// than a tidiness measure: a platform commanded beyond its travel does not
// politely refuse, it drives into its end stops.
type Limits struct {
	Surge, Sway, Heave float64 // metres, symmetric about zero
	Roll, Pitch, Yaw   float64 // degrees, symmetric about zero
}

// DefaultLimits are deliberately small. A rig that has not declared its travel
// should move timidly rather than confidently.
var DefaultLimits = Limits{
	Surge: 0.05, Sway: 0.05, Heave: 0.05,
	Roll: 5, Pitch: 5, Yaw: 5,
}

// Format is how the pose is written on the wire.
type Format string

const (
	// FormatCSV is one ASCII line per pose: "surge,sway,heave,roll,pitch,yaw".
	// Unglamorous, and readable by anything, including a script somebody wrote
	// in an afternoon to drive their own rig.
	FormatCSV Format = "csv"
	// FormatLabelled is the same values with axis names, for adapters that
	// want to be sure which is which.
	FormatLabelled Format = "labelled"
)

// Config describes a motion platform.
type Config struct {
	ID      string
	Addr    string
	Format  Format
	Limits  Limits
	Latency time.Duration
	// Rate caps how often pose is sent. Zero means no cap; the curve driver
	// already limits it.
	Rate time.Duration
}

// Platform is a motion rig addressed over UDP.
type Platform struct {
	cfg  Config
	conn net.Conn

	mu       sync.Mutex
	last     time.Time
	lastPose Pose
	sent     int
	clamped  int
}

// New dials the rig.
func New(cfg Config) (*Platform, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("motion: ID is required")
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("motion: %s has no address", cfg.ID)
	}
	if cfg.Format == "" {
		cfg.Format = FormatCSV
	}
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits
	}
	conn, err := net.Dial("udp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("motion: dial %s: %w", cfg.Addr, err)
	}
	return &Platform{cfg: cfg, conn: conn}, nil
}

func (p *Platform) Close() error { return p.conn.Close() }

func (p *Platform) Manifest() instrument.Manifest {
	return instrument.Manifest{
		ID:      p.cfg.ID,
		Kind:    "motion",
		Latency: p.cfg.Latency,
		SafeState: map[string]float64{
			"surge": 0, "sway": 0, "heave": 0,
			"roll": 0, "pitch": 0, "yaw": 0,
		},
	}
}

// Dispatch accepts a pose as domain values and sends it, clamped.
func (p *Platform) Dispatch(d instrument.Dispatch) error {
	pose := Pose{
		Surge: d.Cue.Params["surge"],
		Sway:  d.Cue.Params["sway"],
		Heave: d.Cue.Params["heave"],
		Roll:  d.Cue.Params["roll"],
		Pitch: d.Cue.Params["pitch"],
		Yaw:   d.Cue.Params["yaw"],
	}
	switch d.Cue.Action {
	case "safe", "off", "neutral", "stop":
		pose = Pose{}
	}

	p.mu.Lock()
	if p.cfg.Rate > 0 && !p.last.IsZero() && !d.Wall.IsZero() && d.Wall.Sub(p.last) < p.cfg.Rate {
		p.mu.Unlock()
		return nil
	}
	if !d.Wall.IsZero() {
		p.last = d.Wall
	}
	clamped, hit := clamp(pose, p.cfg.Limits)
	if hit {
		p.clamped++
	}
	p.lastPose = clamped
	p.sent++
	p.mu.Unlock()

	_, err := p.conn.Write([]byte(encode(clamped, p.cfg.Format)))
	return err
}

// Clamped reports how many poses were outside the declared travel. A score
// that clamps constantly was written for a different rig, and the operator
// should be told rather than left wondering why nothing reaches the extremes.
func (p *Platform) Clamped() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clamped
}

// Sent reports how many poses have been transmitted.
func (p *Platform) Sent() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent
}

func clamp(p Pose, l Limits) (Pose, bool) {
	hit := false
	limit := func(v, max float64) float64 {
		if max <= 0 {
			return 0
		}
		if v > max {
			hit = true
			return max
		}
		if v < -max {
			hit = true
			return -max
		}
		if math.IsNaN(v) {
			hit = true
			return 0
		}
		return v
	}
	return Pose{
		Surge: limit(p.Surge, l.Surge),
		Sway:  limit(p.Sway, l.Sway),
		Heave: limit(p.Heave, l.Heave),
		Roll:  limit(p.Roll, l.Roll),
		Pitch: limit(p.Pitch, l.Pitch),
		Yaw:   limit(p.Yaw, l.Yaw),
	}, hit
}

func encode(p Pose, f Format) string {
	switch f {
	case FormatLabelled:
		var b strings.Builder
		fmt.Fprintf(&b, "surge=%.5f sway=%.5f heave=%.5f roll=%.3f pitch=%.3f yaw=%.3f\n",
			p.Surge, p.Sway, p.Heave, p.Roll, p.Pitch, p.Yaw)
		return b.String()
	default:
		return fmt.Sprintf("%.5f,%.5f,%.5f,%.3f,%.3f,%.3f\n",
			p.Surge, p.Sway, p.Heave, p.Roll, p.Pitch, p.Yaw)
	}
}
