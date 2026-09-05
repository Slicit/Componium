package studio

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/Slicit/componium/internal/colour"
	"github.com/Slicit/componium/internal/instrument"
)

// A live adjustment to what colour instruments are sent.
//
// Applied on the way out and never written to the score, because it is not a
// statement about the film. It is a statement about a strip: two reels of LEDs
// with the same part number on the bag reach the same numbers differently, and
// the fix belongs where the difference is rather than in a score that has to
// play on somebody else's rig.
//
// The case that asked for it: a generated ambient curve whose saturation runs
// between 0.03 and 0.31. The hue swings the whole way round the circle and the
// timeline draws it beautifully, and at five percent saturation the strip is
// white. Nothing is wrong with the score and nothing is wrong with the strip.
// What is missing is a knob.
//
// Added rather than multiplied, deliberately. Multiplying a saturation of 0.05
// by two gives 0.1, which is still white; adding 0.3 gives 0.35, which is a
// colour. The values that need help are the small ones, and multiplication is
// exactly the operation that will not help them.
type Trim struct {
	// Brightness and Saturation are added to a colour's intensity and
	// saturation, in -1 to +1. Zero changes nothing, which is the default and
	// the state everything was in before this existed.
	Brightness float64
	Saturation float64
}

// Zero reports whether this trim would change anything.
func (t Trim) Zero() bool { return t.Brightness == 0 && t.Saturation == 0 }

// Apply adjusts one set of cue parameters.
//
// Anything that is not a colour comes back untouched. A fan takes an
// intensity, a fogger takes an output, and neither has any business being made
// more saturated: this is asked of every instrument and answers for lights.
//
// A stop carries no parameters at all, so it passes through as itself. That
// matters more than it looks: a blackout must stay a blackout however the
// sliders are set, and a trim that could brighten a stop would be a light that
// cannot be turned off.
func (t Trim) Apply(p map[string]float64) map[string]float64 {
	if t.Zero() || len(p) == 0 {
		return p
	}

	var c colour.HSI
	switch {
	case colour.IsHSI(p):
		c = colour.HSI{H: p["h"], S: p["s"], I: p["i"]}
	case hasRGB(p):
		c = colour.FromRGB(colour.RGB{R: p["r"], G: p["g"], B: p["b"]})
	default:
		return p
	}

	c.S = clamp01(c.S + t.Saturation)
	c.I = clamp01(c.I + t.Brightness)
	rgb := colour.ToRGB(c)

	// A copy, because the score's own values are shared with the timeline and
	// with whatever else is reading this cue. Trimming in place would edit the
	// film.
	out := make(map[string]float64, len(p)+4)
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

func hasRGB(p map[string]float64) bool {
	_, r := p["r"]
	_, g := p["g"]
	_, b := p["b"]
	return r || g || b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// trimmed wraps an instrument so that what reaches it is adjusted.
//
// Outside the safety guard rather than inside it. The supervisor's own idea of
// safe is exact and must not be brightened by a slider somebody left at plus
// eighty, so anything it sends goes out untrimmed.
type trimmed struct {
	inner instrument.Instrument
	of    func() Trim
}

func (t trimmed) Manifest() instrument.Manifest { return t.inner.Manifest() }

func (t trimmed) Dispatch(d instrument.Dispatch) error {
	if instrument.IsStop(d.Cue.Action) {
		return t.inner.Dispatch(d)
	}
	d.Cue.Params = t.of().Apply(d.Cue.Params)
	return t.inner.Dispatch(d)
}

var _ instrument.Instrument = trimmed{}

// trimHolder is the studio's current trim, read at dispatch time.
//
// Read per cue rather than captured when the rig is armed, which is the whole
// point: the sliders are for moving while a film plays and watching the strip.
type trimHolder struct {
	mu sync.Mutex
	t  Trim
}

func (h *trimHolder) get() Trim {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.t
}

func (h *trimHolder) set(t Trim) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.t = t
}

// handleLiveTrim reads and sets the sliders.
//
// On the studio rather than on the live session, so that disarming to move a
// board and arming again does not silently throw away a setting somebody spent
// ten minutes finding.
func (s *Server) handleLiveTrim(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, wireTrim(s.trim.get()))

	case http.MethodPost, http.MethodPut:
		var in struct {
			Brightness *float64 `json:"brightness"`
			Saturation *float64 `json:"saturation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Absent means leave it, rather than means zero. A page moving one
		// slider should not have to send the other, and a client that sent
		// only what it changed would otherwise reset the rest.
		next := s.trim.get()
		if in.Brightness != nil {
			next.Brightness = clampTrim(*in.Brightness)
		}
		if in.Saturation != nil {
			next.Saturation = clampTrim(*in.Saturation)
		}
		s.trim.set(next)
		writeJSON(w, http.StatusOK, wireTrim(next))

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// The wire carries whole numbers from -100 to +100, because that is what a
// slider is, and the inside works in -1 to +1 because that is what a colour is.
func wireTrim(t Trim) map[string]float64 {
	return map[string]float64{
		"brightness": t.Brightness * 100,
		"saturation": t.Saturation * 100,
	}
}

func clampTrim(v float64) float64 {
	v = v / 100
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}
