// Package rig reads the description of one physical installation.
//
// A score says what should happen. A rig says what is in the room and how to
// reach it. Keeping them apart is what lets the same score play on somebody
// else's hardware: the score names "light.ambient", the rig knows that on this
// installation it is an RGB fixture at DMX address 10 on universe 1.
package rig

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/Slicit/componium/instruments/motion"
	"github.com/Slicit/componium/instruments/sacn"
	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/cip"
	"github.com/Slicit/componium/internal/colour"
	"github.com/Slicit/componium/internal/instrument"
)

// Config is a rig file.
type Config struct {
	// secrets resolves a board's shared secret by address, for entries
	// with none of their own. Never read from or written to the file: a
	// rig is a thing you commit, and a secret is not.
	secrets func(addr string) string `toml:"-"`

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

	// Brightness and Saturation correct this fixture on the way out, in
	// -1 to +1, added to what the score asks for. Zero, and absent, change
	// nothing.
	//
	// Here rather than in the score because it describes a strip rather
	// than a film: two reels of LEDs with the same part number reach the
	// same numbers differently, and a score edited to suit one of them
	// plays wrong on every other rig. Here rather than in the studio
	// because a show needs it too, and a correction that existed only in
	// preview would make a room look right until it played.
	Brightness float64 `toml:"brightness,omitempty"`
	Saturation float64 `toml:"saturation,omitempty"`

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
	return Parse(b)
}

// Parse reads a rig from bytes, with the same checks Load applies.
//
// Separate from Load so that a rig arriving from somewhere other than a
// path can be held to the same standard before it is written anywhere. An
// imported file that parses only after it is on the shelf is a show that
// does not start, discovered by whoever is standing in the room.
func Parse(b []byte) (*Config, error) {
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
	// universes are shared by the fixtures in them. A DMX universe carries
	// several fixtures at different start addresses, and E1.31 sends all 512
	// channels in every packet, so a fixture that owned one would blank every
	// other fixture on it with each frame it sent.
	universes []*sacn.Universe
	// trims adjust what reaches each fixture. Seeded from the file and
	// movable while a film plays, which is what makes them knobs.
	trims trims
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
// Secrets resolves a board's shared secret from its address, for entries that
// do not carry one of their own.
//
// A function rather than a lookup table so that the rig package does not have
// to know where secrets are kept, and so it cannot import the package that
// keeps them, which imports this one.
//
// Set it before Build. Nil means every entry is on its own, which is the right
// behaviour for a rig used without a studio.
func (c *Config) UseSecrets(from func(addr string) string) {
	c.secrets = from
}

func (c *Config) Build() (*Built, error) {
	// Captured here because c is shadowed further down by the cip client, and
	// a resolver that silently became nil would be a board that silently went
	// unauthenticated.
	secretFor := c.secrets

	out := &Built{Instruments: map[string]instrument.Instrument{}}
	// Fixtures asking for the same universe get the same one, and entries
	// pointing at the same board get the same client.
	universes := map[string]*sacn.Universe{}
	nodes := map[string]*cip.Client{}
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
			key := sacn.Key(in.Universe, in.Addr)
			u, ok := universes[key]
			if !ok {
				var err error
				u, err = sacn.Dial(in.Universe, in.Addr, "")
				if err != nil {
					out.Close()
					return nil, err
				}
				universes[key] = u
				out.universes = append(out.universes, u)
				out.closers = append(out.closers, u)
			}
			l, err := sacn.On(u, sacn.Config{
				ID: in.ID, Universe: in.Universe, Addr: in.Addr,
				Start: in.Start, Mode: mode, Latency: in.Latency.Duration(),
			})
			if err != nil {
				out.Close()
				return nil, err
			}
			out.Instruments[in.ID] = l
		case "cip":
			// The manifests come from the node rather than from this file. The
			// device is the only thing that actually knows its own latency, and
			// a rig file that disagrees with the hardware is worse than no rig
			// file at all.
			//
			// One client per board, shared by every entry pointing at it. A
			// board carries several devices since ADR 0007, and a client per
			// entry would mean a socket, a heartbeat and a watchdog each, for
			// one board that only has one of any of them.
			wait := in.RemoteTimeout.Duration()
			if wait <= 0 {
				wait = 2 * time.Second
			}
			c, ok := nodes[in.Addr]
			if !ok {
				var err error
				// The entry's own secret first, then the installation's.
				// An entry that names one is an entry that meant it.
				secret := in.Secret
				if secret == "" && secretFor != nil {
					secret = secretFor(in.Addr)
				}
				c, err = cip.Dial(in.Addr, wait, secret)
				if err != nil {
					out.Close()
					return nil, err
				}
				nodes[in.Addr] = c
				out.closers = append(out.closers, c)
				out.remotes = append(out.remotes, c)
			}
			device, found := c.Device(in.ID)
			if !found {
				out.Close()
				// Naming what is there, because the useful sentence is what
				// the board actually has rather than what it does not. This
				// is what the old "one node is one instrument" error became:
				// two entries at one address are ordinary now, and the fault
				// is asking for a device that was never configured.
				return nil, fmt.Errorf(
					"rig: %s has no instrument %q; it announced %v",
					in.Addr, in.ID, c.Names())
			}
			out.Instruments[in.ID] = device
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

	/* Trims last, over whatever each driver produced, so that every
	 * instrument is corrected the same way whether it is a strip on a
	 * board, a fixture on a universe, or a virtual one being logged.
	 * Wrapping at each driver instead would be four places to forget. */
	for _, in := range c.Instruments {
		out.trims.set(in.ID, colour.Trim{
			Brightness: colour.Clamp(in.Brightness),
			Saturation: colour.Clamp(in.Saturation),
		})
	}
	for id, inst := range out.Instruments {
		out.Instruments[id] = trimmed{inner: inst, id: id, of: out.trims.get}
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

// Keepalive keeps every sACN universe being transmitted until ctx is done.
//
// Not optional, and for a long time not called by anything, which is a bug that
// only shows on a fixture driven by cues rather than by a curve. E1.31
// receivers drop back to idle after about two and a half seconds of silence, so
// a curve track at 50Hz keeps its own fixture alive by accident and an event
// light flashes once and then goes dark on the receiver's timer rather than on
// the score's.
//
// Nothing in Componium starts a goroutine the caller did not ask for, so this
// is the caller's to run: `go built.Keepalive(ctx)`.
func (b *Built) Keepalive(ctx context.Context) {
	var wg sync.WaitGroup
	for _, u := range b.universes {
		wg.Add(1)
		go func(u *sacn.Universe) {
			defer wg.Done()
			_ = u.Keepalive(ctx, time.Second)
		}(u)
	}
	wg.Wait()
}

// Safe orders every remote node to its safe state immediately.
func (b *Built) Safe() {
	for _, c := range b.remotes {
		_ = c.Safe()
	}
}

// Remotes reports how many instruments are reached over the network.
func (b *Built) Remotes() int { return len(b.remotes) }
