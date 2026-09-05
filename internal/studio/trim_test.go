package studio

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/* The knobs, from the studio's side.
 *
 * The numbers live in the rig, which is what makes a show honour them too and
 * what makes them survive a restart. This end is the knob and the pen: it
 * moves them while a film plays, and writes them down so that finding a
 * setting is something somebody does once.
 *
 * The colour arithmetic is tested in internal/colour and the wiring in
 * internal/rig. What is left here is the part that could lose somebody's work.
 */

const trimRig = `
[rig]
name = "bench"

[[instrument]]
id = "light.ambient"
kind = "light"
driver = "virtual"

[[instrument]]
id = "light.event"
kind = "light"
driver = "virtual"

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "virtual"
`

// withRig is a studio holding a rig it can write to.
func withRig(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	rigPath := filepath.Join(dir, "rig.toml")
	if err := os.WriteFile(rigPath, []byte(trimRig), 0o644); err != nil {
		t.Fatal(err)
	}
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: score, Rig: rigPath})
	if err != nil {
		t.Fatal(err)
	}
	return s, rigPath
}

func TestATrimIsWrittenDownWhereARestartWillFindIt(t *testing.T) {
	/* The whole request. A number that has to be rediscovered every time the
	 * studio restarts is not a setting, it is a chore, and the point of
	 * spending ten minutes with a strip is not having to do it again. */
	s, rigPath := withRig(t)

	w := do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.ambient","saturation":60,"brightness":-25}`)
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "unsaved") {
		t.Errorf("it could not write it down: %s", w.Body.String())
	}

	text, err := os.ReadFile(rigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "saturation = 0.6") {
		t.Errorf("the rig does not carry it:\n%s", text)
	}
	if !strings.Contains(string(text), "brightness = -0.25") {
		t.Errorf("the rig does not carry it:\n%s", text)
	}

	// And a studio starting fresh against that file finds it, which is the
	// part the operator actually experiences.
	again, err := New(Options{Score: s.path, Rig: rigPath})
	if err != nil {
		t.Fatal(err)
	}
	got := trimsFrom(t, do(t, again, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["saturation"] != 60 || got["light.ambient"]["brightness"] != -25 {
		t.Errorf("a fresh studio came up with %v", got["light.ambient"])
	}
}

func TestEachLightKeepsItsOwnSetting(t *testing.T) {
	/* The reason this is per instrument. An ambient wash behind a screen and
	 * an event strip in a cornice are different parts, bought at different
	 * times, and the number that makes one right makes the other wrong. */
	s, _ := withRig(t)

	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":40}`)
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
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":75}`)
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
	s, _ := withRig(t)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":40}`)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","brightness":-25}`)

	got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["brightness"] != -25 || got["light.ambient"]["saturation"] != 40 {
		t.Errorf("came back as %v", got["light.ambient"])
	}
}

func TestATrimBackAtZeroIsForgottenRatherThanStored(t *testing.T) {
	/* So that what comes back is the set of things somebody has actually
	 * adjusted, and a rig where nothing has been touched says so plainly
	 * rather than listing every fixture at zero. */
	s, rigPath := withRig(t)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":40}`)
	do(t, s, "POST", "/api/live/trim", `{"instrument":"light.ambient","saturation":0}`)

	if got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes()); len(got) != 0 {
		t.Errorf("a trim back at zero is still being kept: %v", got)
	}
	text, err := os.ReadFile(rigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "saturation =") {
		t.Errorf("and it is still in the file:\n%s", text)
	}
}

func TestATrimWithNoInstrumentIsRefused(t *testing.T) {
	/* Rather than applied to everything. A missing name is a page with a bug,
	 * and guessing that it meant the whole room is a way to move a fixture
	 * nobody was looking at. */
	s, _ := withRig(t)
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
	s, _ := withRig(t)
	do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.ambient","brightness":900,"saturation":-900}`)

	got := trimsFrom(t, do(t, s, "GET", "/api/live/trim", "").Body.Bytes())
	if got["light.ambient"]["brightness"] != 100 || got["light.ambient"]["saturation"] != -100 {
		t.Errorf("out of range came back as %v", got["light.ambient"])
	}
}

func TestAStudioWithNoRigFileSaysTheTrimWillNotLast(t *testing.T) {
	/* The knob still works on the room in front of it, and there is nowhere to
	 * write the answer down. Saying so is the difference between a setting the
	 * operator knows is temporary and one they find missing tomorrow. */
	s, _ := withBoards(t) // no -rig
	w := do(t, s, "POST", "/api/live/trim",
		`{"instrument":"light.ambient","saturation":40}`)
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsaved") {
		t.Errorf("it claimed to have saved it: %s", w.Body.String())
	}
}

func TestTrimIsNotSomethingAPageChangesByAccident(t *testing.T) {
	s, _ := withRig(t)
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
