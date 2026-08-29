package tune

import (
	"time"

	"github.com/Slicit/componium/internal/source"
)

// MeasureTimer records how late a ticker actually fires.
//
// Unlike the rest of Componium this genuinely has to wait for real time to
// pass: it is measuring the passage of time itself, and there is nothing to
// inject.
func MeasureTimer(interval, duration time.Duration) Stats {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	deadline := start.Add(duration)
	var late []time.Duration
	n := 0

	for now := range ticker.C {
		if now.After(deadline) {
			break
		}
		n++
		// Intended wake measured from the start rather than from the previous
		// tick, so that error does not accumulate into the result.
		want := start.Add(time.Duration(n) * interval)
		late = append(late, time.Since(want))
	}
	return Summarise(late)
}

// SourceReport is what measuring a player yields.
type SourceReport struct {
	Query        Stats
	UpdatePeriod time.Duration
	RatePPM      float64
	Samples      int
	Unavailable  int
}

// MeasureSource characterises a player: how expensive it is to ask, how finely
// it reports position, and how accurately it paces playback.
//
// The player must actually be playing something. A paused or idle player
// yields an update period of zero and a meaningless rate.
func MeasureSource(src source.TimeSource, interval, duration time.Duration) SourceReport {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	deadline := start.Add(duration)

	var rtts []time.Duration
	var rep SourceReport
	var firstPos, lastPos time.Duration
	var firstWall, lastWall time.Time
	var prevPos time.Duration
	minStep := time.Duration(0)
	havePrev := false

	for now := range ticker.C {
		if now.After(deadline) {
			break
		}
		t0 := time.Now()
		pos, ok, err := src.Position()
		rtt := time.Since(t0)
		if err != nil {
			continue
		}
		if !ok {
			rep.Unavailable++
			continue
		}
		rtts = append(rtts, rtt)
		rep.Samples++

		if firstWall.IsZero() {
			firstPos, firstWall = pos, t0
		}
		lastPos, lastWall = pos, t0

		if havePrev && pos != prevPos {
			step := pos - prevPos
			if step < 0 {
				step = -step
			}
			if minStep == 0 || step < minStep {
				minStep = step
			}
		}
		prevPos = pos
		havePrev = true
	}

	rep.Query = Summarise(rtts)
	rep.UpdatePeriod = minStep
	if wall := lastWall.Sub(firstWall); wall > time.Second {
		rate := float64(lastPos-firstPos) / float64(wall)
		rep.RatePPM = (rate - 1) * 1e6
	}
	return rep
}
