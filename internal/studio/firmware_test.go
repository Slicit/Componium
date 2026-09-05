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

func TestTheFlasherIsToldHowMuchIsAboutToBeWritten(t *testing.T) {
	/* The size used to be the first part's, which was the whole thing while
	 * the firmware was one blob written at offset 0. It is written in pieces
	 * now so that nothing lands on the gap where the wifi credentials and the
	 * device configuration live, and the first piece is a small bootloader.
	 *
	 * A page offering to flash 26KB of a 900KB firmware is wrong in the one
	 * way somebody would notice and not be able to explain. */
	dir := t.TempDir()
	write := func(name string, n int) {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("bootloader.bin", 26112)
	write("partition-table.bin", 3072)
	write("otadata.bin", 8192)
	write("app.bin", 882656)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"name":"Componium node","builds":[{"chipFamily":"ESP32","parts":[
		{"path":"bootloader.bin","offset":4096},
		{"path":"partition-table.bin","offset":32768},
		{"path":"otadata.bin","offset":61440},
		{"path":"app.bin","offset":131072}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{firmware: dir}
	var got struct {
		Available bool  `json:"available"`
		Bytes     int64 `json:"bytes"`
	}
	if err := json.Unmarshal(ask(s, "/api/firmware").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Available {
		t.Fatal("a four part build was not reported as available")
	}
	if want := int64(26112 + 3072 + 8192 + 882656); got.Bytes != want {
		t.Errorf("the page is told %d bytes and %d will be written", got.Bytes, want)
	}
}

func TestNothingIsWrittenWhereTheSettingsLive(t *testing.T) {
	/* The reason for packaging in pieces at all. nvs runs from 0x9000 to
	 * 0xf000 and holds the wifi credentials and the device configuration; a
	 * single image at offset 0 covers it, so every flash over USB erased both
	 * as a side effect of how the image was built. */
	const nvsFrom, nvsTo = 0x9000, 0xf000

	body, err := os.ReadFile(filepath.Join("..", "..", "firmware", "esp32", "web", "manifest.json"))
	if err != nil {
		t.Skip("no packaged firmware here")
	}
	var m struct {
		Builds []struct {
			Parts []struct {
				Path   string `json:"path"`
				Offset int64  `json:"offset"`
			} `json:"parts"`
		} `json:"builds"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Builds) == 0 || len(m.Builds[0].Parts) == 0 {
		t.Fatal("the manifest describes nothing to write")
	}

	dir := filepath.Join("..", "..", "firmware", "esp32", "web")
	for _, part := range m.Builds[0].Parts {
		st, err := os.Stat(filepath.Join(dir, filepath.Base(part.Path)))
		if err != nil {
			t.Errorf("the manifest names %s and it is not there", part.Path)
			continue
		}
		from, to := part.Offset, part.Offset+st.Size()
		if from < nvsTo && to > nvsFrom {
			t.Errorf("%s covers 0x%x to 0x%x and would erase the settings at "+
				"0x%x to 0x%x", part.Path, from, to, nvsFrom, nvsTo)
		}
	}
}
