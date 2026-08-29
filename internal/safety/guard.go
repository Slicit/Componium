package safety

import (
	"fmt"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// guard is one instrument wrapped by the supervisor.
//
// It tracks how long the instrument has been active, so that a fogger told to
// run and then forgotten about is shut off by something other than the
// operator noticing.
type guard struct {
	sup      *Supervisor
	inner    instrument.Instrument
	manifest instrument.Manifest

	mu       sync.Mutex
	active   time.Time
	isActive bool
	window   []span
	forced   bool
}

type span struct{ from, to time.Time }

func (g *guard) Manifest() instrument.Manifest { return g.manifest }

func (g *guard) Dispatch(d instrument.Dispatch) error {
	if g.sup.Stopped() {
		return fmt.Errorf("safety: rig is stopped, refusing %s for %s", d.Cue.Action, g.manifest.ID)
	}
	now := d.Wall
	if now.IsZero() {
		now = time.Now()
	}

	if isActive(d.Cue) {
		if err := g.checkLimits(now); err != nil {
			g.sup.record(Event{At: now, Reason: StopLimit, Instrument: g.manifest.ID, Detail: err.Error()})
			g.forceSafe(now)
			return err
		}
	}
	g.mark(now, isActive(d.Cue))
	return g.inner.Dispatch(d)
}

// isActive decides whether a cue leaves the instrument doing something.
//
// Deliberately crude: any non-zero parameter counts as active. An instrument
// that knows better should enforce its own limits, which is where the real
// defence belongs anyway.
func isActive(c instrument.Cue) bool {
	switch c.Action {
	case "off", "safe", "stop":
		return false
	}
	for _, v := range c.Params {
		if v > 0 {
			return true
		}
	}
	return len(c.Params) == 0 && c.Action != ""
}

func (g *guard) checkLimits(now time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if max := g.manifest.MaxContinuous; max > 0 && g.isActive {
		if run := now.Sub(g.active); run > max {
			return fmt.Errorf("%s active for %v, limit is %v", g.manifest.ID, run.Round(time.Millisecond), max)
		}
	}

	duty := g.manifest.DutyCycle
	if duty <= 0 {
		return nil
	}
	win := g.manifest.DutyWindow
	if win <= 0 {
		win = time.Minute
	}
	cutoff := now.Add(-win)
	var busy time.Duration
	kept := g.window[:0]
	for _, s := range g.window {
		if s.to.Before(cutoff) {
			continue
		}
		from := s.from
		if from.Before(cutoff) {
			from = cutoff
		}
		busy += s.to.Sub(from)
		kept = append(kept, s)
	}
	g.window = kept
	if g.isActive {
		from := g.active
		if from.Before(cutoff) {
			from = cutoff
		}
		busy += now.Sub(from)
	}
	if ratio := busy.Seconds() / win.Seconds(); ratio > duty {
		return fmt.Errorf("%s at %.0f percent duty over %v, limit is %.0f percent",
			g.manifest.ID, ratio*100, win, duty*100)
	}
	return nil
}

func (g *guard) mark(now time.Time, on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case on && !g.isActive:
		g.isActive = true
		g.active = now
	case !on && g.isActive:
		g.isActive = false
		g.window = append(g.window, span{from: g.active, to: now})
	}
}

// forceSafe drives the instrument to its declared safe state, bypassing every
// check including the stop latch, because the latch is the reason we are here.
func (g *guard) forceSafe(now time.Time) {
	g.mu.Lock()
	if g.isActive {
		g.window = append(g.window, span{from: g.active, to: now})
	}
	g.isActive = false
	g.forced = true
	params := g.manifest.SafeState
	g.mu.Unlock()

	if params == nil {
		params = map[string]float64{}
	}
	_ = g.inner.Dispatch(instrument.Dispatch{
		Cue: instrument.Cue{
			Instrument: g.manifest.ID,
			Action:     "safe",
			Params:     params,
		},
		Wall: now,
	})
}

func (g *guard) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forced = false
	g.isActive = false
	g.window = nil
}
