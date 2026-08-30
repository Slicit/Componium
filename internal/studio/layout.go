package studio

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// How a person arranged the timeline, kept beside the score rather than in it.
//
// Track order, which groups are folded, which are hidden: none of that changes
// what the show does, and the score is the file the conductor reads and a
// person hand-edits. Putting view state in it would mean every cosmetic drag
// dirties an artefact that has nothing to do with the arrangement — and worse,
// that two people editing the same score conflict over each other's scrolling.
//
// A sidecar has one real cost: it does not travel when the score is copied
// somewhere else, so an arrangement can be lost. That is the right way round.
// Losing an arrangement is an annoyance; a score that will not load because
// somebody's collapsed-track list confused the parser is not.
type layoutState struct {
	// Order is instrument ids, most significant first. Anything not listed
	// keeps its position from the score, so a rebuild that adds a track can
	// never make it invisible.
	Order []string `json:"order,omitempty"`
	// Collapsed groups, by instrument id.
	Collapsed []string `json:"collapsed,omitempty"`
	// Hidden groups, by instrument id.
	Hidden []string `json:"hidden,omitempty"`
}

// layoutLimit caps what will be read. This file is written by the editor and
// read back without ceremony; a bound keeps a mistake or a hostile client from
// filling the scores directory.
const layoutLimit = 256 << 10

// layoutPath is the sidecar beside whichever score is open.
func (s *Server) layoutPath() string {
	s.mu.Lock()
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".layout.json"
}

func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	path := s.layoutPath()
	if path == "" {
		writeJSON(w, http.StatusOK, layoutState{})
		return
	}

	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(path)
		if err != nil {
			// No sidecar is the normal state of a score nobody has arranged,
			// not an error. An empty arrangement means "use the defaults".
			writeJSON(w, http.StatusOK, layoutState{})
			return
		}
		var out layoutState
		if err := json.Unmarshal(b, &out); err != nil {
			// A corrupt sidecar costs an arrangement and nothing else, so it
			// is discarded rather than allowed to stop the editor opening.
			writeJSON(w, http.StatusOK, layoutState{})
			return
		}
		writeJSON(w, http.StatusOK, out)

	case http.MethodPut:
		var in layoutState
		if err := json.NewDecoder(io.LimitReader(r.Body, layoutLimit)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		b, err := json.MarshalIndent(in, "", "  ")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Write and rename, so an interrupted save leaves the previous
		// arrangement rather than a truncated file.
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := os.Rename(tmp, path); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"saved": filepath.Base(path)})

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
