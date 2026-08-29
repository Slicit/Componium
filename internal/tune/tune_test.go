package tune

import (
	"testing"
	"time"
)

func TestSummariseOrdersAndComputes(t *testing.T) {
	in := []time.Duration{
		5 * time.Millisecond,
		1 * time.Millisecond,
		3 * time.Millisecond,
		2 * time.Millisecond,
		4 * time.Millisecond,
	}
	s := Summarise(in)

	if s.N != 5 {
		t.Errorf("N %d, want 5", s.N)
	}
	if s.Min != time.Millisecond {
		t.Errorf("Min %v, want 1ms", s.Min)
	}
	if s.Max != 5*time.Millisecond {
		t.Errorf("Max %v, want 5ms", s.Max)
	}
	if s.Mean != 3*time.Millisecond {
		t.Errorf("Mean %v, want 3ms", s.Mean)
	}
}

func TestSummariseEmptyIsZero(t *testing.T) {
	if got := Summarise(nil); got.N != 0 {
		t.Errorf("N %d for empty input, want 0", got.N)
	}
}

// The estimate must never be optimistic, for the same reason the clock's own
// precision must not be: the conductor refuses cues on the strength of it.
func TestEstimateUsesTheTailNotTheMean(t *testing.T) {
	p := Profile{
		PollInterval: 5 * time.Millisecond,
		Timer: Stats{
			Mean: 500 * time.Microsecond,
			P99:  4 * time.Millisecond,
		},
	}
	p.Estimate()
	if p.Achievable != 9*time.Millisecond {
		t.Errorf("achievable %v, want 9ms (poll + p99)", p.Achievable)
	}
	if p.Achievable <= p.PollInterval+p.Timer.Mean {
		t.Error("estimate used the mean, which would be optimistic")
	}
}

func TestEstimatePenalisesCoarsePlayers(t *testing.T) {
	// A player like VLC, updating every 247ms and costing 21ms to ask, cannot
	// reach the precision a per-frame player can.
	coarse := Profile{
		PollInterval: 5 * time.Millisecond,
		Timer:        Stats{P99: 4 * time.Millisecond},
		UpdatePeriod: 247 * time.Millisecond,
		Query:        Stats{P95: 21 * time.Millisecond},
	}
	fine := Profile{
		PollInterval: 5 * time.Millisecond,
		Timer:        Stats{P99: 4 * time.Millisecond},
		UpdatePeriod: 0,
	}
	coarse.Estimate()
	fine.Estimate()
	if !(coarse.Achievable > fine.Achievable) {
		t.Errorf("coarse %v not worse than fine %v", coarse.Achievable, fine.Achievable)
	}
}

func TestProfileRoundTrips(t *testing.T) {
	path := t.TempDir() + "/p.json"
	want := &Profile{
		Machine: "claude-machine-02", Player: "mpv", PlayerVersion: "0.40.0",
		Created:      time.Now().UTC().Truncate(time.Second),
		Timer:        Stats{N: 10, Mean: time.Millisecond},
		Query:        Stats{N: 10, P50: 50 * time.Microsecond},
		UpdatePeriod: 41666666 * time.Nanosecond, RateStabilityPPM: 121,
		PollInterval: 5 * time.Millisecond,
	}
	want.Estimate()
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine != want.Machine || got.UpdatePeriod != want.UpdatePeriod ||
		got.Achievable != want.Achievable || got.RateStabilityPPM != want.RateStabilityPPM {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestSanitiseKeepsPathsSafe(t *testing.T) {
	if got := sanitise("../../etc/passwd"); got != "------etc-passwd" {
		t.Errorf("sanitise gave %q", got)
	}
	if got := sanitise(""); got != "unknown" {
		t.Errorf("empty name gave %q, want unknown", got)
	}
}

func TestMeasureTimerProducesSamples(t *testing.T) {
	s := MeasureTimer(2*time.Millisecond, 60*time.Millisecond)
	if s.N < 10 {
		t.Errorf("only %d ticks in 60ms at 2ms, want at least 10", s.N)
	}
	if s.Max < 0 {
		t.Error("negative maximum lateness")
	}
}
