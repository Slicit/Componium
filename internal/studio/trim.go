package studio

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Slicit/componium/internal/colour"
	"github.com/Slicit/componium/internal/rig"
)

// The colour trim, from the studio's side.
//
// The numbers themselves live in the rig, which is where a statement about a
// strip belongs and is what makes a show honour them too. This end is the
// knob: it moves them while a film plays, and writes them down so that finding
// a setting is something somebody does once.
//
// Two places have to agree, and they are deliberately not the same place. The
// rig file is the record, read at startup by anything that opens a rig. The
// armed session holds the live copy, because a cue in flight has to be trimmed
// without re-reading a file.

// handleLiveTrim reads and sets the sliders, one instrument at a time.
func (s *Server) handleLiveTrim(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"trim": wireTrims(s.trims())})

	case http.MethodPost, http.MethodPut:
		var in struct {
			Instrument string   `json:"instrument"`
			Brightness *float64 `json:"brightness"`
			Saturation *float64 `json:"saturation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
			return
		}
		if in.Instrument == "" {
			// Refused rather than applied to everything. A missing name is a
			// page with a bug, and guessing that it meant the whole room is a
			// way to move a fixture nobody was looking at.
			http.Error(w, "say which instrument to trim", http.StatusBadRequest)
			return
		}

		// Absent means leave it, rather than means zero. A page moving one
		// slider should not have to send the other, and a client that sent
		// only what it changed would otherwise reset the rest.
		next := s.trims()[in.Instrument]
		if in.Brightness != nil {
			next.Brightness = colour.Clamp(*in.Brightness / 100)
		}
		if in.Saturation != nil {
			next.Saturation = colour.Clamp(*in.Saturation / 100)
		}

		// The room first, then the file. A slider that moved the strip and
		// failed to save is a smaller surprise than one that saved and did
		// nothing, and this way the reason for a failed save is reported while
		// the operator is still looking at the light it just changed.
		s.liveMu.Lock()
		if s.live != nil && s.live.built != nil {
			s.live.built.SetTrim(in.Instrument, next)
		}
		s.liveMu.Unlock()

		s.mu.Lock()
		err := s.rememberTrim(in.Instrument, next)
		s.mu.Unlock()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"trim": wireTrims(s.trims()),
				// Said rather than returned as a failure: the light did change,
				// and the operator needs to know the difference between that
				// and it having been written down.
				"unsaved": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"trim": wireTrims(s.trims())})

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// trims is what is currently in force, from the armed rig if there is one and
// from the file if there is not.
//
// Both, because the sliders have to be readable before anything is armed: a
// page that opened onto zeroes and then jumped when the rig came up would look
// like it had lost the setting.
func (s *Server) trims() map[string]colour.Trim {
	s.liveMu.Lock()
	l := s.live
	s.liveMu.Unlock()
	if l != nil && l.built != nil {
		return l.built.Trims()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]colour.Trim{}
	if s.rig == nil {
		return out
	}
	for _, in := range s.rig.Instruments {
		t := colour.Trim{Brightness: in.Brightness, Saturation: in.Saturation}
		if !t.Zero() {
			out[in.ID] = t
		}
	}
	return out
}

// rememberTrim writes one instrument's correction into the rig file.
//
// Held under the studio's lock by the caller, because this is a read, a change
// and a write of a file the admin page also edits.
func (s *Server) rememberTrim(id string, t colour.Trim) error {
	if s.rigPath == "" || s.rig == nil {
		// A studio started without -rig. The knob still works on the room in
		// front of it; there is simply nowhere to write the answer down, and
		// saying so is better than pretending it was saved.
		return errNoRigFile
	}
	cfg := s.rig
	found := false
	for i := range cfg.Instruments {
		if cfg.Instruments[i].ID != id {
			continue
		}
		cfg.Instruments[i].Brightness = t.Brightness
		cfg.Instruments[i].Saturation = t.Saturation
		found = true
	}
	if !found {
		return nil // a live-only instrument, so nothing to write it against
	}
	return rig.Save(s.rigPath, cfg)
}

// The wire carries whole numbers from -100 to +100, because that is what a
// slider is, and the inside works in -1 to +1 because that is what a colour is.
func wireTrims(all map[string]colour.Trim) map[string]map[string]float64 {
	out := make(map[string]map[string]float64, len(all))
	for id, t := range all {
		out[id] = map[string]float64{
			"brightness": t.Brightness * 100,
			"saturation": t.Saturation * 100,
		}
	}
	return out
}

// errNoRigFile is a studio started without -rig: the knob works, and there is
// nowhere to keep the answer.
var errNoRigFile = errors.New(
	"this studio was started without -rig, so a trim lasts until it restarts")
