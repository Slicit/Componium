package studio

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `
[score]
componium = "0.1"
title = "Demo"

[score.media]
duration = "00:02:00.000"
hash = "sha256:keepme"

[[track]]
instrument = "wind.main"
type = "cue"
cues = [
  { t = "00:00:10.000", action = "gust", params = { intensity = 0.8 } },
]

[[track]]
instrument = "light.ambient"
type = "curve"
interpolation = "step"
points = [
  { t = "00:00:00.000", value = { r = 0.0 } },
  { t = "00:00:20.000", value = { r = 1.0 } },
]
`

func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.componium")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func get(t *testing.T, s *Server) wireScore {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/score", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET returned %d: %s", rec.Code, rec.Body)
	}
	var out wireScore
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func put(t *testing.T, s *Server, in wireScore) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(in)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/score", bytes.NewReader(b))
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestServesTheScore(t *testing.T) {
	s, _ := newServer(t)
	got := get(t, s)

	if got.Title != "Demo" {
		t.Errorf("title %q", got.Title)
	}
	if got.Duration != 120 {
		t.Errorf("duration %v, want 120", got.Duration)
	}
	if len(got.Tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(got.Tracks))
	}
	if got.Tracks[0].Cues[0].T != 10 {
		t.Errorf("cue at %v, want 10", got.Tracks[0].Cues[0].T)
	}
}

func TestEditIsWrittenBack(t *testing.T) {
	s, path := newServer(t)
	sc := get(t, s)
	sc.Tracks[0].Cues[0].T = 42
	sc.Tracks[0].Cues[0].Params["intensity"] = 0.25

	if rec := put(t, s, sc); rec.Code != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", rec.Code, rec.Body)
	}
	again := get(t, s)
	if again.Tracks[0].Cues[0].T != 42 {
		t.Errorf("cue time %v after save, want 42", again.Tracks[0].Cues[0].T)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "00:00:42.000") {
		t.Errorf("file does not contain the new time:\n%s", onDisk)
	}
}

// The editor never displays the media hash or the interpolation mode, so
// nothing in the page can carry them. Losing the hash would break the binding
// between a score and its film.
func TestFieldsTheEditorNeverShowsAreNotDestroyed(t *testing.T) {
	s, path := newServer(t)
	sc := get(t, s)
	sc.Title = "Renamed"
	if rec := put(t, s, sc); rec.Code != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", rec.Code, rec.Body)
	}

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "sha256:keepme") {
		t.Errorf("media hash was lost:\n%s", b)
	}
	if !strings.Contains(string(b), `interpolation = "step"`) {
		t.Errorf("interpolation mode was lost:\n%s", b)
	}
}

// The studio must never be able to write a score the player would refuse.
func TestInvalidEditIsRefusedAndTheFileIsUntouched(t *testing.T) {
	s, path := newServer(t)
	before, _ := os.ReadFile(path)

	sc := get(t, s)
	sc.Tracks[0].Cues[0].Action = "" // a cue with no action is not playable
	rec := put(t, s, sc)
	if rec.Code == http.StatusOK {
		t.Fatal("a cue with no action was accepted")
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("a refused edit still modified the file")
	}
}

func TestServesThePage(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / returned %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Componium") {
		t.Error("the page does not look like the studio")
	}
}

func TestUnsupportedMethodIsRejected(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/score", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE returned %d, want 405", rec.Code)
	}
}
