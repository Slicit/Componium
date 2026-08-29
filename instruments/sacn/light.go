package sacn

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Slicit/Componium/internal/instrument"
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
// It owns its own socket and its own sequence numbering, because that is
// device I/O rather than show timing. It does not own a ticker: keepalive is
// started explicitly by the caller.
type Light struct {
	cfg  Config
	conn net.Conn

	mu   sync.Mutex
	seq  uint8
	data [Slots]byte
	cid  [16]byte
}

// New dials the destination and prepares the fixture.
func New(cfg Config) (*Light, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("sacn: ID is required")
	}
	if cfg.Start < 1 || cfg.Start+cfg.Mode.width()-1 > Slots {
		return nil, fmt.Errorf("sacn: start address %d does not fit a %s fixture in 512 channels", cfg.Start, cfg.Mode)
	}
	addr := cfg.Addr
	if addr == "" {
		addr = MulticastAddr(cfg.Universe)
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("sacn: dial %s: %w", addr, err)
	}
	if cfg.Latency == 0 {
		cfg.Latency = DefaultLatency
	}
	if cfg.SourceName == "" {
		cfg.SourceName = "componium"
	}
	l := &Light{cfg: cfg, conn: conn}
	rand.Read(l.cid[:])
	return l, nil
}

func (l *Light) Close() error { return l.conn.Close() }

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
	l.mu.Lock()
	defer l.mu.Unlock()

	base := l.cfg.Start - 1
	switch l.cfg.Mode {
	case ModeRGB, ModeRGBW:
		l.data[base+0] = level(d.Cue.Params["r"])
		l.data[base+1] = level(d.Cue.Params["g"])
		l.data[base+2] = level(d.Cue.Params["b"])
		if l.cfg.Mode == ModeRGBW {
			l.data[base+3] = level(d.Cue.Params["w"])
		}
	default:
		l.data[base] = level(d.Cue.Params["intensity"])
	}
	if d.Cue.Action == "off" {
		for i := 0; i < l.cfg.Mode.width(); i++ {
			l.data[base+i] = 0
		}
	}
	return l.send()
}

// send transmits the current universe. The caller must hold the lock.
func (l *Light) send() error {
	p := &Packet{
		CID:        l.cid,
		SourceName: l.cfg.SourceName,
		Universe:   l.cfg.Universe,
		Priority:   100,
		Sequence:   l.seq,
		Data:       l.data,
	}
	l.seq++
	_, err := l.conn.Write(p.Marshal())
	return err
}

// Keepalive retransmits the current state until ctx is done.
//
// E1.31 receivers commonly drop back to their idle state after about 2.5
// seconds without traffic, so a fixture set once and left alone will go dark
// on its own. Componium cues are sparse by nature, so something has to keep
// talking. It is the caller's goroutine, deliberately: nothing in Componium
// starts a goroutine the caller did not ask for.
func (l *Light) Keepalive(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			l.mu.Lock()
			err := l.send()
			l.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
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
