package conductor_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Slicit/Componium/instruments/virtual"
	"github.com/Slicit/Componium/internal/clock"
	"github.com/Slicit/Componium/internal/conductor"
	"github.com/Slicit/Componium/internal/instrument"
)

var origin = time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

const tick = 5 * time.Millisecond

// fan is the recurring example: a real fan takes over a second to move any air.
func fan(latency time.Duration) *virtual.Instrument {
	return virtual.New(instrument.Manifest{
		ID:      "wind.main",
		Kind:    "wind",
		Latency: latency,
		Ramp:    instrument.Ramp{Up: 1800 * time.Millisecond, Down: 3 * time.Second},
	})
}

// play drives the conductor from one media time to another, one tick at a
// time, with a clock that is playing and as precise as stated.
func play(c *conductor.Conductor, from, to, precision time.Duration) {
	for m := from; m <= to; m += tick {
		c.Tick(origin.Add(m), clock.Reading{
			Media:     m,
			Rate:      1,
			Precision: precision,
			State:     clock.StatePlaying,
			Anchored:  true,
		})
	}
}

// TestCueIsDispatchedEarlyByTheInstrumentsLatency is the milestone. Everything
// else in M1 exists to make this assertion possible.
func TestCueIsDispatchedEarlyByTheInstrumentsLatency(t *testing.T) {
	const latency = 1200 * time.Millisecond
	const cueAt = 10 * time.Second

	f := fan(latency)
	c := conductor.New()
	if err := c.Register(f); err != nil {
		t.Fatal(err)
	}
	if err := c.Load([]instrument.Cue{{
		At:         cueAt,
		Instrument: "wind.main",
		Action:     "gust",
		Params:     map[string]float64{"intensity": 0.8},
	}}); err != nil {
		t.Fatal(err)
	}

	play(c, 0, 12*time.Second, time.Millisecond)

	got := f.Received()
	if len(got) != 1 {
		t.Fatalf("got %d dispatches, want 1", len(got))
	}
	want := cueAt - latency // 8.8s
	if d := got[0].Media - want; d > tick || d < 0 {
		t.Errorf("dispatched at media %v, want %v within one %v tick",
			got[0].Media, want, tick)
	}
	t.Logf("cue at %v, latency %v, dispatched at %v", cueAt, latency, got[0].Media)
}

func TestImpreciseClockRefusesTheCue(t *testing.T) {
	f := fan(0)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At:                5 * time.Second,
		Instrument:        "wind.main",
		Action:            "shake",
		RequiredPrecision: 10 * time.Millisecond,
	}})

	// 70ms is roughly what VLC's HTTP interface can manage.
	play(c, 0, 6*time.Second, 70*time.Millisecond)

	if f.Count() != 0 {
		t.Fatalf("dispatched %d cues, want 0", f.Count())
	}
	skips := c.Skips()
	if len(skips) != 1 || skips[0].Reason != conductor.SkipImprecise {
		t.Fatalf("skips %v, want one SkipImprecise", skips)
	}
}

func TestNothingFiresWhilePaused(t *testing.T) {
	f := fan(0)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{At: 2 * time.Second, Instrument: "wind.main", Action: "gust"}})

	for m := time.Duration(0); m <= 4*time.Second; m += tick {
		c.Tick(origin.Add(m), clock.Reading{
			Media:     2 * time.Second, // position exists but is frozen
			Rate:      0,
			Precision: time.Millisecond,
			State:     clock.StatePaused,
			Anchored:  true,
		})
	}
	if f.Count() != 0 {
		t.Errorf("dispatched %d cues while paused, want 0", f.Count())
	}
}

func TestSeekForwardSkipsRatherThanBursts(t *testing.T) {
	f := fan(0)
	c := conductor.New()
	c.Register(f)

	var cues []instrument.Cue
	for i := 1; i <= 20; i++ {
		cues = append(cues, instrument.Cue{
			At:         time.Duration(i) * time.Second,
			Instrument: "wind.main",
			Action:     "gust",
		})
	}
	c.Load(cues)

	play(c, 0, 2*time.Second, time.Millisecond)
	before := f.Count()

	// Jump from 2s to 15s, as a viewer skipping a scene would.
	play(c, 15*time.Second, 16*time.Second, time.Millisecond)

	if got := f.Count() - before; got > 2 {
		t.Errorf("fired %d cues immediately after a seek, want at most 2", got)
	}
	seeked := 0
	for _, s := range c.Skips() {
		if s.Reason == conductor.SkipSeeked {
			seeked++
		}
	}
	if seeked == 0 {
		t.Error("no cues recorded as skipped by the seek")
	}
}

func TestSeekBackwardRearmsCues(t *testing.T) {
	f := fan(0)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{At: 3 * time.Second, Instrument: "wind.main", Action: "gust"}})

	play(c, 0, 4*time.Second, time.Millisecond)
	if f.Count() != 1 {
		t.Fatalf("got %d dispatches on first pass, want 1", f.Count())
	}
	// Rewind to the start and play through the cue again.
	play(c, 0, 4*time.Second, time.Millisecond)
	if f.Count() != 2 {
		t.Errorf("got %d dispatches after rewinding past the cue, want 2", f.Count())
	}
}

func TestCueTooEarlyForTheInstrumentIsReportedUnreachable(t *testing.T) {
	f := fan(1200 * time.Millisecond)
	c := conductor.New()
	c.Register(f)
	// A cue half a second in, for an instrument needing 1.2s of warning.
	c.Load([]instrument.Cue{{At: 500 * time.Millisecond, Instrument: "wind.main", Action: "gust"}})

	un := c.Unreachable()
	if len(un) != 1 {
		t.Fatalf("got %d unreachable cues, want 1", len(un))
	}
}

func TestInstrumentFailureIsRecordedNotSwallowed(t *testing.T) {
	f := fan(0)
	boom := errors.New("serial port closed")
	f.FailAfter(0, boom)

	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{At: time.Second, Instrument: "wind.main", Action: "gust"}})
	play(c, 0, 2*time.Second, time.Millisecond)

	skips := c.Skips()
	if len(skips) != 1 || skips[0].Reason != conductor.SkipFailed {
		t.Fatalf("skips %v, want one SkipFailed", skips)
	}
	if !errors.Is(skips[0].Err, boom) {
		t.Errorf("err %v, want %v", skips[0].Err, boom)
	}
	if c.Dispatched() != 0 {
		t.Errorf("counted %d dispatched, want 0 when the instrument failed", c.Dispatched())
	}
}

func TestDuplicateRegistrationAndUnknownInstrument(t *testing.T) {
	c := conductor.New()
	if err := c.Register(fan(0)); err != nil {
		t.Fatal(err)
	}
	if err := c.Register(fan(0)); err == nil {
		t.Error("registering the same ID twice succeeded, want an error")
	}
	if err := c.Load([]instrument.Cue{{At: time.Second, Instrument: "fog.left"}}); err == nil {
		t.Error("loading a cue for an unregistered instrument succeeded, want an error")
	}
}
