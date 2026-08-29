package conductor_test

import (
	"testing"
	"time"

	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/clock"
	"github.com/Slicit/componium/internal/conductor"
	"github.com/Slicit/componium/internal/instrument"
)

// fogger declares a limit, which is what forces every cue for it to have an
// end.
func fogger(latency, maxContinuous time.Duration) *virtual.Instrument {
	return virtual.New(instrument.Manifest{
		ID: "fog.left", Kind: "fog", Latency: latency,
		MaxContinuous: maxContinuous,
		SafeState:     map[string]float64{"output": 0},
	})
}

func actions(v *virtual.Instrument) []string {
	var out []string
	for _, d := range v.Received() {
		out = append(out, d.Cue.Action)
	}
	return out
}

func paused(media time.Duration) clock.Reading {
	return clock.Reading{Media: media, State: clock.StatePaused, Precision: time.Millisecond}
}

func TestHoldBecomesAStartAndAStop(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	if err := c.Load([]instrument.Cue{{
		At: 2 * time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 4 * time.Second,
	}}); err != nil {
		t.Fatal(err)
	}

	play(c, 0, 8*time.Second, time.Millisecond)

	got := f.Received()
	if len(got) != 2 {
		t.Fatalf("got %d dispatches (%v), want a burst and a stop", len(got), actions(f))
	}
	if got[0].Cue.Action != "burst" || got[1].Cue.Action != instrument.ActionStop {
		t.Fatalf("actions were %v", actions(f))
	}
	// The effect should end 4s after it began.
	if d := got[1].Media - got[0].Media; d < 4*time.Second-tick || d > 4*time.Second+tick {
		t.Errorf("span lasted %v, want 4s", d)
	}
}

func TestStopIsLatencyCompensatedToo(t *testing.T) {
	const latency = 2 * time.Second
	f := fogger(latency, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: 10 * time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 3 * time.Second,
	}})

	play(c, 0, 15*time.Second, time.Millisecond)

	got := f.Received()
	if len(got) != 2 {
		t.Fatalf("got %v", actions(f))
	}
	// Effect should end at 13s, so the stop is sent at 11s.
	if want := 11 * time.Second; got[1].Media < want-tick || got[1].Media > want+tick {
		t.Errorf("stop sent at %v, want %v", got[1].Media, want)
	}
}

func TestMomentaryCueGetsNoStop(t *testing.T) {
	f := virtual.New(instrument.Manifest{ID: "fog.left", Kind: "light"})
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{At: time.Second, Instrument: "fog.left", Action: "flash"}})
	play(c, 0, 3*time.Second, time.Millisecond)

	if got := actions(f); len(got) != 1 || got[0] != "flash" {
		t.Errorf("got %v, want a single flash", got)
	}
}

// A viewer who pauses for twenty minutes should not come back to a room full
// of smoke.
func TestPauseStopsARunningSpan(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 30 * time.Second,
	}})

	play(c, 0, 2*time.Second, time.Millisecond)
	if c.Running() != 1 {
		t.Fatal("the span did not start")
	}
	c.Tick(origin.Add(2*time.Second), paused(2*time.Second))

	if c.Running() != 0 {
		t.Error("still running after a pause")
	}
	if got := actions(f); got[len(got)-1] != instrument.ActionStop {
		t.Errorf("actions %v, want a stop at the end", got)
	}
}

// Seeking out of the middle of a span would otherwise leave the instrument
// going with nothing left in the schedule to end it.
func TestSeekingPastTheEndOfASpanStillStopsIt(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 10 * time.Second,
	}})

	play(c, 0, 2*time.Second, time.Millisecond)
	if c.Running() != 1 {
		t.Fatal("the span did not start")
	}
	// Jump well past where the span should have ended.
	play(c, 60*time.Second, 61*time.Second, time.Millisecond)

	if c.Running() != 0 {
		t.Error("still running after seeking past the end of the span")
	}
	if got := actions(f); got[len(got)-1] != instrument.ActionStop {
		t.Errorf("actions %v, want a stop", got)
	}
}

func TestSeekingBackwardsOutOfASpanStopsIt(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: 10 * time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 20 * time.Second,
	}})

	play(c, 0, 12*time.Second, time.Millisecond)
	if c.Running() != 1 {
		t.Fatal("the span did not start")
	}
	play(c, 0, time.Second, time.Millisecond) // rewind to the beginning

	if c.Running() != 0 {
		t.Error("still running after rewinding out of the span")
	}
}

// Declining to end an effect because the clock is vague would leave hardware
// running, which is strictly worse than ending it slightly off.
func TestStopIsNeverRefusedForImprecision(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 2 * time.Second,
		RequiredPrecision: time.Hour, // so the start is allowed
	}})

	// A clock far too vague for anything, but the stop must still land.
	play(c, 0, 5*time.Second, 500*time.Millisecond)

	if got := actions(f); len(got) != 2 || got[1] != instrument.ActionStop {
		t.Errorf("actions %v, want the stop to have fired anyway", got)
	}
}

// The difference between a score that is merely wrong and one that empties a
// fog machine.
func TestLimitedInstrumentRefusesACueWithNoEnd(t *testing.T) {
	c := conductor.New()
	c.Register(fogger(0, 10*time.Second))
	err := c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1},
	}})
	if err == nil {
		t.Fatal("a cue with no duration was accepted for an instrument with a limit")
	}
}

func TestDurationOverTheDeclaredLimitIsRefused(t *testing.T) {
	c := conductor.New()
	c.Register(fogger(0, 5*time.Second))
	err := c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 30 * time.Second,
	}})
	if err == nil {
		t.Fatal("a 30s span was accepted on an instrument limited to 5s")
	}
}

func TestUnlimitedInstrumentNeedsNoDuration(t *testing.T) {
	c := conductor.New()
	c.Register(virtual.New(instrument.Manifest{ID: "fog.left", Kind: "light"}))
	if err := c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "set",
	}}); err != nil {
		t.Fatalf("a light with no declared limit was refused: %v", err)
	}
}

// The clock predicts forward from its last anchor, so when the next anchor
// lands it can sit a fraction of a millisecond behind the prediction and media
// time steps backwards. Treating that as a seek stopped a running span 364ms
// after it started, on a real player, which is how this was found.
func TestTinyBackwardClockJitterIsNotASeek(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 10 * time.Second,
	}})

	// Play into the span.
	play(c, 0, 2*time.Second, time.Millisecond)
	if c.Running() != 1 {
		t.Fatal("the span did not start")
	}

	// Now jitter: forward, back a hair, forward again, exactly as an anchor
	// landing behind a prediction looks.
	media := 2 * time.Second
	for i := 0; i < 200; i++ {
		media += 5 * time.Millisecond
		jittered := media
		if i%3 == 0 {
			jittered -= 300 * time.Microsecond
		}
		c.Tick(origin.Add(media), clock.Reading{
			Media: jittered, Rate: 1, Precision: time.Millisecond,
			State: clock.StatePlaying, Anchored: true,
		})
	}

	if c.Running() != 1 {
		t.Error("the span was stopped by sub-millisecond clock jitter")
	}
	for _, a := range actions(f) {
		if a == instrument.ActionStop {
			t.Fatalf("a stop was dispatched during jitter: %v", actions(f))
		}
	}
}

// A real seek still has to be caught, or the tolerance is useless.
func TestARealBackwardSeekIsStillDetected(t *testing.T) {
	f := fogger(0, time.Minute)
	c := conductor.New()
	c.Register(f)
	c.Load([]instrument.Cue{{
		At: 10 * time.Second, Instrument: "fog.left", Action: "burst",
		Params: map[string]float64{"output": 1}, Hold: 30 * time.Second,
	}})

	play(c, 0, 12*time.Second, time.Millisecond)
	if c.Running() != 1 {
		t.Fatal("the span did not start")
	}
	// Two seconds backwards is a seek by any reading.
	c.Tick(origin.Add(13*time.Second), clock.Reading{
		Media: 10 * time.Second, Rate: 1, Precision: time.Millisecond,
		State: clock.StatePlaying, Anchored: true,
	})
	if c.Running() != 0 {
		t.Error("a two second backward seek did not stop the span")
	}
}
