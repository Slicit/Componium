package score

import (
	"time"

	"github.com/Slicit/componium/internal/colour"
)

// ValueAt returns the interpolated value of a curve track at a media time.
//
// Before the first point and after the last one the curve holds, rather than
// extrapolating. Extrapolating a wind speed past the end of its authored range
// would invent an effect the author never wrote.
func (t Track) ValueAt(at time.Duration) map[string]float64 {
	if len(t.Points) == 0 {
		return nil
	}
	tc := Timecode(at)
	if tc <= t.Points[0].T {
		return resolve(t, copyValues(t.Points[0].Value))
	}
	last := t.Points[len(t.Points)-1]
	if tc >= last.T {
		return resolve(t, copyValues(last.Value))
	}

	// The points are sorted, so find the segment containing at.
	hi := 0
	for i, p := range t.Points {
		if p.T > tc {
			hi = i
			break
		}
	}
	lo := hi - 1
	a, b := t.Points[lo], t.Points[hi]

	if t.Interpolation == Step {
		return resolve(t, copyValues(a.Value))
	}

	span := float64(b.T - a.T)
	if span <= 0 {
		return resolve(t, copyValues(b.Value))
	}
	f := float64(tc-a.T) / span

	// Hue is not a number you can average.
	//
	// It wraps, so 0.97 to 0.03 is a sixth of a turn through red and not five
	// sixths backwards through green; and it does not exist at all when there
	// is no saturation to have it, so fading white to red must grow into red
	// rather than sweep through whatever number sits beside that white. Both
	// of those are wrong under the channel-by-channel interpolation below, and
	// both of them are wrong at exactly the moments a lighting score cares
	// about.
	if t.Space == HSI {
		return resolve(t, lerpHSI(a.Value, b.Value, f))
	}

	out := make(map[string]float64, len(a.Value)+len(b.Value))
	for k, av := range a.Value {
		if bv, ok := b.Value[k]; ok {
			out[k] = av + (bv-av)*f
		} else {
			// A channel that stops being mentioned holds its last value
			// rather than snapping to zero.
			out[k] = av
		}
	}
	for k, bv := range b.Value {
		if _, ok := a.Value[k]; !ok {
			out[k] = bv
		}
	}
	return resolve(t, out)
}

func copyValues(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// lerpHSI interpolates a colour the way a colour has to be interpolated.
func lerpHSI(a, b map[string]float64, f float64) map[string]float64 {
	c := colour.Lerp(
		colour.HSI{H: a["h"], S: a["s"], I: a["i"]},
		colour.HSI{H: b["h"], S: b["s"], I: b["i"]},
		f,
	)
	out := map[string]float64{"h": c.H, "s": c.S, "i": c.I}
	// Anything else on the track — a white channel, say — is an ordinary
	// number and interpolates like one.
	for k, av := range a {
		if k == "h" || k == "s" || k == "i" {
			continue
		}
		if bv, ok := b[k]; ok {
			out[k] = av + (bv-av)*f
		} else {
			out[k] = av
		}
	}
	return out
}

// resolve adds the red, green and blue a fixture needs, for a track authored
// as hue and saturation. An RGB track is returned untouched.
//
// A track that says nothing about its space is examined rather than assumed.
// The composer wrote flash cues in hue, saturation and intensity and declared
// no space for years, so every flash reached its fixture carrying three
// parameters no driver reads and none of the three it does: the light stayed
// dark and everything reported success.
//
// Safe because colour.Resolve leaves parameters that are not HSI exactly as
// they are, so the only tracks this touches are the ones that were already
// unreadable. A track that declares rgb is still taken at its word: an explicit
// declaration is a statement about intent, and this repairs its absence rather
// than overriding it.
func resolve(t Track, v map[string]float64) map[string]float64 {
	if t.Space == HSI {
		return colour.Resolve(v)
	}
	if t.Space == "" && colour.IsHSI(v) {
		return colour.Resolve(v)
	}
	return v
}
