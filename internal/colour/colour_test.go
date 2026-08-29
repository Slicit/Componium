package colour

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestPrimaries(t *testing.T) {
	cases := []struct {
		name string
		in   HSI
		want RGB
	}{
		{"red", HSI{0, 1, 1}, RGB{1, 0, 0}},
		{"green", HSI{1.0 / 3, 1, 1}, RGB{0, 1, 0}},
		{"blue", HSI{2.0 / 3, 1, 1}, RGB{0, 0, 1}},
		{"red again at a full turn", HSI{1, 1, 1}, RGB{1, 0, 0}},
		{"white", HSI{0, 0, 1}, RGB{1, 1, 1}},
		{"black keeps its hue and shows nothing", HSI{0.5, 1, 0}, RGB{0, 0, 0}},
		{"half a dimmer", HSI{0, 1, 0.5}, RGB{0.5, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ToRGB(c.in)
			if !near(got.R, c.want.R) || !near(got.G, c.want.G) || !near(got.B, c.want.B) {
				t.Errorf("ToRGB(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Intensity is the dimmer, and a dimmer scales what a fixture emits. This is
// the property the safety layer leans on: turning intensity down must turn the
// output down, whatever the colour.
func TestIntensityIsADimmer(t *testing.T) {
	for _, h := range []float64{0, 0.2, 0.5, 0.83} {
		full := ToRGB(HSI{h, 1, 1})
		half := ToRGB(HSI{h, 1, 0.5})
		if !near(half.R, full.R/2) || !near(half.G, full.G/2) || !near(half.B, full.B/2) {
			t.Errorf("at hue %v, half intensity is %v, want half of %v", h, half, full)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, c := range []HSI{
		{0, 1, 1}, {0.15, 0.6, 0.4}, {0.5, 0.2, 0.9}, {0.99, 1, 0.75},
	} {
		back := FromRGB(ToRGB(c))
		if !near(back.H, c.H) || !near(back.S, c.S) || !near(back.I, c.I) {
			t.Errorf("round trip of %v gave %v", c, back)
		}
	}
}

// Editing pushes hue past both ends constantly: dragging a lane up from red
// lands at 1.02, not at magenta.
func TestHueWrapsRatherThanClamping(t *testing.T) {
	if got := ToRGB(HSI{1.25, 1, 1}); !near(got.R, ToRGB(HSI{0.25, 1, 1}).R) {
		t.Errorf("hue 1.25 did not wrap to 0.25")
	}
	if got := ToRGB(HSI{-0.25, 1, 1}); !near(got.B, ToRGB(HSI{0.75, 1, 1}).B) {
		t.Errorf("hue -0.25 did not wrap to 0.75")
	}
}

// The seam is at red, which is not an obscure corner of a lighting score.
func TestHueTakesTheShortWayRound(t *testing.T) {
	a := HSI{0.97, 1, 1}
	b := HSI{0.03, 1, 1}
	mid := Lerp(a, b, 0.5)

	// Half way from 0.97 to 0.03 the short way is 0.00 — red — not 0.5, cyan.
	if !near(mid.H, 0) {
		t.Errorf("midpoint hue = %v, want 0 (through red, not backwards through cyan)", mid.H)
	}

	// And the whole path stays near the seam rather than touring the wheel.
	for i := 0; i <= 10; i++ {
		h := Lerp(a, b, float64(i)/10).H
		if h > 0.06 && h < 0.94 {
			t.Errorf("interpolation wandered to hue %v", h)
		}
	}
}

func TestHueGoesBothWays(t *testing.T) {
	mid := Lerp(HSI{0.03, 1, 1}, HSI{0.97, 1, 1}, 0.5)
	if !near(mid.H, 0) {
		t.Errorf("reverse midpoint hue = %v, want 0", mid.H)
	}
	// A genuine half turn is not a seam crossing and must not be shortcut.
	long := Lerp(HSI{0, 1, 1}, HSI{0.4, 1, 1}, 0.5)
	if !near(long.H, 0.2) {
		t.Errorf("midpoint of 0 to 0.4 = %v, want 0.2", long.H)
	}
}

// White has no hue. Fading white to red must grow into red rather than sweep
// through whatever number was stored next to the white.
func TestUnsaturatedEndsTakeTheirPartnersHue(t *testing.T) {
	white := HSI{H: 0.35, S: 0, I: 1} // a stale hue nobody chose
	red := HSI{H: 0, S: 1, I: 1}

	for i := 1; i < 10; i++ {
		c := Lerp(white, red, float64(i)/10)
		if !near(c.H, 0) {
			t.Fatalf("fading white to red passed through hue %v at step %d", c.H, i)
		}
		rgb := ToRGB(c)
		if rgb.G != rgb.B {
			t.Fatalf("the fade left the red axis: %v", rgb)
		}
	}

	// And the other way round.
	if h := Lerp(red, white, 0.5).H; !near(h, 0) {
		t.Errorf("red to white passed through hue %v", h)
	}
}

func TestTwoNeutralsStayNeutral(t *testing.T) {
	c := Lerp(HSI{0.2, 0, 1}, HSI{0.8, 0, 0.5}, 0.5)
	rgb := ToRGB(c)
	if !near(rgb.R, rgb.G) || !near(rgb.G, rgb.B) {
		t.Errorf("a fade between two greys produced %v", rgb)
	}
}

func TestResolve(t *testing.T) {
	got := Resolve(Params{"h": 0, "s": 1, "i": 0.5})
	if !near(got["r"], 0.5) || !near(got["g"], 0) || !near(got["b"], 0) {
		t.Errorf("Resolve gave %v", got)
	}
	// A dimmer-only fixture reads this, and so does everything asking how hard
	// an effect is running.
	if !near(got["intensity"], 0.5) {
		t.Errorf("intensity = %v, want 0.5", got["intensity"])
	}
	// The authored values survive, so a preview can still show what was meant.
	if !near(got["h"], 0) || !near(got["s"], 1) {
		t.Errorf("Resolve discarded the authored colour: %v", got)
	}
}

// Existing scores are RGB and must keep working untouched.
func TestResolveLeavesRGBAlone(t *testing.T) {
	in := Params{"r": 1, "g": 0.5, "b": 0}
	got := Resolve(in)
	if len(got) != len(in) {
		t.Errorf("Resolve added something to an RGB set: %v", got)
	}
	if Resolve(nil) != nil {
		t.Error("Resolve(nil) should stay nil")
	}
}

func TestNonsenseDoesNotProduceNonsense(t *testing.T) {
	for _, c := range []HSI{
		{math.NaN(), 1, 1}, {0, math.NaN(), 1}, {0, 1, math.NaN()},
		{math.Inf(1), 1, 1}, {0, -5, 3},
	} {
		got := ToRGB(c)
		for _, v := range []float64{got.R, got.G, got.B} {
			if math.IsNaN(v) || v < 0 || v > 1 {
				t.Errorf("ToRGB(%v) = %v, which is not a colour", c, got)
			}
		}
	}
}
