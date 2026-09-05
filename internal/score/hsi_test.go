package score

import (
	"testing"
	"time"
)

/* A flash that reaches its fixture with a colour in it.
 *
 * The fault: light.event cues are written as hue, saturation and intensity, and
 * every light driver reads red, green and blue. The conversion is turned on by
 * the track declaring its space, the composer declared it on the wash and never
 * on the flashes, and so a flash arrived carrying three parameters no driver
 * reads and none of the three it does. The driver read three missing numbers as
 * three zeroes and the strip stayed dark, while the cue was acknowledged and
 * counted and logged as delivered.
 *
 * "The wash works and the flashes do not" was the symptom for a long time.
 */

func TestAFlashWrittenInHueReachesTheFixtureInRGB(t *testing.T) {
	// Exactly what the composer emits, space and all: no space at all.
	track := Track{Space: ""}
	got := resolve(track, map[string]float64{"h": 0.2178, "s": 0.3624, "i": 1.0})

	for _, c := range []string{"r", "g", "b"} {
		if _, ok := got[c]; !ok {
			t.Fatalf("no %q: the fixture reads three channels and would be sent "+
				"three zeroes, which is a light that stays off", c)
		}
	}
	// A hue of 0.2178 at full intensity is not black, whatever else it is.
	if got["r"] == 0 && got["g"] == 0 && got["b"] == 0 {
		t.Errorf("resolved to black: %v", got)
	}
	// And the authored values stay, because the preview draws the hue that was
	// meant rather than the channels it became.
	if got["h"] != 0.2178 {
		t.Errorf("the authored hue was lost: %v", got)
	}
}

func TestADeclaredSpaceIsStillTakenAtItsWord(t *testing.T) {
	/* A track that says rgb means rgb. The repair above is for a declaration
	 * that is missing, not one that disagrees: an explicit space is a statement
	 * about what the author meant and nothing here should overrule it. */
	track := Track{Space: "rgb"}
	in := map[string]float64{"r": 1, "g": 0.6, "b": 0.2}
	got := resolve(track, in)
	if len(got) != 3 || got["r"] != 1 || got["g"] != 0.6 || got["b"] != 0.2 {
		t.Errorf("an rgb track was rewritten: %v", got)
	}
}

func TestParametersThatAreNotAColourAreLeftAlone(t *testing.T) {
	/* Every track goes through here, not only the lights. A gust carries an
	 * intensity and a spray carries an output, and neither is a colour. */
	for _, params := range []map[string]float64{
		{"intensity": 0.8},
		{"output": 0.644},
		{},
	} {
		got := resolve(Track{Space: ""}, params)
		if len(got) != len(params) {
			t.Errorf("%v gained channels it has no use for: %v", params, got)
		}
	}
}

func TestAnHSITrackThatSaysSoStillWorks(t *testing.T) {
	// The path that was already right, which the repair must not disturb.
	got := resolve(Track{Space: HSI}, map[string]float64{"h": 0.5, "s": 1, "i": 1})
	if _, ok := got["r"]; !ok {
		t.Error("a declared hsi track lost its conversion")
	}
}

/* A score that declares one colour space and carries another.
 *
 * Not hypothetical. A generated film's ambient curve said `space = "rgb"` over
 * points holding h, s and i, and the strip stayed dark through the whole film
 * while every counter on both sides agreed that it was working.
 */

func TestPointsWinOverADeclarationThatContradictsThem(t *testing.T) {
	/* The declaration is a claim and the keys are evidence. h, s and i mean
	 * hue, saturation and intensity whatever the header says, because the
	 * alternative is a fixture receiving three parameters no driver reads. */
	tr := Track{
		Instrument: "light.ambient", Type: TrackCurve, Interpolation: Linear,
		Space: RGB,
		Points: []Point{
			{T: 0, Value: map[string]float64{"h": 0.87, "s": 0.9, "i": 0.5}},
			{T: Timecode(10 * time.Second), Value: map[string]float64{"h": 0.87, "s": 0.9, "i": 0.5}},
		},
	}
	got := tr.ValueAt(5 * time.Second)
	for _, k := range []string{"r", "g", "b"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("no %q in %v, so this reaches a strip with nothing it can read", k, got)
		}
	}
	// The authored values survive, because a preview wants the hue and a
	// dimmer wants the intensity.
	if got["h"] != 0.87 {
		t.Errorf("the authored hue was lost: %v", got)
	}
	if got["intensity"] == 0 {
		t.Errorf("no intensity for a dimmer to read: %v", got)
	}
}

func TestADeclaredEmptySpaceNeverReachesResolve(t *testing.T) {
	/* The bug behind the bug. resolve used to repair only a track whose space
	 * was empty, and normalise takes a pointer and rewrites an empty space to
	 * rgb before anything reads one. The repair was unreachable from the day
	 * it was written, which is why this asserts the normalisation rather than
	 * trusting it. */
	sc := &Score{
		Meta: Meta{Componium: Version},
		Tracks: []Track{{
			Instrument: "light.ambient", Type: TrackCurve,
			Points: []Point{
				{T: 0, Value: map[string]float64{"h": 0.1, "s": 1, "i": 1}},
				{T: Timecode(1 * time.Second), Value: map[string]float64{"h": 0.1, "s": 1, "i": 1}},
			},
		}},
	}
	if err := sc.normalise(); err != nil {
		t.Fatal(err)
	}
	if sc.Tracks[0].Space != RGB {
		t.Fatalf("space normalised to %q; if this is ever empty again, the old "+
			"guard in resolve becomes reachable and this test is the place to say so",
			sc.Tracks[0].Space)
	}
	// And it still resolves, which is the whole point of no longer depending
	// on that guard.
	if _, ok := sc.Tracks[0].ValueAt(0)["r"]; !ok {
		t.Error("an undeclared hsi track still reaches a fixture unreadable")
	}
}

func TestAGenuineRGBTrackIsLeftAlone(t *testing.T) {
	// The other half of the rule: nothing is invented for a track that already
	// says what it means in the keys it uses.
	tr := Track{
		Instrument: "light.ambient", Type: TrackCurve, Interpolation: Linear,
		Space: RGB,
		Points: []Point{
			{T: 0, Value: map[string]float64{"r": 0.25, "g": 0.5, "b": 0.75}},
			{T: Timecode(10 * time.Second), Value: map[string]float64{"r": 0.25, "g": 0.5, "b": 0.75}},
		},
	}
	got := tr.ValueAt(5 * time.Second)
	if got["r"] != 0.25 || got["g"] != 0.5 || got["b"] != 0.75 {
		t.Errorf("an rgb track was altered: %v", got)
	}
	if _, ok := got["h"]; ok {
		t.Errorf("a hue was invented for an rgb track: %v", got)
	}
}
