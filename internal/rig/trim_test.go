package rig

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Slicit/componium/instruments/virtual"
	"github.com/Slicit/componium/internal/colour"
	"github.com/Slicit/componium/internal/instrument"
	"github.com/Slicit/componium/internal/safety"
)

/* A correction that belongs to a strip rather than to a film.
 *
 * It lives here, in the rig, for one reason worth stating: a show has to have
 * it too. A trim that existed only in the studio would make a room look right
 * in preview and wrong the moment it played, which is a worse state than not
 * having the feature at all.
 */

const trimmedRig = `
[rig]
name = "bench"

[[instrument]]
id = "light.ambient"
kind = "light"
driver = "virtual"
saturation = 0.6

[[instrument]]
id = "light.event"
kind = "light"
driver = "virtual"
brightness = -0.25

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "virtual"
`

func TestATrimSurvivesTheFile(t *testing.T) {
	// The whole request: found once, kept. A number that has to be rediscovered
	// on every restart is not a setting, it is a chore.
	built := builtWith(t, trimmedRig)

	if got := built.Trim("light.ambient"); math.Abs(got.Saturation-0.6) > 1e-9 {
		t.Errorf("light.ambient came up as %+v", got)
	}
	if got := built.Trim("light.event"); math.Abs(got.Brightness+0.25) > 1e-9 {
		t.Errorf("light.event came up as %+v", got)
	}
	if got := built.Trim("wind.main"); !got.Zero() {
		t.Errorf("an untrimmed instrument came up as %+v", got)
	}
}

func TestSavingARigKeepsItsTrims(t *testing.T) {
	/* The failure this guards against has already happened in this project, to
	 * a score: a field the editor did not carry came back empty, was written
	 * out as a default, and a light stayed dark through a whole film while
	 * every counter reported success. */
	path := filepath.Join(t.TempDir(), "rig.toml")
	if err := os.WriteFile(path, []byte(trimmedRig), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}

	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "saturation = 0.6") {
		t.Errorf("the saturation is gone:\n%s", text)
	}
	if !strings.Contains(string(text), "brightness = -0.25") {
		t.Errorf("the brightness is gone:\n%s", text)
	}
	// And an instrument nobody trimmed gains nothing, so a rig that has never
	// been touched reads exactly as it did before this existed.
	for _, unwanted := range []string{"brightness = 0.0", "saturation = 0.0"} {
		if strings.Contains(string(text), unwanted) {
			t.Errorf("an untrimmed instrument was given %q:\n%s", unwanted, text)
		}
	}
}

func TestTheTrimReachesTheInstrument(t *testing.T) {
	// Built from the file and applied to what is dispatched, which is the
	// difference between a number in a file and a colour in a room.
	built := builtWith(t, trimmedRig)
	got := dispatch(t, built, "light.ambient", "set",
		map[string]float64{"h": 0.1217, "s": 0.049, "i": 0.65})

	if math.Abs(got["s"]-0.649) > 1e-9 {
		t.Errorf("saturation arrived as %v", got["s"])
	}
	spread := math.Max(got["r"], math.Max(got["g"], got["b"])) -
		math.Min(got["r"], math.Min(got["g"], got["b"]))
	if spread < 0.15 {
		t.Errorf("r %.3f g %.3f b %.3f is still white", got["r"], got["g"], got["b"])
	}
}

func TestEachInstrumentIsTrimmedByItsOwnNumbers(t *testing.T) {
	/* Without this the wrappers could all be reading the same row and every
	 * other test here would still pass, because they each look at one
	 * instrument and one instrument cannot show a mix up. Which is the actual
	 * hazard: applying one light's correction to another is worse than having
	 * no correction, because the rig would name two and mean one. */
	built := builtWith(t, trimmedRig)
	params := func() map[string]float64 {
		return map[string]float64{"h": 0.3, "s": 0.2, "i": 0.5}
	}

	ambient := dispatch(t, built, "light.ambient", "set", params())
	event := dispatch(t, built, "light.event", "set", params())

	if math.Abs(ambient["s"]-0.8) > 1e-9 || math.Abs(ambient["i"]-0.5) > 1e-9 {
		t.Errorf("light.ambient got s %v i %v, want 0.8 and 0.5", ambient["s"], ambient["i"])
	}
	if math.Abs(event["s"]-0.2) > 1e-9 || math.Abs(event["i"]-0.25) > 1e-9 {
		t.Errorf("light.event got s %v i %v, want 0.2 and 0.25", event["s"], event["i"])
	}
}

func TestAnUntrimmedInstrumentIsLeftExactlyAlone(t *testing.T) {
	// Most of a rig, most of the time, and the state every rig was in before
	// this existed.
	built := builtWith(t, trimmedRig)
	got := dispatch(t, built, "wind.main", "set", map[string]float64{"intensity": 0.8})
	if got["intensity"] != 0.8 || len(got) != 1 {
		t.Errorf("a fan came out as %v", got)
	}
}

func TestATrimCanBeMovedWhileTheRigIsRunning(t *testing.T) {
	// What makes it a knob rather than a setting: the studio moves these while
	// a film plays, and the next cue carries the new value.
	built := builtWith(t, trimmedRig)

	before := dispatch(t, built, "light.event", "set",
		map[string]float64{"h": 0.3, "s": 0.2, "i": 0.5})
	built.SetTrim("light.event", colour.Trim{Saturation: 0.5})
	after := dispatch(t, built, "light.event", "set",
		map[string]float64{"h": 0.3, "s": 0.2, "i": 0.5})

	if math.Abs(before["s"]-0.2) > 1e-9 {
		t.Errorf("before moving it: s %v", before["s"])
	}
	if math.Abs(after["s"]-0.7) > 1e-9 {
		t.Errorf("moving the knob did not reach the next cue: s %v", after["s"])
	}

	// And held to the range, so a number arriving from somewhere that is not a
	// slider cannot mean more than the top of it.
	built.SetTrim("light.event", colour.Trim{Saturation: 9})
	if got := built.Trim("light.event"); got.Saturation != 1 {
		t.Errorf("out of range came back as %+v", got)
	}
}

func TestTheSupervisorsIdeaOfSafeIsNeverTrimmed(t *testing.T) {
	/* The safety property, and worth testing rather than trusting the order of
	 * the wrappers: the rig wraps first and the supervisor wraps around it, so
	 * a forced safe travels through the trim on its way out. It survives
	 * because a stop passes through untouched, and "safe" is a stop.
	 *
	 * A light that a slider left at plus eighty could brighten back on is a
	 * light that cannot be turned off. */
	built := builtWith(t, trimmedRig)
	built.SetTrim("light.ambient", colour.Trim{Brightness: 0.9, Saturation: 0.9})

	inst := built.Instruments["light.ambient"]
	guarded := safety.New(0).Guard(inst)
	inner := innerOf(t, inst)

	for _, action := range []string{"safe", "stop", "off", "neutral"} {
		if err := guarded.Dispatch(instrument.Dispatch{
			Cue: instrument.Cue{Instrument: "light.ambient", Action: action,
				Params: map[string]float64{"h": 0.5, "s": 0, "i": 0}},
		}); err != nil {
			t.Fatal(err)
		}
		got := last(t, inner)
		if got["i"] != 0 || got["s"] != 0 {
			t.Errorf("%q arrived at s %v i %v", action, got["s"], got["i"])
		}
	}
}

// builtWith opens a rig from text and builds it.
func builtWith(t *testing.T, text string) *Built {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rig.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	built, err := c.Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	return built
}

// innerOf reaches past the trim to the instrument the rig actually built.
//
// Asserting the type on the way, which is a check worth having: if Build ever
// stops wrapping, every test above would keep passing on untrimmed values and
// report that the feature works.
func innerOf(t *testing.T, inst instrument.Instrument) *virtual.Instrument {
	t.Helper()
	wrapper, ok := inst.(trimmed)
	if !ok {
		t.Fatalf("the rig did not wrap this instrument at all: %T", inst)
	}
	inner, ok := wrapper.inner.(*virtual.Instrument)
	if !ok {
		t.Fatalf("expected a virtual instrument inside, got %T", wrapper.inner)
	}
	return inner
}

func last(t *testing.T, inner *virtual.Instrument) map[string]float64 {
	t.Helper()
	got := inner.Received()
	if len(got) == 0 {
		t.Fatal("nothing arrived")
	}
	return got[len(got)-1].Cue.Params
}

// dispatch sends one cue through the rig's own wrapper and returns what
// reached the instrument on the far side of it.
func dispatch(t *testing.T, b *Built, id, action string, p map[string]float64) map[string]float64 {
	t.Helper()
	inst, ok := b.Instruments[id]
	if !ok {
		t.Fatalf("no instrument %q", id)
	}
	inner := innerOf(t, inst)
	if err := inst.Dispatch(instrument.Dispatch{
		Cue: instrument.Cue{Instrument: id, Action: action, Params: p},
	}); err != nil {
		t.Fatal(err)
	}
	return last(t, inner)
}
