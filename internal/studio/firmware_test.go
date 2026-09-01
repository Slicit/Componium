package studio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A firmware directory is a directory of images a browser is about to write to
// a microcontroller over USB, reached from a page anybody on the network can
// open. So the interesting tests are the refusals.

const testManifest = `{"name":"Componium node","builds":[{"parts":[{"path":"node.bin","offset":0}]}]}`

func withFirmware(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(testManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.bin"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Server{firmware: dir}
}

func ask(s *Server, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if path == "/api/firmware" {
		s.handleFirmwareInfo(w, r)
	} else {
		s.handleFirmwareFile(w, r)
	}
	return w
}

func TestNoFirmwareDirectoryIsNotAnError(t *testing.T) {
	// The ordinary case: most people run the studio nowhere near a soldering
	// iron. The page has to be able to say so rather than showing an error.
	s := &Server{}
	var got struct {
		Available bool   `json:"available"`
		Why       string `json:"why"`
	}
	if err := json.Unmarshal(ask(s, "/api/firmware").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Available {
		t.Error("claimed firmware with no directory configured")
	}
	if got.Why == "" {
		t.Error("said no without saying why")
	}
}

func TestAConfiguredDirectoryWithNoBuildSaysSo(t *testing.T) {
	s := &Server{firmware: t.TempDir()}
	var got struct {
		Available bool `json:"available"`
	}
	_ = json.Unmarshal(ask(s, "/api/firmware").Body.Bytes(), &got)
	if got.Available {
		t.Error("an empty directory is not a build")
	}
}

func TestABuildIsReportedWithItsSize(t *testing.T) {
	s := withFirmware(t)
	var got struct {
		Available bool   `json:"available"`
		Name      string `json:"name"`
		Bytes     int64  `json:"bytes"`
	}
	if err := json.Unmarshal(ask(s, "/api/firmware").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Name != "Componium node" {
		t.Fatalf("build not reported: %+v", got)
	}
	// Read through the manifest rather than guessed at, so the page shows the
	// size of the image that will actually be written.
	if got.Bytes != 2048 {
		t.Errorf("size %d, want 2048", got.Bytes)
	}
}

func TestTheImageIsServed(t *testing.T) {
	s := withFirmware(t)
	w := ask(s, "/firmware/node.bin")
	if w.Code != http.StatusOK || w.Body.Len() != 2048 {
		t.Fatalf("got %d, %d bytes", w.Code, w.Body.Len())
	}
}

func TestNothingElseIs(t *testing.T) {
	s := withFirmware(t)
	// A secret dropped in that directory by a build should not become a
	// download, and a listing of it should not become a menu.
	if err := os.WriteFile(filepath.Join(s.firmware, "sdkconfig"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/firmware/",
		"/firmware/sdkconfig",
		"/firmware/.env",
		"/firmware/sub/dir.bin",
	} {
		if code := ask(s, path).Code; code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, code)
		}
	}
}

func TestServingIsOffWhenNoDirectoryWasGiven(t *testing.T) {
	// Otherwise an unconfigured studio serves its working directory.
	s := &Server{}
	if code := ask(s, "/firmware/manifest.json").Code; code != http.StatusNotFound {
		t.Errorf("answered %d with no directory configured", code)
	}
}
