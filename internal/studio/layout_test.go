package studio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func layoutServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "film.componium")
	const src = `
[score]
componium = "0.1"
title = "t"

[score.media]
duration = "1m"

[[track]]
instrument = "wind.main"
type = "cue"
cues = [ { t = "00:00:01.000", action = "gust" } ]
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: path, Scores: dir})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestLayoutIsEmptyBeforeAnyoneArrangesAnything(t *testing.T) {
	s, _ := layoutServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/layout", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var got layoutState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Order) != 0 || len(got.Collapsed) != 0 {
		t.Errorf("a fresh score came back arranged: %+v", got)
	}
}

func TestLayoutRoundTrips(t *testing.T) {
	s, dir := layoutServer(t)
	body := `{"order":["b","a"],"collapsed":["a"],"hidden":["c"]}`

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/layout", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("put status %d: %s", rec.Code, rec.Body)
	}

	// Beside the score, named after it, and not inside it.
	if _, err := os.Stat(filepath.Join(dir, "film.layout.json")); err != nil {
		t.Fatalf("no sidecar written: %v", err)
	}
	score, err := os.ReadFile(filepath.Join(dir, "film.componium"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(score), "collapsed") {
		t.Error("the arrangement leaked into the score")
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/layout", nil))
	var got layoutState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Order) != 2 || got.Order[0] != "b" || got.Collapsed[0] != "a" || got.Hidden[0] != "c" {
		t.Errorf("round trip gave %+v", got)
	}
}

// A corrupt sidecar costs an arrangement and nothing else. It must not stop
// the editor opening the score.
func TestCorruptLayoutIsDiscardedRatherThanFatal(t *testing.T) {
	s, dir := layoutServer(t)
	if err := os.WriteFile(filepath.Join(dir, "film.layout.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/layout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
}

func TestLayoutRefusesSomethingEnormous(t *testing.T) {
	s, _ := layoutServer(t)
	huge := `{"order":[` + strings.Repeat(`"x",`, 200000) + `"y"]}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/layout", strings.NewReader(huge)))
	if rec.Code == http.StatusOK {
		t.Error("a quarter-megabyte arrangement was accepted")
	}
}
