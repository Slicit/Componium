// Package conductor schedules cues against a media clock.
//
// Its one job is deciding when to hand a cue to an instrument, which is not
// the same as when the cue is written in the score. Every instrument declares
// how long it takes to do anything, and the conductor subtracts that, so a
// fogger with three seconds of lag is told three seconds early and the fog
// appears on the right frame.
//
// Like the clock, the conductor is passive: it does not poll, tick or sleep.
// The caller drives it with Tick, passing wall time and a clock reading in
// explicitly, which is what makes latency compensation testable without
// waiting for real time to pass.
package conductor

import (
	"fmt"
	"sort"
	"time"

	"github.com/Slicit/Componium/internal/clock"
	"github.com/Slicit/Componium/internal/instrument"
)

// SkipReason says why a cue was not dispatched.
type SkipReason int

const (
	// SkipSeeked means playback moved past the cue, so firing it now would
	// put the effect in the wrong place.
	SkipSeeked SkipReason = iota
	// SkipImprecise means the clock could not meet the cue's required
	// precision.
	SkipImprecise
	// SkipUnknownInstrument means no instrument with that ID is registered.
	SkipUnknownInstrument
	// SkipFailed means the instrument returned an error.
	SkipFailed
)

func (r SkipReason) String() string {
	switch r {
	case SkipSeeked:
		return "seeked past"
	case SkipImprecise:
		return "clock too imprecise"
	case SkipUnknownInstrument:
		return "unknown instrument"
	case SkipFailed:
		return "instrument failed"
	}
	return "unknown"
}

// Skip records a cue that was not dispatched, and why. Silently dropping cues
// would make a rig feel broken with no way to find out why, so nothing is
// dropped without a record.
type Skip struct {
	Cue    instrument.Cue
	Reason SkipReason
	Err    error
}

type scheduled struct {
	cue instrument.Cue
	// at is when to dispatch, in media time: the cue's own time minus the
	// instrument's declared latency. It may be negative for a cue near the
	// start of a film addressed to a slow instrument, which simply means it
	// can never be fired on time.
	at time.Duration
}

// Conductor holds a set of instruments and a schedule.
//
// It is not safe for concurrent use.
type Conductor struct {
	instruments map[string]instrument.Instrument
	sched       []scheduled
	next        int

	lastMedia    time.Duration
	haveLast     bool
	dispatched   int
	skips        []Skip
	unreachable  []instrument.Cue
	loadedCueLen int
}

// New returns an empty conductor.
func New() *Conductor {
	return &Conductor{instruments: map[string]instrument.Instrument{}}
}

// Register adds an instrument. Registering the same ID twice is an error,
// because two instruments answering to one name is never what was meant.
func (c *Conductor) Register(i instrument.Instrument) error {
	m := i.Manifest()
	if m.ID == "" {
		return fmt.Errorf("instrument has no ID")
	}
	if _, dup := c.instruments[m.ID]; dup {
		return fmt.Errorf("instrument %q already registered", m.ID)
	}
	c.instruments[m.ID] = i
	return nil
}

// Load builds the schedule, converting each cue's score time into a dispatch
// time by subtracting its instrument's latency, then ordering by that.
//
// Cues whose dispatch time is before the start of the media are reported as
// unreachable rather than quietly fired late: an effect that physically
// cannot happen on time is a fact about the score that the author should see.
func (c *Conductor) Load(cues []instrument.Cue) error {
	c.sched = c.sched[:0]
	c.unreachable = c.unreachable[:0]
	c.next = 0
	c.haveLast = false
	c.loadedCueLen = len(cues)

	for _, cue := range cues {
		inst, ok := c.instruments[cue.Instrument]
		if !ok {
			return fmt.Errorf("cue at %v addresses unregistered instrument %q", cue.At, cue.Instrument)
		}
		at := cue.At - inst.Manifest().Latency
		if at < 0 {
			c.unreachable = append(c.unreachable, cue)
		}
		c.sched = append(c.sched, scheduled{cue: cue, at: at})
	}
	sort.SliceStable(c.sched, func(i, j int) bool { return c.sched[i].at < c.sched[j].at })
	return nil
}

// Unreachable lists cues that cannot be dispatched early enough, because the
// instrument's latency exceeds the cue's own position in the media.
func (c *Conductor) Unreachable() []instrument.Cue { return c.unreachable }

// Tick advances the schedule to the given clock reading, dispatching every cue
// whose moment has arrived.
//
// Nothing is dispatched unless the clock says playback is actually running.
// A paused player still has a position, and firing cues against it would empty
// a fog machine into a motionless room.
func (c *Conductor) Tick(wall time.Time, r clock.Reading) {
	if r.State != clock.StatePlaying {
		c.haveLast = false
		return
	}

	// A seek moves media time discontinuously in either direction, and the
	// cursor into the schedule has to move with it. Without this, seeking
	// backwards would fire nothing ever again, and seeking forwards would
	// dump every intervening cue at once.
	if c.haveLast && (r.Media < c.lastMedia || r.Media-c.lastMedia > seekTolerance) {
		c.resync(r.Media)
	}
	c.lastMedia = r.Media
	c.haveLast = true

	for c.next < len(c.sched) && c.sched[c.next].at <= r.Media {
		s := c.sched[c.next]
		c.next++
		c.fire(s, wall, r)
	}
}

// seekTolerance is how far media time may jump forward between ticks before it
// is treated as a seek rather than as a slow tick. Generous, because a
// stuttering caller should not be mistaken for a seek.
const seekTolerance = 2 * time.Second

func (c *Conductor) fire(s scheduled, wall time.Time, r clock.Reading) {
	if s.cue.RequiredPrecision > 0 && r.Precision > s.cue.RequiredPrecision {
		c.skips = append(c.skips, Skip{Cue: s.cue, Reason: SkipImprecise})
		return
	}
	inst, ok := c.instruments[s.cue.Instrument]
	if !ok {
		c.skips = append(c.skips, Skip{Cue: s.cue, Reason: SkipUnknownInstrument})
		return
	}
	err := inst.Dispatch(instrument.Dispatch{
		Cue:       s.cue,
		Media:     r.Media,
		Wall:      wall,
		Precision: r.Precision,
	})
	if err != nil {
		c.skips = append(c.skips, Skip{Cue: s.cue, Reason: SkipFailed, Err: err})
		return
	}
	c.dispatched++
}

// resync moves the schedule cursor to media, recording any cue it steps over
// as skipped rather than firing a burst of stale cues.
func (c *Conductor) resync(media time.Duration) {
	want := sort.Search(len(c.sched), func(i int) bool { return c.sched[i].at > media })
	for i := c.next; i < want && i < len(c.sched); i++ {
		c.skips = append(c.skips, Skip{Cue: c.sched[i].cue, Reason: SkipSeeked})
	}
	c.next = want
}

// Dispatched reports how many cues have been handed to instruments.
func (c *Conductor) Dispatched() int { return c.dispatched }

// Skips reports every cue that was not dispatched, with the reason.
func (c *Conductor) Skips() []Skip { return c.skips }

// Pending reports how many cues remain in the schedule.
func (c *Conductor) Pending() int { return len(c.sched) - c.next }
