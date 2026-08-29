package safety_test

import (
	"testing"
	"time"

	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/instrument"
	"github.com/Slicit/componium/internal/safety"
)

var origin = time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

func fogger() *virtual.Instrument {
	return virtual.New(instrument.Manifest{
		ID: "fog.left", Kind: "fog", Latency: 2 * time.Second,
		SafeState:     map[string]float64{"output": 0},
		MaxContinuous: 5 * time.Second,
		DutyCycle:     0.25,
		DutyWindow:    time.Minute,
	})
}

func burst(at time.Time, level float64) instrument.Dispatch {
	return instrument.Dispatch{
		Wall: at,
		Cue: instrument.Cue{Instrument: "fog.left", Action: "burst",
			Params: map[string]float64{"output": level}},
	}
}

// The failure that matters most: the show wedges while a fogger is running.
func TestWatchdogDrivesEverythingSafeWhenTheHeartbeatStops(t *testing.T) {
	sup := safety.New(300 * time.Millisecond)
	f := fogger()
	g := sup.Guard(f)

	sup.Heartbeat(origin)
	if err := g.Dispatch(burst(origin, 1)); err != nil {
		t.Fatal(err)
	}

	sup.Check(origin.Add(200 * time.Millisecond))
	if sup.Stopped() {
		t.Fatal("stopped early, within the timeout")
	}
	sup.Check(origin.Add(400 * time.Millisecond))
	if !sup.Stopped() {
		t.Fatal("watchdog did not trip after the timeout")
	}

	got := f.Received()
	last := got[len(got)-1]
	if last.Cue.Action != "safe" {
		t.Errorf("last dispatch was %q, want safe", last.Cue.Action)
	}
	if last.Cue.Params["output"] != 0 {
		t.Errorf("safe state left output at %v", last.Cue.Params["output"])
	}
}

func TestStoppedRigRefusesEverything(t *testing.T) {
	sup := safety.New(0)
	f := fogger()
	g := sup.Guard(f)
	sup.AllStop(origin, safety.StopManual, "operator")

	before := f.Count()
	if err := g.Dispatch(burst(origin, 1)); err == nil {
		t.Error("dispatch accepted while stopped")
	}
	if f.Count() != before {
		t.Error("a refused dispatch still reached the instrument")
	}
}

func TestResetAllowsUseAgain(t *testing.T) {
	sup := safety.New(0)
	g := sup.Guard(fogger())
	sup.AllStop(origin, safety.StopManual, "operator")
	sup.Reset(origin)
	if sup.Stopped() {
		t.Fatal("still stopped after reset")
	}
	if err := g.Dispatch(burst(origin, 1)); err != nil {
		t.Errorf("dispatch refused after reset: %v", err)
	}
}

func TestContinuousRunLimitTrips(t *testing.T) {
	sup := safety.New(time.Hour)
	f := fogger()
	g := sup.Guard(f)
	sup.Heartbeat(origin)

	if err := g.Dispatch(burst(origin, 1)); err != nil {
		t.Fatal(err)
	}
	err := g.Dispatch(burst(origin.Add(6*time.Second), 1))
	if err == nil {
		t.Fatal("a fogger running 6s past a 5s limit was allowed")
	}
	got := f.Received()
	if got[len(got)-1].Cue.Action != "safe" {
		t.Errorf("limit breach did not force the safe state")
	}
}

func TestDutyCycleLimitTrips(t *testing.T) {
	sup := safety.New(time.Hour)
	f := virtual.New(instrument.Manifest{
		ID: "fog.left", SafeState: map[string]float64{"output": 0},
		DutyCycle: 0.10, DutyWindow: 10 * time.Second,
	})
	g := sup.Guard(f)
	sup.Heartbeat(origin)

	g.Dispatch(burst(origin, 1))
	g.Dispatch(instrument.Dispatch{Wall: origin.Add(3 * time.Second),
		Cue: instrument.Cue{Instrument: "fog.left", Action: "off"}})

	if err := g.Dispatch(burst(origin.Add(4*time.Second), 1)); err == nil {
		t.Error("duty cycle limit did not trip")
	}
}

func TestIdleInstrumentIsNotLimited(t *testing.T) {
	sup := safety.New(time.Hour)
	f := fogger()
	g := sup.Guard(f)
	sup.Heartbeat(origin)

	for i := 0; i < 100; i++ {
		off := instrument.Dispatch{
			Wall: origin.Add(time.Duration(i) * time.Second),
			Cue: instrument.Cue{Instrument: "fog.left", Action: "off",
				Params: map[string]float64{"output": 0}},
		}
		if err := g.Dispatch(off); err != nil {
			t.Fatalf("an off cue was refused: %v", err)
		}
	}
}

// Starting up must not itself look like a fault.
func TestFirstCheckDoesNotTripBeforeAnyHeartbeat(t *testing.T) {
	sup := safety.New(10 * time.Millisecond)
	sup.Guard(fogger())
	sup.Check(origin)
	if sup.Stopped() {
		t.Error("tripped on the first check, before any heartbeat")
	}
	sup.Check(origin.Add(time.Second))
	if !sup.Stopped() {
		t.Error("did not trip once the clock had started and no beat arrived")
	}
}

func TestEventsExplainWhatHappened(t *testing.T) {
	sup := safety.New(50 * time.Millisecond)
	sup.Guard(fogger())
	sup.Heartbeat(origin)
	sup.Check(origin.Add(time.Second))

	ev := sup.Events()
	if len(ev) == 0 {
		t.Fatal("no events recorded")
	}
	if ev[0].Reason != safety.StopWatchdog {
		t.Errorf("reason %q, want the watchdog", ev[0].Reason)
	}
}
