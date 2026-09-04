package score

import (
	"testing"
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
