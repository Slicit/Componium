// Package show runs a performance: it is the only part of Componium that owns
// a goroutine, a ticker, or the passage of time.
//
// internal/clock and internal/conductor are both deliberately passive, which
// makes them testable without waiting. Something still has to poll the player
// and drive them, and that something is here, kept small enough to read in one
// sitting because it is the one piece that cannot be tested instantly.
package show

import (
	"context"
	"fmt"
	"time"

	"github.com/Slicit/Componium/internal/clock"
	"github.com/Slicit/Componium/internal/conductor"
	"github.com/Slicit/Componium/internal/source"
)

// DefaultPollInterval is 5ms, or 200Hz.
//
// Measured cost on Debian 13: an mpv query takes about 50us, so this occupies
// roughly one percent of a core, and anchor precision is bounded by the poll
// interval rather than by the frame interval. Polling harder buys very little;
// polling much softer costs precision directly.
const DefaultPollInterval = 5 * time.Millisecond

// Config describes one performance.
type Config struct {
	Source    source.TimeSource
	Clock     *clock.Clock
	Conductor *conductor.Conductor

	// PollInterval defaults to DefaultPollInterval.
	PollInterval time.Duration

	// Now defaults to time.Now. Injectable so that the loop itself can be
	// driven deterministically in a test.
	Now func() time.Time

	// OnReading, if set, is called after every sample with the reading the
	// conductor was given. Intended for display; it runs on the hot path, so
	// it must not block.
	OnReading func(clock.Reading)

	// MaxConsecutiveErrors is how many failed polls in a row end the show.
	// Defaults to 50, which at 200Hz is a quarter of a second of silence.
	MaxConsecutiveErrors int
}

// Run polls the source and drives the clock and conductor until ctx is done,
// the source fails persistently, or Stop is reached.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Source == nil || cfg.Clock == nil || cfg.Conductor == nil {
		return fmt.Errorf("show: source, clock and conductor are all required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxConsecutiveErrors <= 0 {
		cfg.MaxConsecutiveErrors = 50
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	var consecutive int
	var lastErr error

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		pos, ok, err := cfg.Source.Position()
		// Wall time is read once and used for the sample, the reading and the
		// tick alike, so that all three agree on when this iteration happened.
		now := cfg.Now()

		if err != nil {
			consecutive++
			lastErr = err
			if consecutive >= cfg.MaxConsecutiveErrors {
				return fmt.Errorf("show: %d consecutive poll failures: %w", consecutive, lastErr)
			}
			continue
		}
		consecutive = 0

		cfg.Clock.Sample(now, pos, ok)
		r := cfg.Clock.At(now)
		cfg.Conductor.Tick(now, r)
		if cfg.OnReading != nil {
			cfg.OnReading(r)
		}
	}
}
