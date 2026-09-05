package studio

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Slicit/componium/internal/rig"
)

// Making rigs, and taking work off this machine.
//
// A shelf that can only be chosen from is a shelf somebody has to fill over
// ssh, which means the studio can manage a rig right up until the moment
// anybody wants a second one. Three things were missing and they are the same
// thing: a rig you can start, a copy you can experiment on, and a file you can
// carry to the machine that runs the room for real.
//
// Export and import are deliberately the file itself rather than some
// interchange format of our own. A rig is TOML that people hand edit; the
// thing worth downloading is exactly what is on disk, and the thing worth
// accepting is anything that would load.

// rigNameProblem says why a name cannot be used, or returns empty.
//
// Strict on purpose. This becomes a filename in a directory that a conductor
// reads on startup, so a name that escapes the shelf is a rig that plays from
// somewhere nobody looked.
func rigNameProblem(name string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "give it a name"
	case len(name) > 64:
		return "that name is longer than 64 characters"
	case name != filepath.Base(name):
		// Redundant today, and kept. The character list below admits no
		// separator on any platform, so it already stops every path this
		// would: removing this line breaks no test. It stays because it is
		// the check that still holds if that list ever grows, and because a
		// name walking out of the shelf writes over a file somewhere nobody
		// is looking.
		return "a name is not a path"
	case strings.HasPrefix(name, "."):
		return "a name starting with a dot is hidden from the shelf that holds it"
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Sprintf("%q is not something to put in a file name; "+
				"letters, digits, dashes and underscores", r)
		}
	}
	return ""
}

// rigFileName turns what somebody typed into what lands on the shelf.
func rigFileName(name string) string {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, ".toml") {
		name += ".toml"
	}
	return name
}

// blankRig is what a new rig starts as.
//
// One virtual light, rather than nothing. A rig with no instruments cannot be
// selected, because the studio needs something to draw and a show needs
// something to drive, and a new rig that immediately refuses to load is a poor
// welcome. Virtual because a new rig knows about no hardware yet.
func blankRig(name string) *rig.Config {
	return &rig.Config{
		Rig: rig.Meta{Name: name},
		Instruments: []rig.InstConfig{{
			ID: "light.ambient", Kind: "light", Driver: "virtual",
			Position: &rig.Position{X: 0, Y: 1.4, Z: -0.1},
		}},
	}
}

// handleRigNew puts a new rig on the shelf, blank or copied from another.
func (s *Server) handleRigNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var want struct {
		Name string `json:"name"`
		// From copies an existing rig instead of starting blank. The usual way
		// a second rig comes to exist: the room as it stands, plus one change.
		From string `json:"from"`
		// Select makes it the one in use straight away.
		Select bool `json:"select"`
	}
	if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rigDir == "" {
		http.Error(w, "this studio was started with a single rig file rather "+
			"than a directory, so there is no shelf to add to", http.StatusConflict)
		return
	}
	if problem := rigNameProblem(want.Name); problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}

	file := rigFileName(want.Name)
	path := filepath.Join(s.rigDir, file)
	if _, err := os.Stat(path); err == nil {
		// Refused rather than overwritten. Somebody typing a name that already
		// exists means to open it, not to erase it.
		http.Error(w, file+" is already on the shelf", http.StatusConflict)
		return
	}

	cfg := blankRig(strings.TrimSuffix(file, ".toml"))
	if want.From != "" {
		from, err := rig.Load(filepath.Join(s.rigDir, rigFileName(want.From)))
		if err != nil {
			http.Error(w, "could not read "+want.From+": "+err.Error(),
				http.StatusBadRequest)
			return
		}
		cfg = from
		cfg.Rig.Name = strings.TrimSuffix(file, ".toml")
	}
	if err := rig.Save(path, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if want.Select {
		if err := rig.Select(s.rigDir, file); err != nil {
			http.Error(w, "made it, but could not select it: "+err.Error(),
				http.StatusInternalServerError)
			return
		}
		if err := s.openChosenRig(); err != nil {
			http.Error(w, "made it, and it will not load: "+err.Error(),
				http.StatusInternalServerError)
			return
		}
	}
	s.sayShelf(w)
}

// handleRigDelete takes one off the shelf.
func (s *Server) handleRigDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var want struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rigDir == "" {
		http.Error(w, "there is no shelf to remove from", http.StatusConflict)
		return
	}
	if problem := rigNameProblem(want.Name); problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}

	file := rigFileName(want.Name)
	files, err := rig.Files(s.rigDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(files) <= 1 {
		// A shelf has to hold something. An empty one is a studio that cannot
		// open and a conductor that cannot start, recoverable only by putting
		// a file there by hand.
		http.Error(w, "this is the only rig on the shelf, and a shelf with "+
			"nothing on it is a studio that will not open", http.StatusConflict)
		return
	}

	if err := os.Remove(filepath.Join(s.rigDir, file)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Whatever was chosen may have just gone. Resolve falls back on its own,
	// so this is a re-open rather than a decision.
	if err := s.openChosenRig(); err != nil {
		http.Error(w, "removed it, and what is left will not load: "+err.Error(),
			http.StatusInternalServerError)
		return
	}
	s.sayShelf(w)
}

// handleRigExport hands back one rig, as the file it is.
func (s *Server) handleRigExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	dir, current := s.rigDir, s.rigPath
	s.mu.Unlock()

	name := r.URL.Query().Get("rig")
	path := current
	if name != "" {
		if problem := rigNameProblem(name); problem != "" {
			http.Error(w, problem, http.StatusBadRequest)
			return
		}
		if dir == "" {
			http.Error(w, "this studio holds a single rig", http.StatusConflict)
			return
		}
		path = filepath.Join(dir, rigFileName(name))
	}
	if path == "" {
		http.Error(w, "this studio was started without a rig", http.StatusConflict)
		return
	}
	serveDownload(w, path)
}

// handleRigImport takes a rig file and puts it on the shelf.
//
// The body is the file. Validated by loading it before anything is written,
// because a shelf is read by a conductor at startup and a rig that will not
// parse is a show that does not begin.
func (s *Server) handleRigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rigDir == "" {
		http.Error(w, "this studio was started with a single rig file rather "+
			"than a directory, so there is nowhere to import to", http.StatusConflict)
		return
	}

	name := r.URL.Query().Get("name")
	if problem := rigNameProblem(name); problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	file := rigFileName(name)
	path := filepath.Join(s.rigDir, file)
	if _, err := os.Stat(path); err == nil && r.URL.Query().Get("replace") != "yes" {
		http.Error(w, file+" is already on the shelf; rename it or say replace",
			http.StatusConflict)
		return
	}

	cfg, err := rig.Parse(body)
	if err != nil {
		http.Error(w, "that is not a rig this build can read: "+err.Error(),
			http.StatusBadRequest)
		return
	}
	if problems := cfg.Validate(); len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"problems": problems})
		return
	}
	if err := rig.Save(path, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.sayShelf(w)
}

// handleScoreExport hands back a score, as the file it is.
func (s *Server) handleScoreExport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	path := s.path
	s.mu.Unlock()

	if film := r.URL.Query().Get("film"); film != "" {
		if problem := rigNameProblem(film); problem != "" {
			http.Error(w, problem, http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		path = s.jobs.ScorePath(film)
		s.mu.Unlock()
	}
	if path == "" {
		http.Error(w, "no score is open", http.StatusConflict)
		return
	}
	serveDownload(w, path)
}

// serveDownload sends a file as an attachment, named as it is on disk.
//
// An attachment rather than a page, because the point is a file on somebody's
// machine: a score opened in a browser tab is a score they then have to work
// out how to save.
func serveDownload(w http.ResponseWriter, path string) {
	body, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "could not read it: "+err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+filepath.Base(path)+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

// sayShelf answers with the shelf as it now stands. The caller holds the lock.
func (s *Server) sayShelf(w http.ResponseWriter) {
	files, err := rig.Files(s.rigDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"shelf":   true,
		"current": filepath.Base(s.rigPath),
		"rigs":    files,
	})
}
