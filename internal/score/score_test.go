package score

import (
	"testing"
	"time"
)

const sample = `
[score]
componium = "0.1"
title = "Dune"

[score.media]
duration = "02:35:12.000"
hash = "sha256:abc"
fps = 24.0

[[track]]
instrument = "wind.main"
type = "cue"
cues = [
  { t = "01:04:22.100", action = "gust", params = { intensity = 0.8 }, duration = "4s" },
  { t = "00:12:04.000", action = "gust", params = { intensity = 0.3 } },
]

[[track]]
instrument = "light.ambient"
type = "curve"
interpolation = "linear"
points = [
  { t = "00:00:10.000", value = { r = 0.0, g = 0.0, b = 0.0 } },
  { t = "00:00:20.000", value = { r = 1.0, g = 0.5, b = 0.0 } },
]
`

func TestParseSample(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Meta.Title != "Dune" {
		t.Errorf("title %q", s.Meta.Title)
	}
	if got, want := s.Meta.Media.Duration.Duration(), 2*time.Hour+35*time.Minute+12*time.Second; got != want {
		t.Errorf("duration %v, want %v", got, want)
	}
	if len(s.Tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(s.Tracks))
	}
}

// A hand edited score with a cue inserted in the wrong place is the common
// case, and a miserable thing to debug if it plays out of order.
func TestCuesComeOutSortedRegardlessOfFileOrder(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	cues := s.Cues()
	if len(cues) != 2 {
		t.Fatalf("%d cues, want 2", len(cues))
	}
	if !(cues[0].At < cues[1].At) {
		t.Errorf("cues not sorted: %v then %v", cues[0].At, cues[1].At)
	}
	if cues[0].At != 12*time.Minute+4*time.Second {
		t.Errorf("first cue at %v", cues[0].At)
	}
	if cues[0].Params["intensity"] != 0.3 {
		t.Errorf("params did not survive: %v", cues[0].Params)
	}
}

func TestTimecodeForms(t *testing.T) {
	cases := map[string]time.Duration{
		"01:04:22.100": time.Hour + 4*time.Minute + 22*time.Second + 100*time.Millisecond,
		"04:22.5":      4*time.Minute + 22*time.Second + 500*time.Millisecond,
		"22":           22 * time.Second,
		"1h4m22s":      time.Hour + 4*time.Minute + 22*time.Second,
		"0":            0,
	}
	for in, want := range cases {
		got, err := ParseTimecode(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got.Duration() != want {
			t.Errorf("%q gave %v, want %v", in, got.Duration(), want)
		}
	}
	for _, bad := range []string{"", "banana", "1:2:3:4", "-5:00"} {
		if _, err := ParseTimecode(bad); err == nil {
			t.Errorf("%q parsed but should not have", bad)
		}
	}
}

func TestTimecodeRoundTrip(t *testing.T) {
	in := "01:04:22.100"
	tc, err := ParseTimecode(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := tc.String(); got != in {
		t.Errorf("round trip gave %q, want %q", got, in)
	}
}

func TestCurveInterpolates(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	curves := s.Curves()
	if len(curves) != 1 {
		t.Fatalf("%d curves, want 1", len(curves))
	}
	c := curves[0]

	mid := c.ValueAt(15 * time.Second)
	if mid["r"] != 0.5 {
		t.Errorf("r at halfway is %v, want 0.5", mid["r"])
	}
	if mid["g"] != 0.25 {
		t.Errorf("g at halfway is %v, want 0.25", mid["g"])
	}

	// Before the first point and after the last, a curve holds rather than
	// extrapolating: inventing values the author never wrote would be worse
	// than repeating ones they did.
	before := c.ValueAt(0)
	if before["r"] != 0 {
		t.Errorf("before the curve r is %v, want the first point's 0", before["r"])
	}
	after := c.ValueAt(time.Hour)
	if after["r"] != 1.0 {
		t.Errorf("after the curve r is %v, want the last point's 1.0", after["r"])
	}
}

func TestStepInterpolationHolds(t *testing.T) {
	src := `
[score]
componium = "0.1"
[[track]]
instrument = "light.x"
type = "curve"
interpolation = "step"
points = [
  { t = "0", value = { i = 0.0 } },
  { t = "10", value = { i = 1.0 } },
]
`
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Curves()[0].ValueAt(9 * time.Second)["i"]; got != 0 {
		t.Errorf("step curve gave %v at 9s, want 0", got)
	}
}

func TestRejectsBadScores(t *testing.T) {
	cases := map[string]string{
		"no version":      `[[track]]` + "\ninstrument = \"a\"\n",
		"wrong version":   "[score]\ncomponium = \"9.9\"\n[[track]]\ninstrument = \"a\"\n",
		"no tracks":       "[score]\ncomponium = \"0.1\"\n",
		"no instrument":   "[score]\ncomponium = \"0.1\"\n[[track]]\ntype = \"cue\"\n",
		"cue with points": "[score]\ncomponium = \"0.1\"\n[[track]]\ninstrument = \"a\"\ntype = \"cue\"\npoints = [{ t = \"0\", value = { x = 1.0 } }]\n",
		"short curve":     "[score]\ncomponium = \"0.1\"\n[[track]]\ninstrument = \"a\"\ntype = \"curve\"\npoints = [{ t = \"0\", value = { x = 1.0 } }]\n",
		"cue no action":   "[score]\ncomponium = \"0.1\"\n[[track]]\ninstrument = \"a\"\ntype = \"cue\"\ncues = [{ t = \"1\" }]\n",
		"bad interp":      "[score]\ncomponium = \"0.1\"\n[[track]]\ninstrument = \"a\"\ntype = \"curve\"\ninterpolation = \"bezier\"\npoints = [{ t = \"0\", value = { x = 1.0 } }, { t = \"1\", value = { x = 2.0 } }]\n",
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: parsed but should have been rejected", name)
		}
	}
}

func TestTrackTypeIsInferred(t *testing.T) {
	src := "[score]\ncomponium = \"0.1\"\n[[track]]\ninstrument = \"a\"\npoints = [{ t = \"0\", value = { x = 1.0 } }, { t = \"1\", value = { x = 2.0 } }]\n"
	s, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if s.Tracks[0].Type != TrackCurve {
		t.Errorf("type inferred as %q, want curve", s.Tracks[0].Type)
	}
}

func TestInstrumentsListed(t *testing.T) {
	s, _ := Parse([]byte(sample))
	got := s.Instruments()
	if len(got) != 2 || got[0] != "light.ambient" || got[1] != "wind.main" {
		t.Errorf("instruments %v", got)
	}
}
