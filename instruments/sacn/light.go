package sacn

import (
	"context"
	"fmt"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// Mode is how a fixture's channels are laid out from its start address.
type Mode string

const (
	ModeDimmer Mode = "dimmer" // one channel: intensity
	ModeRGB    Mode = "rgb"    // three channels
	ModeRGBW   Mode = "rgbw"   // four channels
)

func (m Mode) width() int {
	switch m {
	case ModeRGB:
		return 3
	case ModeRGBW:
		return 4
	default:
		return 1
	}
}

// DefaultLatency is the dead time assumed for a DMX fixture reached over the
// network. Measured typical values for an sACN node plus an LED driver are
// tens of milliseconds; this is declared rather than measured, and a rig with
// a slower fixture should override it.
const DefaultLatency = 20 * time.Millisecond

// Config describes one lighting instrument.
type Config struct {
	ID       string
	Universe uint16
	// Addr overrides the destination. Empty means the conventional multicast
	// group for the universe.
	Addr string
	// Start is the fixture's DMX start address, 1 based as every lighting
	// desk in the world numbers it.
	Start   int
	Mode    Mode
	Latency time.Duration
	// SourceName appears in the E1.31 packet, so an operator staring at a
	// lighting console can see who is talking.
	SourceName string
}

// Light is a DMX fixture addressed over sACN.
//
// A view onto a few channels of a universe, not an owner of one. A universe is
// 512 channels and is meant to carry several fixtures; a fixture that owned its
// own buffer and socket would transmit all 512 slots with every other
// fixture's channels at zero, and two of them on one universe would erase each
// other. See universe.go, which is where that bug is written up.
//
// It does not own a ticker either: keepalive belongs to the universe and is
// started explicitly by the caller.
type Light struct {
	cfg Config
	u   *Universe
	// mine is the universe this light dialled for itself, and is what Close
	// closes. Nil when the universe was handed in, because a fixture does not
	// get to close a universe other fixtures are using.
	mine *Universe
}

// New dials the destination and prepares the fixture.
func New(cfg Config) (*Light, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("sacn: ID is required")
	}
	if cfg.Start < 1 || cfg.Start+cfg.Mode.width()-1 > Slots {
		return nil, fmt.Errorf("sacn: start address %d does not fit a %s fixture in 512 channels", cfg.Start, cfg.Mode)
	}
	u, err := Dial(cfg.Universe, cfg.Addr, cfg.SourceName)
	if err != nil {
		return nil, err
	}
	l, err := On(u, cfg)
	if err != nil {
		u.Close()
		return nil, err
	}
	l.mine = u
	return l, nil
}

// On puts a fixture into a universe somebody else is holding.
//
// This is how a rig with three lights on one universe is built: one universe,
// three views. They share the buffer, so setting one does not blank the others.
func On(u *Universe, cfg Config) (*Light, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("sacn: ID is required")
	}
	if cfg.Start < 1 || cfg.Start+cfg.Mode.width()-1 > Slots {
		return nil, fmt.Errorf("sacn: start address %d does not fit a %s fixture in 512 channels", cfg.Start, cfg.Mode)
	}
	if cfg.Latency == 0 {
		cfg.Latency = DefaultLatency
	}
	return &Light{cfg: cfg, u: u}, nil
}

// Close releases the universe, if this fixture is the one that opened it.
func (l *Light) Close() error {
	if l.mine == nil {
		return nil
	}
	return l.mine.Close()
}

func (l *Light) Manifest() instrument.Manifest {
	return instrument.Manifest{
		ID:      l.cfg.ID,
		Kind:    "light",
		Latency: l.cfg.Latency,
	}
}

// Dispatch accepts domain values, never channel numbers.
//
// Params are 0 to 1: r, g, b, w for colour, intensity for a dimmer. Mapping
// them onto DMX channels is this instrument's problem, which is exactly the
// division ADR 0001 sets out.
func (l *Light) Dispatch(d instrument.Dispatch) error {
	values := make([]byte, l.cfg.Mode.width())
	switch l.cfg.Mode {
	case ModeRGB, ModeRGBW:
		values[0] = level(d.Cue.Params["r"])
		values[1] = level(d.Cue.Params["g"])
		values[2] = level(d.Cue.Params["b"])
		if l.cfg.Mode == ModeRGBW {
			values[3] = level(d.Cue.Params["w"])
		}
	default:
		values[0] = level(d.Cue.Params["intensity"])
	}
	if d.Cue.Action == "off" {
		for i := range values {
			values[i] = 0
		}
	}
	// Only this fixture's channels. Everything else in the universe belongs to
	// somebody else and is none of this fixture's business.
	return l.u.Set(l.cfg.Start-1, values)
}

// Universe is the universe this fixture is in, for a caller that needs to keep
// it alive or close it.
func (l *Light) Sender() *Universe { return l.u }

// Keepalive keeps this fixture's universe being transmitted.
//
// Kept here because a caller holding one fixture should not have to know about
// universes to stop it going dark. Two fixtures on one universe running this
// is two tickers on one socket, which is wasteful and harmless; a rig builds
// one per universe instead.
func (l *Light) Keepalive(ctx context.Context, interval time.Duration) error {
	return l.u.Keepalive(ctx, interval)
}

// level converts a 0 to 1 domain value into a DMX level, clamping rather than
// wrapping: a colour of 1.5 is a mistake, and going dark would hide it.
func level(v float64) byte {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return byte(v*255 + 0.5)
	}
}
