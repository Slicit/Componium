// Package safety keeps a rig from hurting anyone or itself.
//
// The ordering principle from ADR 0001: an instrument must be safe when the
// conductor is absent, malicious, or wrong. Three mechanisms, in decreasing
// order of how much they can be trusted:
//
//  1. The instrument's own limits, enforced in the instrument or its firmware.
//     Nothing here can override those, and nothing should try.
//  2. A watchdog. If the show stops feeding heartbeats, everything is driven
//     to its safe state. This catches the conductor wedging or crashing, which
//     is precisely when a fogger left open matters most.
//  3. Limits enforced here, which are a convenience and a second line, not the
//     real defence.
//
// Like the clock and the conductor, the supervisor is passive: it is told the
// time rather than reading it, so every timing rule is testable instantly.
package safety

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// DefaultTimeout is how long the supervisor waits without a heartbeat before
// driving everything to its safe state.
//
// 300ms: long enough to survive a slow tick or a garbage collection pause,
// short enough that a wedged conductor cannot empty a fog machine.
const DefaultTimeout = 300 * time.Millisecond

// StopReason says why a rig was stopped.
type StopReason string

const (
	// StopManual is an operator pressing the button.
	StopManual StopReason = "manual all-stop"
	// StopWatchdog means the heartbeat stopped arriving.
	StopWatchdog StopReason = "watchdog: no heartbeat"
	// StopLimit means an instrument exceeded its declared limits.
	StopLimit StopReason = "instrument limit exceeded"
)

// Event records something the supervisor did, so that a rig which suddenly
// went quiet can be explained afterwards.
type Event struct {
	At         time.Time
	Reason     StopReason
	Instrument string
	Detail     string
}

// Supervisor guards a set of instruments.
type Supervisor struct {
	timeout time.Duration

	mu       sync.Mutex
	guards   map[string]*guard
	lastBeat time.Time
	stopped  bool
	events   []Event
}

// New returns a supervisor. A zero timeout means DefaultTimeout.
func New(timeout time.Duration) *Supervisor {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Supervisor{timeout: timeout, guards: map[string]*guard{}}
}

// Guard wraps an instrument so that every dispatch passes through the
// supervisor. The returned instrument is what should be registered with the
// conductor.
func (s *Supervisor) Guard(inner instrument.Instrument) instrument.Instrument {
	m := inner.Manifest()
	g := &guard{sup: s, inner: inner, manifest: m}
	s.mu.Lock()
	s.guards[m.ID] = g
	s.mu.Unlock()
	return g
}

// Heartbeat tells the supervisor the show is still alive.
func (s *Supervisor) Heartbeat(now time.Time) {
	s.mu.Lock()
	s.lastBeat = now
	s.mu.Unlock()
}

// Check is called periodically with the current time. If the heartbeat has
// stopped arriving, everything is driven to its safe state.
//
// The first Check before any heartbeat starts the clock rather than tripping,
// so that starting up is not itself a fault.
func (s *Supervisor) Check(now time.Time) {
	s.mu.Lock()
	if s.lastBeat.IsZero() {
		s.lastBeat = now
		s.mu.Unlock()
		return
	}
	overdue := now.Sub(s.lastBeat) > s.timeout
	already := s.stopped
	s.mu.Unlock()

	if overdue && !already {
		s.AllStop(now, StopWatchdog, fmt.Sprintf("no heartbeat for %v", s.timeout))
	}
}

// AllStop drives every instrument to its safe state and latches, refusing
// further dispatches until Reset.
//
// It bypasses the scheduler entirely and does not care what the score wanted.
func (s *Supervisor) AllStop(now time.Time, reason StopReason, detail string) {
	s.mu.Lock()
	s.stopped = true
	s.events = append(s.events, Event{At: now, Reason: reason, Detail: detail})
	guards := make([]*guard, 0, len(s.guards))
	for _, g := range s.guards {
		guards = append(guards, g)
	}
	s.mu.Unlock()

	for _, g := range guards {
		g.forceSafe(now)
	}
}

// Reset clears the latch so the rig can be used again.
func (s *Supervisor) Reset(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = false
	s.lastBeat = now
	for _, g := range s.guards {
		g.reset()
	}
}

// Stopped reports whether the latch is set.
func (s *Supervisor) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// Events returns everything the supervisor has done.
func (s *Supervisor) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *Supervisor) record(e Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

// Watch runs the watchdog until ctx is done.
//
// It must run independently of whatever is calling Heartbeat. A watchdog
// checked from inside the show loop could never notice the show loop stopping,
// which is the one failure it exists to catch.
func Watch(ctx context.Context, s *Supervisor, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultTimeout / 3
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.Check(now)
		}
	}
}
