// Package colour converts between how a light is authored and how a fixture is
// driven.
//
// A score describes a light as hue, saturation and intensity. A fixture takes
// red, green and blue. Those are the same information and the choice between
// them is not about colour theory, it is about which one a person can edit and
// which one the rest of this system already understands.
//
// Intensity, specifically, rather than HSL's lightness. Every other instrument
// in Componium has an intensity — wind, shake, mist — and the duty cycle, the
// maximum continuous run and the composer's rest budget all reason about it.
// A light stored as RGB is the one instrument whose "how hard" has to be
// guessed at, by taking the largest channel. Storing intensity makes a light
// ordinary. HSL's lightness would not: L=0.5 yellow and L=0.5 blue are wildly
// different to look at, so it answers a question nobody asked.
//
// The geometry here is HSV's, with V named intensity. At s=0 the result is
// neutral at the intensity given; at s=1 it is a fully saturated hue at that
// intensity. That is how a fixture behaves when you turn its dimmer down.
package colour

import "math"

// Hue is in turns, not degrees: 0 is red, 1/3 green, 2/3 blue, and 1 is red
// again.
//
// Turns because every other parameter in a score is a fraction between zero
// and one, and the editor draws every channel on a lane of that height. Making
// hue the one parameter measured in degrees would need a special case in the
// format, in the editor and in every instrument, to save a reader from
// dividing by three hundred and sixty.
type HSI struct {
	H, S, I float64
}

type RGB struct {
	R, G, B float64
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// wrap brings a hue into [0,1). Editing pushes hue past both ends constantly —
// dragging a lane up from red goes to 1.02, not to magenta — and a hue outside
// the range would otherwise produce a black or a nonsense colour.
func wrap(h float64) float64 {
	if math.IsNaN(h) || math.IsInf(h, 0) {
		return 0
	}
	h = math.Mod(h, 1)
	if h < 0 {
		h++
	}
	// Hue 1 and hue 0 are the same colour, and arithmetic across the seam
	// lands on either. Interpolating from 0.03 to 0.97 arrives at -3.4e-18,
	// which wraps to 0.9999999999999999: red, correctly, but displayed at the
	// far end of a lane from where the drag started. Canonicalising to zero
	// keeps the number agreeing with the colour.
	if h > 1-1e-9 {
		return 0
	}
	return h
}

// ToRGB converts a light's authored colour to what a fixture is sent.
func ToRGB(c HSI) RGB {
	h := wrap(c.H)
	s := clamp01(c.S)
	i := clamp01(c.I)

	if s == 0 {
		return RGB{i, i, i}
	}

	sector := h * 6
	k := int(sector) % 6
	f := sector - math.Floor(sector)
	p := i * (1 - s)
	q := i * (1 - s*f)
	t := i * (1 - s*(1-f))

	switch k {
	case 0:
		return RGB{i, t, p}
	case 1:
		return RGB{q, i, p}
	case 2:
		return RGB{p, i, t}
	case 3:
		return RGB{p, q, i}
	case 4:
		return RGB{t, p, i}
	default:
		return RGB{i, p, q}
	}
}

// FromRGB is the inverse, for reading an existing score or an analysed frame.
//
// Lossy in one specific way worth knowing about: a neutral colour has no hue,
// so converting grey and back produces grey but the hue in between is an
// invention. Callers converting a whole score should carry a neighbouring
// hue rather than accept the zero this returns — see HoldHue.
func FromRGB(c RGB) HSI {
	r, g, b := clamp01(c.R), clamp01(c.G), clamp01(c.B)
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	d := max - min

	out := HSI{I: max}
	if max == 0 {
		return out
	}
	out.S = d / max
	if d == 0 {
		return out
	}

	switch max {
	case r:
		out.H = math.Mod((g-b)/d, 6) / 6
	case g:
		out.H = ((b-r)/d + 2) / 6
	default:
		out.H = ((r-g)/d + 4) / 6
	}
	out.H = wrap(out.H)
	return out
}

// Lerp interpolates between two authored colours.
//
// This is the whole reason hue cannot be an ordinary channel that the curve
// evaluator interpolates like any other number.
//
// Hue wraps, so the arithmetic has to take the short way round: from 0.97 to
// 0.03 is a sixth of a turn through red, not five sixths backwards through
// green. Linear interpolation gets that exactly wrong at the seam, and the
// seam is red, which is not an obscure corner of a lighting score.
//
// And hue is undefined when there is no saturation to have a hue: white has no
// colour, so fading white to red must not sweep through whatever number
// happened to be stored beside that white. An unsaturated end takes its
// partner's hue and simply grows into it.
func Lerp(a, b HSI, f float64) HSI {
	const neutral = 1e-4

	ah, bh := wrap(a.H), wrap(b.H)
	switch {
	case a.S <= neutral && b.S <= neutral:
		ah, bh = 0, 0
	case a.S <= neutral:
		ah = bh
	case b.S <= neutral:
		bh = ah
	}

	d := bh - ah
	if d > 0.5 {
		d -= 1
	} else if d < -0.5 {
		d += 1
	}

	return HSI{
		H: wrap(ah + d*f),
		S: a.S + (b.S-a.S)*f,
		I: a.I + (b.I-a.I)*f,
	}
}

// Params is the loose map a score and an instrument speak in.
type Params = map[string]float64

// IsHSI reports whether a parameter set describes a colour the authored way.
func IsHSI(p Params) bool {
	_, h := p["h"]
	_, s := p["s"]
	_, i := p["i"]
	return h || s || i
}

// Resolve adds the red, green and blue an instrument needs, leaving the
// authored values in place.
//
// Both, rather than a replacement, because different fixtures want different
// things: an RGB head takes the three channels, a plain dimmer takes only a
// level, and a preview wants the hue to show what was meant. Sending all of it
// costs a few bytes and means no instrument has to know about colour spaces.
//
// A parameter set that is already RGB is returned untouched, so existing
// scores keep working exactly as they did.
func Resolve(p Params) Params {
	if p == nil || !IsHSI(p) {
		return p
	}
	c := ToRGB(HSI{H: p["h"], S: p["s"], I: p["i"]})

	out := make(Params, len(p)+4)
	for k, v := range p {
		out[k] = v
	}
	out["r"] = c.R
	out["g"] = c.G
	out["b"] = c.B
	// A dimmer-only fixture reads this, and so does everything that asks how
	// hard an effect is running.
	if _, ok := out["intensity"]; !ok {
		out["intensity"] = clamp01(p["i"])
	}
	return out
}
