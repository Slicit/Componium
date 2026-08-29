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
	s, err := New(path, "", "")
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

// --- the room and the film ---

func TestRigIsInferredWhenNoneIsGiven(t *testing.T) {
	// A preview with no devices in it is not a preview, so the room falls back
	// to whatever the score addresses.
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rig", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/rig returned %d", rec.Code)
	}
	var got wireRig
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Instruments) != 2 {
		t.Fatalf("%d instruments inferred, want 2", len(got.Instruments))
	}
	if got.HasMedia {
		t.Error("reported media when none was loaded")
	}
	for _, in := range got.Instruments {
		if in.Position == [3]float64{} {
			t.Errorf("%s has no position, so the room cannot draw it", in.ID)
		}
	}
}

func TestKindIsTakenFromTheInstrumentIdWhenInferring(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rig", nil))
	var got wireRig
	json.Unmarshal(rec.Body.Bytes(), &got)

	kinds := map[string]string{}
	for _, in := range got.Instruments {
		kinds[in.ID] = in.Kind
	}
	if kinds["wind.main"] != "wind" || kinds["light.ambient"] != "light" {
		t.Errorf("kinds inferred as %v", kinds)
	}
}

func TestDefaultPositionsDifferByKind(t *testing.T) {
	// Everything landing in one spot would make the room useless.
	seen := map[[3]float64]string{}
	for _, kind := range []string{"light", "wind", "shake", "motion", "mist", "fog", "scent"} {
		p := defaultPosition(kind)
		if other, dup := seen[p]; dup {
			t.Errorf("%s and %s share position %v", kind, other, p)
		}
		seen[p] = kind
	}
}

func TestMediaIsRefusedWhenNoneIsLoaded(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /media returned %d with no media, want 404", rec.Code)
	}
}

// Without range requests a browser must download a whole film before it can
// seek, and scrubbing a timeline is the entire point of previewing.
func TestMediaSupportsRangeRequests(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "film.mp4")
	body := bytes.Repeat([]byte("componium"), 1000) // 9000 bytes
	if err := os.WriteFile(media, body, 0o644); err != nil {
		t.Fatal(err)
	}
	scorePath := filepath.Join(dir, "s.componium")
	os.WriteFile(scorePath, []byte(sample), 0o644)

	s, err := New(scorePath, "", media)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/media", nil)
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range request returned %d, want 206", rec.Code)
	}
	if n := rec.Body.Len(); n != 100 {
		t.Errorf("returned %d bytes, want 100", n)
	}
	if !bytes.Equal(rec.Body.Bytes(), body[100:200]) {
		t.Error("returned the wrong slice of the file")
	}
}

func TestMissingMediaFileIsRefusedAtStartup(t *testing.T) {
	dir := t.TempDir()
	scorePath := filepath.Join(dir, "s.componium")
	os.WriteFile(scorePath, []byte(sample), 0o644)
	if _, err := New(scorePath, "", filepath.Join(dir, "nope.mp4")); err == nil {
		t.Error("a missing film was accepted, and would have failed silently later")
	}
}

func TestCueDurationSurvivesTheEditor(t *testing.T) {
	// Spans are the difference between a fog burst and a flash. Dropping the
	// duration on save would quietly turn every span into a momentary cue.
	s, path := newServer(t)
	sc := get(t, s)
	sc.Tracks[0].Cues[0].Duration = 4.5
	if rec := put(t, s, sc); rec.Code != http.StatusOK {
		t.Fatalf("PUT returned %d: %s", rec.Code, rec.Body)
	}
	again := get(t, s)
	if again.Tracks[0].Cues[0].Duration != 4.5 {
		t.Errorf("duration %v after save, want 4.5", again.Tracks[0].Cues[0].Duration)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "4.5s") {
		t.Errorf("file does not carry the duration:\n%s", b)
	}
}

// --- the media picker ---

func mediaDir(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	films := filepath.Join(dir, "films")
	if err := os.MkdirAll(films, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.mp4", "b.mkv", "notes.txt", "poster.jpg"} {
		if err := os.WriteFile(filepath.Join(films, name), bytes.Repeat([]byte("x"), 500), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Something outside the directory, to try to reach.
	os.WriteFile(filepath.Join(dir, "secret.mp4"), []byte("not yours"), 0o644)

	scorePath := filepath.Join(dir, "s.componium")
	os.WriteFile(scorePath, []byte(sample), 0o644)
	s, err := New(scorePath, "", films)
	if err != nil {
		t.Fatal(err)
	}
	return s, films
}

func TestMediaListingOnlyIncludesPlayableFiles(t *testing.T) {
	s, _ := mediaDir(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/media", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d", rec.Code)
	}
	var got []mediaFile
	json.Unmarshal(rec.Body.Bytes(), &got)

	names := map[string]bool{}
	for _, f := range got {
		names[f.Name] = true
	}
	if !names["a.mp4"] || !names["b.mkv"] {
		t.Errorf("films missing from the listing: %v", names)
	}
	if names["notes.txt"] || names["poster.jpg"] {
		t.Errorf("listing includes things that are not films: %v", names)
	}
}

func TestMediaPickerServesTheNamedFile(t *testing.T) {
	s, _ := mediaDir(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media?file=b.mkv", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("returned %d", rec.Code)
	}
	if rec.Body.Len() != 500 {
		t.Errorf("served %d bytes, want 500", rec.Body.Len())
	}
}

// Path traversal is not something that has to be got right here, it is
// something that cannot be expressed: only a name that appeared in the
// listing is ever served.
func TestMediaPickerRefusesAnythingNotInTheListing(t *testing.T) {
	s, _ := mediaDir(t)
	for _, attempt := range []string{
		"../secret.mp4",
		"..%2Fsecret.mp4",
		"/etc/passwd",
		"films/a.mp4",
		"notes.txt",
		"a.mp4/../../secret.mp4",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/media", nil)
		q := req.URL.Query()
		q.Set("file", attempt)
		req.URL.RawQuery = q.Encode()
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%q returned %d, want 404", attempt, rec.Code)
		}
	}
}

func TestMediaDefaultsToTheFirstFilm(t *testing.T) {
	s, _ := mediaDir(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("returned %d with no file named, want the first film", rec.Code)
	}
}

func TestASingleFileStillWorks(t *testing.T) {
	// Pointing at one film rather than a directory must keep working, since
	// that is what every existing invocation does.
	dir := t.TempDir()
	film := filepath.Join(dir, "one.mp4")
	os.WriteFile(film, []byte("hello"), 0o644)
	scorePath := filepath.Join(dir, "s.componium")
	os.WriteFile(scorePath, []byte(sample), 0o644)

	s, err := New(scorePath, "", film)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Errorf("single file mode returned %d %q", rec.Code, rec.Body.String())
	}
}

// embed.FS reports a zero modification time, so Go sends no Last-Modified and
// a browser has nothing to revalidate against. Without an explicit header it
// caches heuristically and keeps showing an old build after an upgrade.
func TestAssetsAreNotCached(t *testing.T) {
	s, _ := newServer(t)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
			t.Errorf("%s served with Cache-Control %q, want no-store", path, got)
		}
	}
}

// Cache-Control alone was not enough: a browser that had already cached the
// old scripts kept serving them alongside a freshly fetched page, producing
// new HTML with old JavaScript and an application that failed silently.
func TestAssetUrlsCarryAContentVersion(t *testing.T) {
	s, _ := newServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if strings.Contains(body, "__V__") {
		t.Fatal("the version placeholder was served unsubstituted")
	}
	for _, asset := range []string{"app.js", "room.js", "state.js", "timeline.js", "style.css"} {
		if !strings.Contains(body, asset+"?v=") {
			t.Errorf("%s is referenced without a version, so it can be served stale", asset)
		}
	}
}

func TestTheVersionIsStableAndShort(t *testing.T) {
	s, _ := newServer(t)
	get := func() string {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		i := strings.Index(rec.Body.String(), "?v=")
		if i < 0 {
			t.Fatal("no version in the page")
		}
		rest := rec.Body.String()[i+3:]
		return rest[:strings.IndexAny(rest, "\"'")]
	}
	first, second := get(), get()
	if first != second {
		t.Errorf("version changed between requests: %q then %q", first, second)
	}
	if len(first) != 12 {
		t.Errorf("version %q is %d characters, want 12", first, len(first))
	}
}
