// Package rig reads the description of one physical installation.
//
// A score says what should happen. A rig says what is in the room and how to
// reach it. Keeping them apart is what lets the same score play on somebody
// else's hardware: the score names "light.ambient", the rig knows that on this
// installation it is an RGB fixture at DMX address 10 on universe 1.
package rig

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Slicit/componium/instruments/motion"
	"github.com/Slicit/componium/instruments/sacn"
	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/cip"
	"github.com/Slicit/componium/internal/instrument"
)

// Config is a rig file.
type Config struct {
	Rig         Meta         `toml:"rig"`
	Instruments []InstConfig `toml:"instrument"`
}

// Meta names the installation.
type Meta struct {
	Name string `toml:"name"`
}

// InstConfig describes one instrument and how to drive it.
type InstConfig struct {
	ID      string   `toml:"id"`
	Kind    string   `toml:"kind"`
	Driver  string   `toml:"driver,omitempty"`
	Latency Duration `toml:"latency,omitempty"`

	// Addr is where to reach the device, used by the sacn and cip drivers.
	RemoteTimeout Duration `toml:"remote_timeout,omitempty"`
	// Secret authenticates CIP traffic. Both ends must agree.
	Secret string `toml:"secret,omitempty"`

	// Position places the instrument in the room, for the studio's preview.
	// Metres, origin at the centre of the screen wall, x right, y up,
	// z toward the audience. Optional: the studio falls back to a sensible
	// spot for the kind.
	Position *Position `toml:"position,omitempty"`

	// Motion fields, used when Driver is "motion".
	Format string        `toml:"format,omitempty"`
	Travel *MotionTravel `toml:"travel,omitempty"`
	// Scents is what each reservoir of a scent instrument holds, by number.
	//
	//     [instrument.scents]
	//     1 = "smoke"
	//     2 = "petrichor"
	//
	// Keyed by string because TOML tables are, and read back through Scent.
	// Five bottles or fifteen is a longer table and no code: a score names a
	// smell and this says which one that is here.
	Scents map[string]string `toml:"scents,omitempty"`
	// sACN fields, used when Driver is "sacn".
	Universe uint16 `toml:"universe,omitempty"`
	Start    int    `toml:"start,omitempty"`
	Mode     string `toml:"mode,omitempty"`
	Addr     string `toml:"addr,omitempty"`
}

// Duration is a Go duration string in TOML.
type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("duration %q: %w", b, err)
	}
	*d = Duration(v)
	return nil
}

// MarshalText writes a duration the way the file has always spelled one.
//
// Without it the encoder writes an integer of nanoseconds, which is not what
// UnmarshalText reads, so a rig saved by the studio would not load again. The
// asymmetry is only visible once something writes these files, which nothing
// did until the studio could.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Position is where an instrument physically sits, in metres.
type Position struct {
	X float64 `toml:"x"`
	Y float64 `toml:"y"`
	Z float64 `toml:"z"`
}

// MotionTravel is a platform's declared range of movement, in metres and
// degrees. Nothing is ever commanded outside it, because a platform driven
// past its travel does not politely refuse: it drives into its end stops.
type MotionTravel struct {
	Surge float64 `toml:"surge"`
	Sway  float64 `toml:"sway"`
	Heave float64 `toml:"heave"`
	Roll  float64 `toml:"roll"`
	Pitch float64 `toml:"pitch"`
	Yaw   float64 `toml:"yaw"`
}

// Load reads a rig file, or the chosen rig from a directory of them.
//
// Taking a directory here rather than only in the studio is what makes the
// choice shared: `componium play -rig /rigs` reads whatever was picked in the
// browser, and nothing had to tell it.
func Load(path string) (*Config, error) {
	path, err := Resolve(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if _, err := toml.Decode(string(b), &c); err != nil {
		return nil, fmt.Errorf("rig: %w", err)
	}
	if len(c.Instruments) == 0 {
		return nil, fmt.Errorf("rig: no instruments")
	}
	seen := map[string]bool{}
	for i, in := range c.Instruments {
		if in.ID == "" {
			return nil, fmt.Errorf("rig: instrument %d has no id", i)
		}
		if seen[in.ID] {
			return nil, fmt.Errorf("rig: instrument %q appears twice", in.ID)
		}
		seen[in.ID] = true
	}
	return &c, nil
}

// Built is an assembled rig, with whatever needs closing.
type Built struct {
	Instruments map[string]instrument.Instrument
	closers     []io.Closer
	remotes     []*cip.Client
}

// Close releases every instrument that holds a resource.
func (b *Built) Close() error {
	var first error
	for _, c := range b.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Build turns the configuration into live instruments.
//
// An unknown driver is an error rather than a silent fallback to virtual: a
// rig that quietly pretends to drive hardware is worse than one that refuses
// to start.
func (c *Config) Build() (*Built, error) {
	out := &Built{Instruments: map[string]instrument.Instrument{}}
	for _, in := range c.Instruments {
		switch in.Driver {
		case "", "virtual":
			out.Instruments[in.ID] = virtual.New(instrument.Manifest{
				ID: in.ID, Kind: in.Kind, Latency: in.Latency.Duration(),
			})
		case "sacn":
			mode := sacn.Mode(in.Mode)
			if in.Mode == "" {
				mode = sacn.ModeRGB
			}
			l, err := sacn.New(sacn.Config{
				ID: in.ID, Universe: in.Universe, Addr: in.Addr,
				Start: in.Start, Mode: mode, Latency: in.Latency.Duration(),
			})
			if err != nil {
				out.Close()
				return nil, err
			}
			out.Instruments[in.ID] = l
			out.closers = append(out.closers, l)
		case "cip":
			// The manifest comes from the node rather than from this file.
			// The device is the only thing that actually knows its own
			// latency, and a rig file that disagrees with the hardware is
			// worse than no rig file at all.
			wait := in.RemoteTimeout.Duration()
			if wait <= 0 {
				wait = 2 * time.Second
			}
			c, err := cip.Dial(in.Addr, wait, in.Secret)
			if err != nil {
				out.Close()
				return nil, err
			}
			out.Instruments[in.ID] = c
			out.closers = append(out.closers, c)
			out.remotes = append(out.remotes, c)
		case "motion":
			cfg := motion.Config{
				ID: in.ID, Addr: in.Addr,
				Format:  motion.Format(in.Format),
				Latency: in.Latency.Duration(),
			}
			if t := in.Travel; t != nil {
				cfg.Limits = motion.Limits{
					Surge: t.Surge, Sway: t.Sway, Heave: t.Heave,
					Roll: t.Roll, Pitch: t.Pitch, Yaw: t.Yaw,
				}
			}
			m, err := motion.New(cfg)
			if err != nil {
				out.Close()
				return nil, err
			}
			out.Instruments[in.ID] = m
			out.closers = append(out.closers, m)
		default:
			out.Close()
			return nil, fmt.Errorf("rig: instrument %q has unknown driver %q", in.ID, in.Driver)
		}
	}
	return out, nil
}

// Heartbeat tells every remote node the conductor is still alive.
//
// A node that stops hearing this drives itself safe without being asked, which
// is the only protection that survives the conductor crashing.
func (b *Built) Heartbeat() {
	for _, c := range b.remotes {
		_ = c.Heartbeat()
	}
}

// Safe orders every remote node to its safe state immediately.
func (b *Built) Safe() {
	for _, c := range b.remotes {
		_ = c.Safe()
	}
}

// Remotes reports how many instruments are reached over the network.
func (b *Built) Remotes() int { return len(b.remotes) }
