package clock

import (
	"testing"
	"time"
)

const (
	fps24 = time.Second / 24
	poll  = 5 * time.Millisecond
)

var origin = time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

// quantise floors d to a frame boundary, reproducing what a player reports:
// the timestamp of the frame currently on screen, not the true instant.
func quantise(d, frame time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return (d / frame) * frame
}

func cfg() Config {
	return Config{FrameInterval: fps24, PollInterval: poll}
}

// drive feeds samples from start to start+dur, calling check after each one
// with the wall time and the true (unquantised) media position.
func drive(c *Clock, start time.Time, dur time.Duration, rate float64,
	mediaAt func(wall time.Time) (time.Duration, bool),
	check func(wall time.Time),
) {
	for t := start; !t.After(start.Add(dur)); t = t.Add(poll) {
		media, ok := mediaAt(t)
		c.Sample(t, quantise(media, fps24), ok)
		if check != nil {
			check(t)
		}
	}
}

func playing(start time.Time, rate float64) func(time.Time) (time.Duration, bool) {
	return func(t time.Time) (time.Duration, bool) {
		return time.Duration(float64(t.Sub(start)) * rate), true
	}
}

func TestAnchorsBeatNaiveSampling(t *testing.T) {
	c := New(cfg())
	src := playing(origin, 1.0)

	var worst time.Duration
	drive(c, origin, 5*time.Second, 1.0, src, func(wall time.Time) {
		if wall.Sub(origin) < time.Second {
			return // warm up
		}
		truth, _ := src(wall)
		got := c.At(wall)
		if !got.Anchored {
			t.Fatalf("not anchored after %s", wall.Sub(origin))
		}
		err := got.Media - truth
		if err < 0 {
			err = -err
		}
		if err > worst {
			worst = err
		}
	})

	// A single naive sample is wrong by up to one frame, 41.7ms. Anchoring
	// should keep us inside roughly one polling interval.
	if worst > 2*poll {
		t.Errorf("worst error %v, want <= %v (one frame is %v)", worst, 2*poll, fps24)
	}
	t.Logf("worst error %v against a frame interval of %v", worst, fps24)
}

func TestPrecisionIsNeverOptimistic(t *testing.T) {
	c := New(cfg())
	src := playing(origin, 1.0)

	drive(c, origin, 5*time.Second, 1.0, src, func(wall time.Time) {
		truth, _ := src(wall)
		got := c.At(wall)
		err := got.Media - truth
		if err < 0 {
			err = -err
		}
		if err > got.Precision {
			t.Fatalf("at %s: error %v exceeded reported precision %v",
				wall.Sub(origin), err, got.Precision)
		}
	})
}

func TestRateEstimateConverges(t *testing.T) {
	const rate = 1.0002 // 200ppm, within the range measured on real players
	c := New(cfg())
	drive(c, origin, 10*time.Second, rate, playing(origin, rate), nil)

	got, ok := c.Rate()
	if !ok {
		t.Fatal("rate never became valid")
	}
	if diff := got - rate; diff > 0.0005 || diff < -0.0005 {
		t.Errorf("rate %.6f, want %.6f within 500ppm", got, rate)
	}
}

func TestPauseIsDetectedAndHoldsPosition(t *testing.T) {
	c := New(cfg())
	pauseAt := origin.Add(2 * time.Second)
	frozen := time.Duration(-1)

	src := func(t2 time.Time) (time.Duration, bool) {
		if t2.Before(pauseAt) {
			return t2.Sub(origin), true
		}
		if frozen < 0 {
			frozen = quantise(pauseAt.Sub(origin), fps24)
		}
		return frozen, true
	}
	drive(c, origin, 4*time.Second, 1.0, src, nil)

	end := origin.Add(4 * time.Second)
	got := c.At(end)
	if got.State != StatePaused {
		t.Fatalf("state %v, want paused", got.State)
	}
	if got.Rate != 0 {
		t.Errorf("rate %v while paused, want 0", got.Rate)
	}
	// A clock that missed the pause would be ~2s wrong here, which is the
	// failure the measurement in feat-timing-core called out.
	if d := got.Media - frozen; d > fps24 || d < -fps24 {
		t.Errorf("media %v, want %v within a frame", got.Media, frozen)
	}
}

func TestPauseThenResume(t *testing.T) {
	c := New(cfg())
	pauseAt := origin.Add(time.Second)
	resumeAt := origin.Add(3 * time.Second)
	held := quantise(time.Second, fps24)

	src := func(t2 time.Time) (time.Duration, bool) {
		switch {
		case t2.Before(pauseAt):
			return t2.Sub(origin), true
		case t2.Before(resumeAt):
			return held, true
		default:
			return held + t2.Sub(resumeAt), true
		}
	}
	drive(c, origin, 5*time.Second, 1.0, src, nil)

	end := origin.Add(5 * time.Second)
	got := c.At(end)
	if got.State != StatePlaying {
		t.Fatalf("state %v after resume, want playing", got.State)
	}
	want := held + end.Sub(resumeAt)
	if d := got.Media - want; d > 3*poll || d < -3*poll {
		t.Errorf("media %v, want %v within %v", got.Media, want, 3*poll)
	}
}

func TestSeekIsDetectedAndRecovers(t *testing.T) {
	c := New(cfg())
	seekAt := origin.Add(2 * time.Second)
	const jump = 30 * time.Second

	src := func(t2 time.Time) (time.Duration, bool) {
		d := t2.Sub(origin)
		if t2.Before(seekAt) {
			return d, true
		}
		return d + jump, true
	}
	drive(c, origin, 5*time.Second, 1.0, src, nil)

	if c.Discontinuities != 1 {
		t.Errorf("discontinuities %d, want 1", c.Discontinuities)
	}
	end := origin.Add(5 * time.Second)
	truth, _ := src(end)
	got := c.At(end)
	if d := got.Media - truth; d > 3*poll || d < -3*poll {
		t.Errorf("media %v, want %v within %v after seek", got.Media, truth, 3*poll)
	}
}

func TestNormalFrameStepIsNotASeek(t *testing.T) {
	// The spike used a fixed 25ms threshold and flagged every 41.7ms frame
	// advance. Nothing here may repeat that.
	c := New(cfg())
	drive(c, origin, 5*time.Second, 1.0, playing(origin, 1.0), nil)
	if c.Discontinuities != 0 {
		t.Errorf("discontinuities %d during steady playback, want 0", c.Discontinuities)
	}
}

func TestUnavailablePositionResets(t *testing.T) {
	c := New(cfg())
	drive(c, origin, 2*time.Second, 1.0, playing(origin, 1.0), nil)
	if c.Anchors() == 0 {
		t.Fatal("expected anchors before the gap")
	}
	c.Sample(origin.Add(2*time.Second+poll), 0, false)

	got := c.At(origin.Add(2 * time.Second))
	if got.State != StateUnknown {
		t.Errorf("state %v, want unknown", got.State)
	}
	if got.Anchored {
		t.Error("still anchored after the position became unavailable")
	}
}
