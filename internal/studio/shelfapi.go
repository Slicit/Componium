package studio

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/Slicit/componium/internal/rig"
)

// Choosing which rig is in use.
//
// A bench with a board on it, the room as it actually stands, and the
// demonstration that needs no hardware are three different rigs, and switching
// between them by editing a flag and restarting a container is how people end
// up with one file that is none of them.
//
// The choice is a file on the shelf rather than a setting in the studio, and
// that is deliberate. `-rig` takes a directory as well as a file, so the
// conductor pointed at the same shelf plays whatever was chosen in the browser.
// A selection only the studio knew about would be a selection the thing holding
// the mains does not.

// handleRigs lists the shelf, or moves it.
func (s *Server) handleRigs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rigDir == "" {
		// Started with one file, or with none. Answering with the single rig
		// rather than an error keeps the page's one code path.
		writeJSON(w, http.StatusOK, map[string]any{
			"shelf":   false,
			"current": filepath.Base(s.rigPath),
			"rigs":    []string{},
		})
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var want struct {
			Rig string `json:"rig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
			http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := rig.Select(s.rigDir, want.Rig); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.openChosenRig(); err != nil {
			// The choice is recorded and the file will not load. Say both, or
			// the next person wonders why the studio came up on a rig nobody
			// picked.
			http.Error(w, "chose "+want.Rig+", but it will not load: "+err.Error(),
				http.StatusBadRequest)
			return
		}
	}

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

// openChosenRig points the studio at whatever the shelf now says. The caller
// holds the lock.
func (s *Server) openChosenRig() error {
	path, err := rig.Resolve(s.rigDir)
	if err != nil {
		return err
	}
	cfg, err := rig.Load(path)
	if err != nil {
		return err
	}
	s.rigPath = path
	s.rig = cfg
	return nil
}
