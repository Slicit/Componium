package studio

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Slicit/componium/internal/rig"
)

// Saving a rig from the browser writes the rig *file*. Which means the browser
// can destroy things it cannot see, and that is what most of this is about.

const startingRig = `
[rig]
name = "bench"

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "cip"
addr = "192.168.1.91:5570"
latency = "1.2s"
secret = "shared-with-the-node"

[[instrument]]
id = "scent.main"
kind = "scent"
driver = "virtual"

[instrument.scents]
1 = "smoke"
2 = "petrichor"
`

func studioWithRig(t *testing.T) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rig.toml")
	if err := os.WriteFile(path, []byte(startingRig), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := rig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return &Server{rig: cfg, rigPath: path}
}

func putRig(s *Server, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/rig", bytes.NewReader(b))
	s.handleRig(w, r)
	return w
}

func readRig(t *testing.T, s *Server) *rig.Config {
	t.Helper()
	cfg, err := rig.Load(s.rigPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSavingKeepsWhatThePageCannotSee(t *testing.T) {
	// The page sends the handful of fields it can edit. A scent rack and a CIP
	// secret are not among them, and rebuilding each instrument from the wire
	// alone would delete both the first time somebody changed an address.
	// Nothing would announce it.
	s := studioWithRig(t)
	w := putRig(s, wireRig{Name: "bench", Instruments: []wireInstrument{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "192.168.1.99:5570", Latency: 1.2},
		{ID: "scent.main", Kind: "scent", Driver: "virtual"},
	}})
	if w.Code != http.StatusOK {
		t.Fatalf("save answered %d: %s", w.Code, w.Body)
	}

	back := readRig(t, s)
	var wind, scent rig.InstConfig
	for _, in := range back.Instruments {
		switch in.ID {
		case "wind.main":
			wind = in
		case "scent.main":
			scent = in
		}
	}
	if wind.Addr != "192.168.1.99:5570" {
		t.Errorf("the edit did not land: %q", wind.Addr)
	}
	if wind.Secret != "shared-with-the-node" {
		t.Errorf("the CIP secret was lost: %q", wind.Secret)
	}
	if scent.Scents["1"] != "smoke" || scent.Scents["2"] != "petrichor" {
		t.Errorf("the scent rack was lost: %v", scent.Scents)
	}
}

func TestRemovingAnInstrumentRemovesIt(t *testing.T) {
	s := studioWithRig(t)
	putRig(s, wireRig{Name: "bench", Instruments: []wireInstrument{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "192.168.1.91:5570"},
	}})
	if got := readRig(t, s).Instruments; len(got) != 1 || got[0].ID != "wind.main" {
		t.Fatalf("came back as %+v", got)
	}
}

func TestAddingOne(t *testing.T) {
	s := studioWithRig(t)
	w := putRig(s, wireRig{Name: "bench", Instruments: []wireInstrument{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "192.168.1.91:5570"},
		{ID: "scent.main", Kind: "scent", Driver: "virtual"},
		{ID: "light.ambient", Kind: "light", Driver: "sacn",
			Addr: "192.168.1.90:5568", Universe: 1, Start: 1, Mode: "rgb"},
	}})
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body)
	}
	if got := readRig(t, s).Instruments; len(got) != 3 {
		t.Fatalf("%d instruments", len(got))
	}
}

func TestARigThatWouldNotStartIsRefusedWithReasons(t *testing.T) {
	s := studioWithRig(t)
	w := putRig(s, wireRig{Name: "bench", Instruments: []wireInstrument{
		{ID: "wind.main", Kind: "wind", Driver: "sacn"},
	}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("answered %d", w.Code)
	}
	var body struct {
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Problems) == 0 {
		t.Fatal("refused it without saying why")
	}
	// And left the file alone, so a rejected save is not a lost rig.
	if got := readRig(t, s).Instruments; len(got) != 2 {
		t.Errorf("a refused save changed the file: %d instruments", len(got))
	}
}

func TestChangingDriverDropsTheOldDriversSettings(t *testing.T) {
	// A fogger that used to be a light should not keep a DMX address nobody
	// can see, waiting to confuse whoever reads the file next.
	s := studioWithRig(t)
	putRig(s, wireRig{Name: "bench", Instruments: []wireInstrument{
		{ID: "wind.main", Kind: "wind", Driver: "cip", Addr: "192.168.1.91:5570"},
		{ID: "scent.main", Kind: "scent", Driver: "virtual",
			Universe: 4, Start: 9, Mode: "rgb"},
	}})
	for _, in := range readRig(t, s).Instruments {
		if in.ID == "scent.main" && (in.Universe != 0 || in.Start != 0 || in.Mode != "") {
			t.Errorf("kept lighting settings on a virtual scent: %+v", in)
		}
	}
}

func TestAStudioWithNoRigFileWillNotPretendToSave(t *testing.T) {
	s := &Server{}
	if w := putRig(s, wireRig{Name: "x"}); w.Code != http.StatusConflict {
		t.Errorf("answered %d, want 409", w.Code)
	}
}

func TestTheRigItServesSaysWhereEachDeviceIs(t *testing.T) {
	// The gap that made the admin's device list show a dash for every address:
	// the page rendered fields the server had never sent.
	s := studioWithRig(t)
	w := httptest.NewRecorder()
	s.handleRig(w, httptest.NewRequest(http.MethodGet, "/api/rig", nil))

	var got wireRig
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Editable {
		t.Error("a rig read from a file is editable")
	}
	var found bool
	for _, in := range got.Instruments {
		if in.ID == "wind.main" {
			found = in.Addr == "192.168.1.91:5570"
		}
	}
	if !found {
		t.Errorf("no address came back: %+v", got.Instruments)
	}
}
