package source

import (
	"testing"
	"time"
)

func at(t *testing.T) (*Studio, func(time.Duration)) {
	t.Helper()
	now := time.Unix(0, 0)
	s := &Studio{Now: func() time.Time { return now }, Stale: time.Second}
	return s, func(d time.Duration) { now = now.Add(d) }
}

func TestNothingReportedIsNoPosition(t *testing.T) {
	s, _ := at(t)
	if _, ok, err := s.Position(); ok || err != nil {
		t.Errorf("claimed a position before anything reported: %v, %v", ok, err)
	}
	if !s.Silent() {
		t.Error("not silent before anything reported")
	}
}

func TestItInterpolatesBetweenReports(t *testing.T) {
	// The page reports at the film's rate, around 24 a second; the show loop
	// asks 200 times a second. Answering with a position that only moves when
	// a report arrives would quantise every cue to a frame boundary.
	s, tick := at(t)
	s.Report(10*time.Second, true, time.Second/24)
	tick(100 * time.Millisecond)

	got, ok, _ := s.Position()
	if !ok {
		t.Fatal("no position")
	}
	if want := 10*time.Second + 100*time.Millisecond; got != want {
		t.Errorf("position %v, want %v", got, want)
	}
}

func TestAPausedPlayheadStaysWhereItIs(t *testing.T) {
	s, tick := at(t)
	s.Report(10*time.Second, false, time.Second/24)
	tick(300 * time.Millisecond)
	if got, ok, _ := s.Position(); !ok || got != 10*time.Second {
		t.Errorf("a paused playhead moved to %v", got)
	}
}

func TestASeekLandsWhereItWasPut(t *testing.T) {
	s, tick := at(t)
	s.Report(10*time.Second, true, time.Second/24)
	tick(50 * time.Millisecond)
	s.Report(90*time.Second, true, time.Second/24)
	if got, _, _ := s.Position(); got != 90*time.Second {
		t.Errorf("after a seek, %v", got)
	}
}

func TestSilenceStopsItClaimingToKnow(t *testing.T) {
	/* A browser tab can be closed, put to sleep, or driven into a tunnel, and
	 * none of those look different from here. Carrying on interpolating would
	 * mean a fan driven by a page that no longer exists. */
	s, tick := at(t)
	s.Report(10*time.Second, true, time.Second/24)
	tick(999 * time.Millisecond)
	if _, ok, _ := s.Position(); !ok {
		t.Error("gave up inside the stale window")
	}
	if s.Silent() {
		t.Error("called it silent inside the window")
	}

	tick(2 * time.Millisecond)
	if _, ok, err := s.Position(); ok {
		t.Error("still claiming a position after the window")
	} else if err != nil {
		// Not an error: a player with nothing to say is an ordinary state and
		// the show loop already knows what to do with it.
		t.Errorf("reported silence as an error: %v", err)
	}
	if !s.Silent() {
		t.Error("not silent after the window")
	}
}

func TestReportingAgainBringsItBack(t *testing.T) {
	s, tick := at(t)
	s.Report(10*time.Second, true, time.Second/24)
	tick(2 * time.Second)
	s.Report(11*time.Second, true, time.Second/24)
	if got, ok, _ := s.Position(); !ok || got != 11*time.Second {
		t.Errorf("%v, %v", got, ok)
	}
}

func TestForgetting(t *testing.T) {
	s, _ := at(t)
	s.Report(10*time.Second, true, time.Second/24)
	s.Forget()
	if _, ok, _ := s.Position(); ok {
		t.Error("still had a position after being told to forget")
	}
}

func TestTheFrameRateComesFromTheFilm(t *testing.T) {
	s, _ := at(t)
	if _, ok := s.FrameInterval(); ok {
		t.Error("invented a frame rate before being told one")
	}
	s.Report(0, false, time.Second/24)
	got, ok := s.FrameInterval()
	if !ok || got != time.Second/24 {
		t.Errorf("%v, %v", got, ok)
	}
	// A report that does not carry one keeps the last, rather than dropping to
	// zero and making every discontinuity threshold meaningless.
	s.Report(time.Second, true, 0)
	if got, ok := s.FrameInterval(); !ok || got != time.Second/24 {
		t.Errorf("lost the frame rate: %v, %v", got, ok)
	}
}

func TestItRefusesToGoBackwardsOfZero(t *testing.T) {
	s, _ := at(t)
	s.Report(-5*time.Second, true, 0)
	if got, _, _ := s.Position(); got != 0 {
		t.Errorf("position %v", got)
	}
}

func TestItIsATimeSource(t *testing.T) {
	var _ TimeSource = (*Studio)(nil)
}
