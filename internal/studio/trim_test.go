package studio

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/Slicit/componium/internal/instrument"
)

/* Two knobs for a strip that does not look like the numbers say it should.
 *
 * The case: a generated ambient curve whose saturation runs between 0.03 and
 * 0.31. The hue swings the whole way round the circle, the timeline draws it
 * beautifully, and at five percent saturation the strip is white. Nothing is
 * wrong with the score and nothing is wrong with the strip.
 */

func TestSaturationCanRescueAColourTooPaleToSee(t *testing.T) {
	/* The number from the real film. Doubling 0.049 gives 0.098, which is
	 * still white, which is why this adds rather than multiplies: the values
	 * that need help are the small ones. */
	pale := map[string]float64{"h": 0.1217, "s": 0.049, "i": 0.65}
	got := Trim{Saturation: 0.30}.Apply(pale)

	if math.Abs(got["s"]-0.349) > 1e-9 {
		t.Errorf("saturation came out %v", got["s"])
	}
	// The point of the exercise: the channels have to actually separate.
	spread := math.Max(got["r"], math.Max(got["g"], got["b"])) -
		math.Min(got["r"], math.Min(got["g"], got["b"]))
	if spread < 0.15 {
		t.Errorf("r %.3f g %.3f b %.3f is still white", got["r"], got["g"], got["b"])
	}
	// And the hue is the film's, not something invented on the way through.
	if got["h"] != 0.1217 {
		t.Errorf("the hue moved to %v", got["h"])
	}
}

func TestTheScoresOwnValuesAreNotEdited(t *testing.T) {
	/* A trim is about a strip, not about a film. The same map is read by the
	 * timeline and by whatever else holds this cue, so adjusting in place
	 * would quietly rewrite the score in memory. */
	original := map[string]float64{"h": 0.5, "s": 0.2, "i": 0.4}
	Trim{Brightness: 0.5, Saturation: 0.5}.Apply(original)
	if original["s"] != 0.2 || original["i"] != 0.4 {
		t.Errorf("the cue was edited in place: %v", original)
	}
}

func TestATrimOfZeroIsExactlyNothing(t *testing.T) {
	// The default, and the state everything was in before this existed. It has
	// to be the identity, not merely close to it, or arming the studio changes
	// what every film looks like.
	p := map[string]float64{"h": 0.5, "s": 0.2, "i": 0.4, "r": 0.32, "g": 0.4, "b": 0.4}
	got := Trim{}.Apply(p)
	if len(got) != len(p) {
		t.Fatalf("a zero trim changed the shape: %v", got)
	}
	for k, v := range p {
		if got[k] != v {
			t.Errorf("%s moved from %v to %v with both sliders at zero", k, v, got[k])
		}
	}
}

func TestAnRGBColourIsTrimmedThroughHSI(t *testing.T) {
	/* Tracks authored in rgb get the same knobs, because the operator is
	 * adjusting a strip and does not know or care which space the film was
	 * written in. */
	got := Trim{Saturation: 1.0}.Apply(map[string]float64{"r": 0.5, "g": 0.5, "b": 0.4})
	if got["s"] != 1 {
		t.Errorf("saturation came out %v", got["s"])
	}
	if math.Abs(got["b"]) > 1e-9 {
		t.Errorf("fully saturated and blue is still %v", got["b"])
	}
}

func TestAnythingThatIsNotAColourIsUntouched(t *testing.T) {
	/* This is asked of every instrument on the rig and answers for lights. A
	 * fan takes an intensity and a fogger takes an output, and neither has any
	 * business being made more saturated. */
	for _, p := range []map[string]float64{
		{"intensity": 0.8},
		{"output": 0.6},
		{"surge": 0.2, "heave": -0.1},
	} {
		got := Trim{Brightness: 1, Saturation: 1}.Apply(p)
		for k, v := range p {
			if got[k] != v {
				t.Errorf("%v became %v", p, got)
				break
			}
		}
	}
}

func TestATrimCannotBrightenAStop(t *testing.T) {
	/* The safety property. A blackout has to stay a blackout however the
	 * sliders are set, or a light that cannot be turned off is one slider away.
	 */
	var sent instrument.Dispatch
	spy := &spyInstrument{onDispatch: func(d instrument.Dispatch) { sent = d }}
	tr := trimmed{inner: spy, id: "light.ambient",
		of: func(string) Trim { return Trim{Brightness: 0.8, Saturation: 0.8} }}

	if err := tr.Dispatch(instrument.Dispatch{
		Cue: instrument.Cue{Instrument: "light.ambient", Action: instrument.ActionStop},
	}); err != nil {
		t.Fatal(err)
	}
	if len(sent.Cue.Params) != 0 {
		t.Errorf("a stop arrived carrying %v", sent.Cue.Params)
	}

	// And the same for the other words that end an effect.
	for _, action := range []string{"off", "safe", "neutral"} {
		sent = instrument.Dispatch{}
		_ = tr.Dispatch(instrument.Dispatch{
			Cue: instrument.Cue{Instrument: "light.ambient", Action: action,
				Params: map[string]float64{"r": 0, "g": 0, "b": 0, "i": 0}},
		})
		if sent.Cue.Params["i"] != 0 {
			t.Errorf("%q came out at intensity %v", action, sent.Cue.Params["i"])
		}
	}
}

func TestTheSlidersAreReadAtDispatchNotAtArming(t *testing.T) {
	// The whole point: they are for moving while a film plays and watching the
	// strip. A value captured when the rig was armed would need a re-arm to
	// take effect, which is the opposite of a knob.
	live := Trim{}
	var sent instrument.Dispatch
	spy := &spyInstrument{onDispatch: func(d instrument.Dispatch) { sent = d }}
	tr := trimmed{inner: spy, id: "light.ambient", of: func(string) Trim { return live }}

	cue := func() instrument.Dispatch {
		return instrument.Dispatch{Cue: instrument.Cue{
			Instrument: "light.ambient", Action: "set",
			Params: map[string]float64{"h": 0.3, "s": 0.1, "i": 0.5},
		}}
	}
	_ = tr.Dispatch(cue())
	if sent.Cue.Params["s"] != 0.1 {
		t.Fatalf("before touching anything: %v", sent.Cue.Params)
	}

	live = Trim{Saturation: 0.6}
	_ = tr.Dispatch(cue())
	if math.Abs(sent.Cue.Params["s"]-0.7) > 1e-9 {
		t.Errorf("moving the slider did not reach the next cue: %v", sent.Cue.Params)
	}
}

func TestEachLightKeepsItsOwnSetting(t *testing.T) {
	/* The reason this is per instrument. An ambient wash behind a screen and
	 * an event strip in a cornice are different parts, bought at different
	 * times, and the number that makes one right makes the other wrong. */
	s, _ := withBoards(t)

	do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.ambient","saturation":40}`)
	do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.event","saturation":-15,"brightness":30}`)

	got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["saturation"] != 40 || got["light.ambient"]["brightness"] != 0 {
		t.Errorf("light.ambient came back as %v", got["light.ambient"])
	}
	if got["light.event"]["saturation"] != -15 || got["light.event"]["brightness"] != 30 {
		t.Errorf("light.event came back as %v", got["light.event"])
	}

	// And moving one does not disturb the other, which is the whole point and
	// the thing a single shared value could not do.
	do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.ambient","saturation":75}`)
	got = trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["saturation"] != 75 {
		t.Errorf("light.ambient did not move: %v", got["light.ambient"])
	}
	if got["light.event"]["saturation"] != -15 || got["light.event"]["brightness"] != 30 {
		t.Errorf("light.event moved when its neighbour did: %v", got["light.event"])
	}
}

func TestOneSliderDoesNotResetTheOther(t *testing.T) {
	// A page moving brightness sends brightness. Absent has to mean leave it,
	// or every drag would zero the field it did not mention.
	s, _ := withBoards(t)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":40}`)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","brightness":-25}`)

	got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["brightness"] != -25 || got["light.ambient"]["saturation"] != 40 {
		t.Errorf("came back as %v", got["light.ambient"])
	}
}

func TestATrimBackAtZeroIsForgottenRatherThanStored(t *testing.T) {
	/* So that what comes back is the set of things somebody has actually
	 * adjusted, and a room where nothing has been touched says so plainly
	 * rather than listing every fixture at zero. */
	s, _ := withBoards(t)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":40}`)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":0}`)

	got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if len(got) != 0 {
		t.Errorf("a trim back at zero is still being kept: %v", got)
	}
}

func TestATrimWithNoInstrumentIsRefused(t *testing.T) {
	/* Rather than applied to everything. A missing name is a page with a bug,
	 * and guessing that it meant the whole room is a way to move a fixture
	 * nobody was looking at. */
	s, _ := withBoards(t)
	w := do(t, s, "POST", "/api/live/trim", `{"saturation":40}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("said %d: %s", w.Code, w.Body.String())
	}
	if len(trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())) != 0 {
		t.Error("it trimmed something anyway")
	}
}

func TestTheSlidersHoldTheirLimits(t *testing.T) {
	// Out of range is held rather than refused: a slider cannot send one, but a
	// script can, and a saturation of 900 should mean the top of the range.
	s, _ := withBoards(t)
	do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.ambient","brightness":900,"saturation":-900}`)

	got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["brightness"] != 100 || got["light.ambient"]["saturation"] != -100 {
		t.Errorf("out of range came back as %v", got["light.ambient"])
	}
}

func TestTrimIsNotSomethingAPageChangesByAccident(t *testing.T) {
	s, _ := withBoards(t)
	if w := do(t, s, "DELETE", "/api/live/trim", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("a DELETE said %d", w.Code)
	}
}

func trimsFrom(t *testing.T, body []byte) map[string]map[string]float64 {
	t.Helper()
	var got struct {
		Trim map[string]map[string]float64 `json:"trim"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%v in %s", err, body)
	}
	return got.Trim
}

type spyInstrument struct {
	onDispatch func(instrument.Dispatch)
}

func (s *spyInstrument) Manifest() instrument.Manifest {
	return instrument.Manifest{ID: "light.ambient", Kind: "light"}
}

func (s *spyInstrument) Dispatch(d instrument.Dispatch) error {
	s.onDispatch(d)
	return nil
}

func TestEachWrapperAsksForItsOwnInstrument(t *testing.T) {
	/* Two strips on one rig, sharing one set of settings, and each must read
	 * the row with its own name on it. Without this the wrappers could all be
	 * asking for the same instrument and every test above would still pass:
	 * they each look at one light, and one light cannot show a mix up.
	 *
	 * Which is the actual hazard. Per instrument that silently applies one
	 * instrument's numbers to another is worse than the single shared value it
	 * replaced, because the page would show two rows and mean one. */
	var holder trimHolder
	holder.set("light.ambient", Trim{Saturation: 0.5})
	holder.set("light.event", Trim{Brightness: -0.4})

	seen := map[string]map[string]float64{}
	wrap := func(id string) trimmed {
		spy := &spyInstrument{onDispatch: func(d instrument.Dispatch) {
			seen[id] = d.Cue.Params
		}}
		return trimmed{inner: spy, id: id, of: holder.get}
	}

	cue := func() instrument.Dispatch {
		return instrument.Dispatch{Cue: instrument.Cue{
			Action: "set",
			Params: map[string]float64{"h": 0.3, "s": 0.2, "i": 0.5},
		}}
	}
	_ = wrap("light.ambient").Dispatch(cue())
	_ = wrap("light.event").Dispatch(cue())

	// The wash got saturation and no brightness.
	if got := seen["light.ambient"]; !near(got["s"], 0.7) || !near(got["i"], 0.5) {
		t.Errorf("light.ambient got s %v i %v, want 0.7 and 0.5", got["s"], got["i"])
	}
	// The flash got brightness and no saturation.
	if got := seen["light.event"]; !near(got["s"], 0.2) || !near(got["i"], 0.1) {
		t.Errorf("light.event got s %v i %v, want 0.2 and 0.1", got["s"], got["i"])
	}
}

func TestAnInstrumentNobodyTrimmedIsLeftAlone(t *testing.T) {
	// Most of a rig, most of the time. Asking for a name that is not in the
	// map has to give back the identity rather than whatever was set last.
	var holder trimHolder
	holder.set("light.ambient", Trim{Saturation: 1})

	var sent instrument.Dispatch
	spy := &spyInstrument{onDispatch: func(d instrument.Dispatch) { sent = d }}
	tr := trimmed{inner: spy, id: "light.event", of: holder.get}

	_ = tr.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "set",
		Params: map[string]float64{"h": 0.3, "s": 0.2, "i": 0.5},
	}})
	if sent.Cue.Params["s"] != 0.2 {
		t.Errorf("an untrimmed light was trimmed anyway: %v", sent.Cue.Params)
	}
}

// near, because 0.2 plus 0.5 is not 0.7 and a test that says it is would be
// failing about arithmetic rather than about routing.
func near(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
