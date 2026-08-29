package score

import "time"

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
		return copyValues(t.Points[0].Value)
	}
	last := t.Points[len(t.Points)-1]
	if tc >= last.T {
		return copyValues(last.Value)
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
		return copyValues(a.Value)
	}

	span := float64(b.T - a.T)
	if span <= 0 {
		return copyValues(b.Value)
	}
	f := float64(tc-a.T) / span

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
	return out
}

func copyValues(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
