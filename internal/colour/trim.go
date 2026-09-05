package colour

// A correction applied to a colour on its way to a fixture.
//
// Not a statement about a film: a statement about a strip. Two reels of LEDs
// with the same part number on the bag reach the same numbers differently, and
// an ambient wash behind a screen and an event strip in a cornice are
// different parts bought at different times. The number that makes one of them
// right makes the other one wrong, which is why this is per instrument and why
// it lives in the rig rather than in the score. A score edited to suit one
// strip plays wrong on every other rig.
//
// The case that asked for it: a generated ambient curve whose saturation runs
// between 0.03 and 0.31. The hue swings the whole way round the circle, the
// timeline draws it beautifully, and at five percent saturation the strip is
// white. Nothing is wrong with the score and nothing is wrong with the strip.
// What was missing was a knob.
//
// Added rather than multiplied, deliberately. Multiplying a saturation of 0.05
// by two gives 0.1, which is still white; adding 0.3 gives 0.35, which is a
// colour. The values that need help are the small ones, and multiplication is
// exactly the operation that will not help them.
type Trim struct {
	// Brightness and Saturation are added to a colour's intensity and
	// saturation, in -1 to +1. Zero changes nothing, which is the default and
	// the state every rig was in before this existed.
	Brightness float64
	Saturation float64
}

// Zero reports whether this trim would change anything.
func (t Trim) Zero() bool { return t.Brightness == 0 && t.Saturation == 0 }

// Apply adjusts one set of cue parameters.
//
// Anything that is not a colour comes back untouched. A fan takes an intensity
// and a fogger takes an output, and neither has any business being made more
// saturated: this can be asked of any instrument and answers for lights.
//
// The parameters are copied rather than adjusted in place, because the same
// map is held by the score and read by the timeline, and trimming in place
// would edit the film.
func (t Trim) Apply(p Params) Params {
	if t.Zero() || len(p) == 0 {
		return p
	}

	var c HSI
	switch {
	case IsHSI(p):
		c = HSI{H: p["h"], S: p["s"], I: p["i"]}
	case hasRGB(p):
		c = FromRGB(RGB{R: p["r"], G: p["g"], B: p["b"]})
	default:
		return p
	}

	c.S = clamp01(c.S + t.Saturation)
	c.I = clamp01(c.I + t.Brightness)
	rgb := ToRGB(c)

	out := make(Params, len(p)+4)
	for k, v := range p {
		out[k] = v
	}
	out["h"], out["s"], out["i"] = c.H, c.S, c.I
	out["r"], out["g"], out["b"] = rgb.R, rgb.G, rgb.B
	if _, ok := p["intensity"]; ok {
		out["intensity"] = c.I
	}
	return out
}

func hasRGB(p Params) bool {
	_, r := p["r"]
	_, g := p["g"]
	_, b := p["b"]
	return r || g || b
}

// Clamp holds a trim in the range a slider can express, so that a number
// arriving from somewhere else cannot mean more than the top of the range.
func Clamp(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}
