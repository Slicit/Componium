package studio

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"
)

// The switch, and the wire the playhead comes down.
//
// Two endpoints on purpose. Arming is a decision somebody makes and wants an
// answer to; reporting a playhead happens twenty four times a second and wants
// to be as close to nothing as an HTTP request can be.

// handleLive reports what is armed, and arms or disarms it.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.liveState())
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var want struct {
		Armed bool `json:"armed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&want); err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}

	if !want.Armed {
		s.disarmLive()
		s.liveMu.Lock()
		s.liveProblem = ""
		s.liveMu.Unlock()
		writeJSON(w, http.StatusOK, s.liveState())
		return
	}

	s.mu.Lock()
	cfg, sc, name := s.rig, s.sc, filepath.Base(s.rigPath)
	s.mu.Unlock()

	if err := s.armLive(cfg, sc, name); err != nil {
		// Kept, so that a page reopened after a failed arm still finds out
		// why rather than showing a switch that looks merely off.
		s.liveMu.Lock()
		s.liveProblem = err.Error()
		s.liveMu.Unlock()
		writeJSON(w, http.StatusBadRequest, s.liveState())
		return
	}
	writeJSON(w, http.StatusOK, s.liveState())
}

// handleLiveAt takes the playhead from the page.
//
// Deliberately thin. It runs at the film's frame rate for as long as somebody
// is working, so it parses two numbers, hands them over and says nothing back
// unless something has changed. A body on the way out at 24Hz would be a body
// nobody reads.
func (s *Server) handleLiveAt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var at struct {
		At      float64 `json:"at"`
		Playing bool    `json:"playing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&at); err != nil {
		http.Error(w, "could not read that", http.StatusBadRequest)
		return
	}
	if !s.reportLive(time.Duration(at.At*float64(time.Second)), at.Playing) {
		// Nothing is armed. Told plainly, so the page stops reporting rather
		// than talking to a rig that was put away while it was not looking.
		http.Error(w, "not armed", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
