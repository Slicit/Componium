package conductor

import (
	"time"

	"github.com/Slicit/componium/internal/clock"
	"github.com/Slicit/componium/internal/instrument"
)

// CurveTrack is a continuous channel to be sent to an instrument repeatedly.
//
// ValueAt is a function rather than a score type so that this package does not
// depend on the score format. A curve is just something that has a value at a
// time; where that value came from is not the conductor's business.
type CurveTrack struct {
	Instrument string
	ValueAt    func(media time.Duration) map[string]float64
}

// CurveDriver sends curve tracks to their instruments at a fixed rate.
//
// Curves are the other half of a score and behave nothing like cues. A cue
// fires once and latency shifts it earlier; a curve is a value that must keep
// being sent, and sending it early would simply mean sending the wrong value.
// So the driver compensates latency by sampling the curve *ahead* of the
// current media time rather than by dispatching sooner.
type CurveDriver struct {
	interval    time.Duration
	tracks      []CurveTrack
	instruments map[string]instrument.Instrument
	last        time.Time
	sent        int
}

// NewCurveDriver returns a driver that sends no faster than interval.
//
// Fifty hertz is a reasonable default: fast enough that a linear ramp looks
// smooth on a light or a fan, slow enough to leave the network alone.
func NewCurveDriver(interval time.Duration) *CurveDriver {
	if interval <= 0 {
		interval = 20 * time.Millisecond
	}
	return &CurveDriver{interval: interval, instruments: map[string]instrument.Instrument{}}
}

// Register makes an instrument available to curve tracks addressed to it.
func (d *CurveDriver) Register(i instrument.Instrument) {
	d.instruments[i.Manifest().ID] = i
}

// Add attaches a curve track.
func (d *CurveDriver) Add(t CurveTrack) { d.tracks = append(d.tracks, t) }

// Tick sends every curve track, if enough time has passed and playback is
// actually running.
func (d *CurveDriver) Tick(wall time.Time, r clock.Reading) {
	if r.State != clock.StatePlaying {
		return
	}
	if !d.last.IsZero() && wall.Sub(d.last) < d.interval {
		return
	}
	d.last = wall

	for _, t := range d.tracks {
		inst, ok := d.instruments[t.Instrument]
		if !ok {
			continue
		}
		// Sample ahead by the instrument's latency, so that what the device
		// eventually produces matches what the score asked for at that
		// moment.
		at := r.Media + inst.Manifest().Latency
		v := t.ValueAt(at)
		if v == nil {
			continue
		}
		_ = inst.Dispatch(instrument.Dispatch{
			Cue: instrument.Cue{
				Instrument: t.Instrument,
				Action:     "set",
				Params:     v,
				At:         at,
			},
			Media:     r.Media,
			Wall:      wall,
			Precision: r.Precision,
		})
		d.sent++
	}
}

// Sent reports how many curve updates have been dispatched.
func (d *CurveDriver) Sent() int { return d.sent }
