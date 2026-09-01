package studio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/rig"
	"github.com/Slicit/componium/internal/score"
)

// Arming the studio drives real hardware. So the tests that matter here are
// the ones about it stopping: a page that goes away, a rig that cannot be
// built, a score that names something the rig does not have.

const liveScore = `
[score]
componium = "0.1"
title = "Live"

[score.media]
duration = "00:02:00.000"
fps = 24.0

[[track]]
instrument = "wind.main"
type = "cue"
cues = [
  { t = "00:00:10.000", action = "gust", params = { intensity = 0.8 }, duration = "4s" },
]
`

const liveRig = `
[rig]
name = "all virtual"

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "virtual"
latency = "1.2s"
`

func liveServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, "s.componium")
	rp := filepath.Join(dir, "rig.toml")
	if err := os.WriteFile(sp, []byte(liveScore), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rp, []byte(liveRig), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := score.Load(sp)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rig.Load(rp)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{sc: sc, rig: cfg, rigPath: rp}
	t.Cleanup(s.disarmLive)
	return s
}

func live_(s *Server, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/live", strings.NewReader(body))
	s.handleLive(w, r)
	return w
}

func liveNow(s *Server) LiveState {
	w := httptest.NewRecorder()
	s.handleLive(w, httptest.NewRequest(http.MethodGet, "/api/live", nil))
	var st LiveState
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	return st
}

func report(s *Server, at float64, playing bool) int {
	w := httptest.NewRecorder()
	body := `{"at":` + strings.TrimRight(strings.TrimRight(
		formatFloat(at), "0"), ".") + `,"playing":` + boolText(playing) + `}`
	r := httptest.NewRequest(http.MethodPost, "/api/live/at", strings.NewReader(body))
	s.handleLiveAt(w, r)
	return w.Code
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestNothingIsArmedToBeginWith(t *testing.T) {
	// The only default that is safe. A studio that came up driving whatever
	// was last in the rig would be a studio nobody could open at 2am.
	if st := liveNow(liveServer(t)); st.Armed {
		t.Error("armed on arrival")
	}
}

func TestArmingAndDisarming(t *testing.T) {
	s := liveServer(t)
	if w := live_(s, `{"armed":true}`); w.Code != http.StatusOK {
		t.Fatalf("arming answered %d: %s", w.Code, w.Body)
	}
	st := liveNow(s)
	if !st.Armed || st.Rig != "rig.toml" {
		t.Fatalf("state after arming: %+v", st)
	}
	// Everything here is virtual, and saying so matters: an armed rig that
	// moves nothing looks identical to a broken one from across a room.
	if st.Real != 0 {
		t.Errorf("counted %d real instruments in an all virtual rig", st.Real)
	}

	if w := live_(s, `{"armed":false}`); w.Code != http.StatusOK {
		t.Fatalf("disarming answered %d", w.Code)
	}
	if liveNow(s).Armed {
		t.Error("still armed after being disarmed")
	}
}

func TestThePlayheadOnlyGoesSomewhereWhileArmed(t *testing.T) {
	s := liveServer(t)
	if code := report(s, 10, true); code != http.StatusConflict {
		t.Errorf("reporting to a disarmed studio answered %d, want 409", code)
	}
	live_(s, `{"armed":true}`)
	if code := report(s, 10, true); code != http.StatusNoContent {
		t.Errorf("reporting to an armed studio answered %d", code)
	}
}

func TestAPageThatGoesAwayPutsTheRigAway(t *testing.T) {
	/* The one that matters. A browser tab can be closed, put to sleep or
	 * driven into a tunnel, and none of those look different from the server.
	 * Carrying on would mean a fan driven by a page that no longer exists. */
	was := Quiet
	Quiet = 150 * time.Millisecond
	t.Cleanup(func() { Quiet = was })

	s := liveServer(t)
	if w := live_(s, `{"armed":true}`); w.Code != http.StatusOK {
		t.Fatalf("arming answered %d: %s", w.Code, w.Body)
	}
	report(s, 1, true)
	if st := liveNow(s); !st.Armed || st.Silent {
		t.Fatalf("not driving after a report: %+v", st)
	}

	// And then nobody says anything ever again.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !liveNow(s).Armed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("still armed long after the page stopped reporting")
}

func TestArmingWithNoScoreSaysSo(t *testing.T) {
	s := liveServer(t)
	s.sc = nil
	w := live_(s, `{"armed":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
	var st LiveState
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if !strings.Contains(st.Problem, "score") {
		t.Errorf("did not mention the score: %q", st.Problem)
	}
	// Kept, so a page reopened after a failed arm still finds out why rather
	// than seeing a switch that merely looks off.
	if liveNow(s).Problem == "" {
		t.Error("forgot why it refused")
	}
}

func TestAScoreThatNeedsWhatTheRigLacksIsRefused(t *testing.T) {
	// Worth stopping for now rather than discovering halfway through a film.
	s := liveServer(t)
	s.rig = &rig.Config{
		Rig:         rig.Meta{Name: "empty"},
		Instruments: []rig.InstConfig{{ID: "fog.left", Kind: "fog", Driver: "virtual"}},
	}
	w := live_(s, `{"armed":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "wind.main") {
		t.Errorf("did not name what was missing: %s", w.Body)
	}
}

func TestArmingTwiceDoesNotOpenEverythingTwice(t *testing.T) {
	s := liveServer(t)
	live_(s, `{"armed":true}`)
	if w := live_(s, `{"armed":true}`); w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body)
	}
	if !liveNow(s).Armed {
		t.Error("arming twice left it disarmed")
	}
	s.disarmLive()
	if liveNow(s).Armed {
		t.Error("one disarm did not put away two arms")
	}
}
