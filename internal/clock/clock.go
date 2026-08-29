// Package clock turns a media player's coarsely reported playback position
// into a usable media clock.
//
// The design follows directly from measurement (see
// LOGBOOK/features/feat-timing-core.md). A player does not report a noisy
// position, it reports an exact one that is stale: mpv returns the
// presentation timestamp of the frame currently on screen, so the value is a
// staircase that steps once per frame. Averaging would be the wrong tool,
// because there is no noise to average away.
//
// Instead the clock watches for the step edges. When the reported position
// changes, that transition happened somewhere between the previous poll and
// this one, which makes it an anchor whose media time is exact and whose wall
// time is known to within one polling interval. Measured, that is about 9 ms
// at 200 Hz including scheduler lateness, against 41.7 ms for a single naive
// sample at 24 fps.
//
// The clock is deliberately passive. It does no polling and calls no timer of
// its own: the caller feeds it samples and asks it questions, both with an
// explicit wall time. That makes every behaviour here testable without a
// player, a socket, or a sleep.
package clock

import (
	"math"
	"time"
)

// State is what the clock believes the player is doing.
type State int

const (
	// StateUnknown means the position is unavailable, which players report
	// while idle, loading, or between files.
	StateUnknown State = iota
	// StatePlaying means media time is advancing.
	StatePlaying
	// StatePaused means media time has stopped while wall time has not.
	StatePaused
)

func (s State) String() string {
	switch s {
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}

// Reading is the clock's answer at a given wall time.
type Reading struct {
	// Media is the estimated position in the media.
	Media time.Duration
	// Rate is media seconds per wall second. 1.0 is realtime playback.
	Rate float64
	// Precision is a bound on how wrong Media may be. It grows between
	// anchors and collapses at each one. The conductor refuses cues whose
	// required precision it cannot meet, so this value is load bearing and
	// must never be optimistic.
	Precision time.Duration
	// State is what the player appears to be doing.
	State State
	// Anchored reports whether an edge has been observed yet. Before the
	// first anchor, Media is the last raw reading and Precision is one whole
	// frame interval.
	Anchored bool
}

// Config describes the content and the polling regime. Neither is measured by
// the clock: the frame interval comes from the media, and the poll interval is
// whatever the caller has chosen.
type Config struct {
	// FrameInterval is the content's frame period, for example 41667us at
	// 24fps. Discontinuity thresholds are expressed in multiples of it,
	// because at 24fps a single normal frame advance exceeds any fixed
	// millisecond threshold tight enough to catch a small seek.
	FrameInterval time.Duration
	// PollInterval is how often the caller expects to call Sample. It is used
	// only as a floor on reported precision.
	PollInterval time.Duration
	// MaxAnchors caps the history kept for rate estimation. Zero means 512.
	//
	// The count matters because it sets the baseline of the rate fit: at
	// 24fps, 512 anchors span about 21 seconds. A short baseline makes the
	// estimate jitter by thousands of ppm, when real pacing error is tens.
	MaxAnchors int
	// StallFrames is how many frame intervals of an unchanging position mean
	// the player is paused rather than merely between frames. Zero means 3.
	StallFrames int
	// SeekFrames is how far media advance may differ from wall advance,
	// in frame intervals, before it is treated as a discontinuity rather
	// than normal progress. Zero means 4.
	SeekFrames int
}

func (c Config) withDefaults() Config {
	if c.MaxAnchors <= 0 {
		c.MaxAnchors = 512
	}
	if c.StallFrames <= 0 {
		c.StallFrames = 3
	}
	if c.SeekFrames <= 0 {
		c.SeekFrames = 4
	}
	if c.FrameInterval <= 0 {
		// 24fps, the most common cinema frame rate, as a last resort.
		c.FrameInterval = time.Second * 1 / 24
	}
	return c
}

// anchor is an observed step edge: at some instant in (wall-window, wall] the
// player began displaying the frame whose timestamp is media.
type anchor struct {
	media  time.Duration
	wall   time.Time
	window time.Duration
}

// Clock estimates media time from a stream of position samples.
//
// It is not safe for concurrent use. The intended shape is one goroutine that
// polls and calls Sample, with readings handed onward, rather than shared
// locking on a hot path.
type Clock struct {
	cfg Config

	anchors []anchor

	lastPos        time.Duration
	lastPosValid   bool
	lastSampleWall time.Time
	lastChangeWall time.Time

	state State

	rate      float64
	rateValid bool
	residual  time.Duration

	// Discontinuities is the number of seeks or other jumps observed. Useful
	// in tests and in componium doctor.
	Discontinuities int
}

// New returns a clock that has not yet seen a sample.
func New(cfg Config) *Clock {
	return &Clock{cfg: cfg.withDefaults(), rate: 1.0}
}

// Sample feeds one observation to the clock. wall is when the observation was
// taken, pos is the position the player reported, and ok is false when the
// player had no position to give.
//
// Sample is where every state transition happens. At only reads.
func (c *Clock) Sample(wall time.Time, pos time.Duration, ok bool) {
	if !ok {
		// The player has no position: idle, loading, or between files.
		// Anchors from a previous file are meaningless now.
		c.reset()
		c.state = StateUnknown
		c.lastSampleWall = wall
		return
	}

	if !c.lastPosValid {
		// First reading. We know where the player is but not when this frame
		// began, so this is not yet an anchor.
		c.lastPos = pos
		c.lastPosValid = true
		c.lastChangeWall = wall
		c.lastSampleWall = wall
		c.state = StatePlaying
		return
	}

	if pos == c.lastPos {
		// Between frames, or paused. Only elapsed time tells them apart.
		stall := time.Duration(c.cfg.StallFrames) * c.cfg.FrameInterval
		if wall.Sub(c.lastChangeWall) > stall {
			c.state = StatePaused
		}
		c.lastSampleWall = wall
		return
	}

	// The position changed. Either the next frame, or a discontinuity.
	delta := pos - c.lastPos
	elapsed := wall.Sub(c.lastSampleWall)
	expected := time.Duration(float64(elapsed) * c.currentRate())
	tolerance := time.Duration(c.cfg.SeekFrames) * c.cfg.FrameInterval

	if delta < -tolerance || delta-expected > tolerance {
		// A seek, a loop back to the start, or a new file. Everything we
		// knew about where playback was is now wrong. Keep the rate estimate,
		// which is a property of the machine and the player rather than of
		// the position, but drop the anchors.
		c.Discontinuities++
		c.anchors = c.anchors[:0]
		c.lastPos = pos
		c.lastChangeWall = wall
		c.lastSampleWall = wall
		c.state = StatePlaying
		return
	}

	// A normal frame edge. The transition happened somewhere in
	// (lastSampleWall, wall], so the media time is exact and the wall time is
	// known to within that window.
	c.appendAnchor(anchor{
		media:  pos,
		wall:   wall,
		window: elapsed,
	})
	c.lastPos = pos
	c.lastChangeWall = wall
	c.lastSampleWall = wall
	c.state = StatePlaying
	c.estimateRate()
}

func (c *Clock) reset() {
	c.anchors = c.anchors[:0]
	c.lastPosValid = false
	c.lastPos = 0
	c.rateValid = false
	c.residual = 0
}

func (c *Clock) appendAnchor(a anchor) {
	c.anchors = append(c.anchors, a)
	if len(c.anchors) > c.cfg.MaxAnchors {
		copy(c.anchors, c.anchors[len(c.anchors)-c.cfg.MaxAnchors:])
		c.anchors = c.anchors[:c.cfg.MaxAnchors]
	}
}

func (c *Clock) currentRate() float64 {
	if c.rateValid {
		return c.rate
	}
	return 1.0
}

// estimateRate fits media time against wall time across the anchor history and
// records the residual spread, which feeds the precision estimate.
//
// Playback pacing was measured at 9 to 282 parts per million, so the rate term
// matters little over a second and a great deal over a three hour film.
func (c *Clock) estimateRate() {
	n := len(c.anchors)
	if n < 4 {
		return
	}
	base := c.anchors[0].wall
	var sx, sy, sxx, sxy float64
	for _, a := range c.anchors {
		x := a.wall.Sub(base).Seconds()
		y := a.media.Seconds()
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	fn := float64(n)
	denom := fn*sxx - sx*sx
	if denom <= 0 {
		return
	}
	slope := (fn*sxy - sx*sy) / denom
	intercept := (sy*sxx - sx*sxy) / denom

	// A rate far from realtime is more likely a bad fit than a player running
	// at triple speed, and trusting it would be worse than assuming 1.0.
	if slope < 0.5 || slope > 2.0 {
		return
	}

	var sumSq float64
	for _, a := range c.anchors {
		x := a.wall.Sub(base).Seconds()
		e := a.media.Seconds() - (slope*x + intercept)
		sumSq += e * e
	}

	c.rate = slope
	c.rateValid = true
	c.residual = time.Duration(math.Sqrt(sumSq/fn) * float64(time.Second))
}

// At estimates the media position at the given wall time.
//
// Precision is never optimistic. Before the first anchor it is a whole frame
// interval, because the reported position may be that stale. After an anchor
// it is the anchor's own window plus the spread of the rate fit plus whatever
// the rate uncertainty accumulates over the elapsed time.
func (c *Clock) At(wall time.Time) Reading {
	r := Reading{
		Rate:     c.currentRate(),
		State:    c.state,
		Anchored: len(c.anchors) > 0,
	}

	switch {
	case !c.lastPosValid:
		r.Precision = c.cfg.FrameInterval
		return r

	case c.state == StatePaused:
		// Paused position is exact to within the frame on screen, and does
		// not decay with time, because nothing is moving.
		r.Media = c.lastPos
		r.Rate = 0
		r.Precision = c.cfg.FrameInterval
		return r

	case len(c.anchors) == 0:
		// Seen a position but no edge yet, so the reading may be a whole
		// frame stale and we cannot say when it began.
		r.Media = c.lastPos
		r.Precision = c.cfg.FrameInterval
		return r
	}

	a := c.anchors[len(c.anchors)-1]
	elapsed := wall.Sub(a.wall)
	r.Media = a.media + time.Duration(float64(elapsed)*r.Rate)

	// rateErr is how wrong the rate estimate might be. Before the fit has
	// settled, assume the worst pacing observed in measurement, well over the
	// 282ppm actually seen.
	rateErr := 0.001
	if c.rateValid {
		rateErr = 0.0005
	}
	drift := time.Duration(math.Abs(float64(elapsed)) * rateErr)

	r.Precision = a.window + c.residual + drift
	if r.Precision < c.cfg.PollInterval {
		r.Precision = c.cfg.PollInterval
	}
	return r
}

// Rate reports the current estimate of media seconds per wall second, and
// whether enough anchors have been seen to trust it.
func (c *Clock) Rate() (float64, bool) { return c.currentRate(), c.rateValid }

// Anchors reports how many step edges are currently held.
func (c *Clock) Anchors() int { return len(c.anchors) }
