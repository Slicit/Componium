package conductor_test

import (
	"testing"
	"time"

	"github.com/Slicit/Componium/instruments/virtual"
	"github.com/Slicit/Componium/internal/clock"
	"github.com/Slicit/Componium/internal/conductor"
	"github.com/Slicit/Componium/internal/instrument"
)

func ramp(media time.Duration) map[string]float64 {
	return map[string]float64{"i": media.Seconds()}
}

func TestCurveDriverRespectsItsInterval(t *testing.T) {
	v := virtual.New(instrument.Manifest{ID: "light.x", Kind: "light"})
	d := conductor.NewCurveDriver(20 * time.Millisecond)
	d.Register(v)
	d.Add(conductor.CurveTrack{Instrument: "light.x", ValueAt: ramp})

	// Drive at 200Hz for one second. At a 20ms interval that is 50 sends,
	// not 200.
	for m := time.Duration(0); m < time.Second; m += 5 * time.Millisecond {
		d.Tick(origin.Add(m), clock.Reading{Media: m, State: clock.StatePlaying, Rate: 1})
	}
	if n := v.Count(); n < 45 || n > 55 {
		t.Errorf("sent %d curve updates in a second at 20ms, want about 50", n)
	}
}

// A curve is a value, not an event. Sending it early would send the wrong
// value, so latency is compensated by sampling ahead instead.
func TestCurveIsSampledAheadByLatency(t *testing.T) {
	const latency = 1200 * time.Millisecond
	v := virtual.New(instrument.Manifest{ID: "light.x", Kind: "light", Latency: latency})
	d := conductor.NewCurveDriver(20 * time.Millisecond)
	d.Register(v)
	d.Add(conductor.CurveTrack{Instrument: "light.x", ValueAt: ramp})

	d.Tick(origin, clock.Reading{Media: 5 * time.Second, State: clock.StatePlaying, Rate: 1})

	got := v.Received()
	if len(got) != 1 {
		t.Fatalf("%d dispatches, want 1", len(got))
	}
	// At media 5s with 1.2s of latency, the value sent should be the curve's
	// value at 6.2s.
	if want := 6.2; got[0].Cue.Params["i"] != want {
		t.Errorf("sent value %v, want %v", got[0].Cue.Params["i"], want)
	}
}

func TestCurveDriverStopsWhenPaused(t *testing.T) {
	v := virtual.New(instrument.Manifest{ID: "light.x", Kind: "light"})
	d := conductor.NewCurveDriver(time.Millisecond)
	d.Register(v)
	d.Add(conductor.CurveTrack{Instrument: "light.x", ValueAt: ramp})

	for m := time.Duration(0); m < 100*time.Millisecond; m += 5 * time.Millisecond {
		d.Tick(origin.Add(m), clock.Reading{Media: time.Second, State: clock.StatePaused})
	}
	if v.Count() != 0 {
		t.Errorf("sent %d curve updates while paused, want 0", v.Count())
	}
}

func TestCurveTrackForUnknownInstrumentIsIgnored(t *testing.T) {
	d := conductor.NewCurveDriver(time.Millisecond)
	d.Add(conductor.CurveTrack{Instrument: "nobody", ValueAt: ramp})
	d.Tick(origin, clock.Reading{Media: time.Second, State: clock.StatePlaying})
	if d.Sent() != 0 {
		t.Errorf("sent %d, want 0", d.Sent())
	}
}
