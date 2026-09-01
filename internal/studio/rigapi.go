package studio

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Slicit/componium/internal/rig"
)

// Editing the rig from the browser.
//
// The rule this obeys, and the reason it is safe: the studio edits *the rig
// file*, not a copy of it. There is still one place that says what is on the
// end of every wire, and it is still a file anybody can open in an editor. A
// studio that kept its own idea of the hardware would be a studio that
// disagrees with the conductor, and the conductor is the one holding the mains.
//
// The consequence worth stating plainly, because the page states it too: the
// conductor reads the rig when it starts. Saving here changes what the next
// show will do, not what a running one is doing.

// handleRigSave replaces the rig with what the page sent.
func (s *Server) handleRigSave(w http.ResponseWriter, r *http.Request) {
	if s.rigPath == "" {
		http.Error(w, "this studio was started without -rig, so there is no file to write", http.StatusConflict)
		return
	}

	var sent wireRig
	if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
		http.Error(w, "could not read that: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := &rig.Config{Rig: rig.Meta{Name: sent.Name}}
	// Merge onto what is already there, keyed by id.
	//
	// The page sends the handful of fields it can edit. An instrument also
	// carries things it cannot: a scent rack, a platform's declared travel, a
	// CIP secret. Building each one from the wire alone would silently delete
	// those the first time anybody moved a slider, which is a worse bug than
	// not being able to edit at all, because nothing announces it.
	existing := map[string]rig.InstConfig{}
	if s.rig != nil {
		for _, in := range s.rig.Instruments {
			existing[in.ID] = in
		}
	}

	for _, in := range sent.Instruments {
		out := existing[in.ID] // zero for one that is new
		out.ID = in.ID
		out.Kind = in.Kind
		out.Driver = in.Driver
		// Forgiving on the way in: a device's address is very often first met
		// as a URL in a browser, and the driver already knows its own port.
		out.Addr = rig.NormaliseAddr(in.Addr, out.Driver)
		out.Universe = in.Universe
		out.Start = in.Start
		out.Mode = in.Mode
		out.Latency = rig.Duration(time.Duration(in.Latency * float64(time.Second)))
		out.Position = &rig.Position{
			X: in.Position[0], Y: in.Position[1], Z: in.Position[2],
		}
		// Fields that belong to a driver the instrument no longer uses are
		// dropped rather than carried: a fogger that used to be a light should
		// not keep a DMX address nobody can see.
		if out.Driver != "sacn" {
			out.Universe, out.Start, out.Mode = 0, 0, ""
		}
		next.Instruments = append(next.Instruments, out)
	}

	if problems := next.Validate(); len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"problems": problems})
		return
	}
	if err := rig.Save(s.rigPath, next); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read back what is now on disk rather than trusting what we just built,
	// so the studio's idea of the rig is the file's idea of it.
	back, err := rig.Load(s.rigPath)
	if err != nil {
		http.Error(w, "saved, but could not read it back: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.rig = back
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "path": s.rigPath})
}

// handleRigOptions tells the page what a rig may contain, so a menu of kinds
// and drivers is built from the same table the loader dispatches on rather
// than from a list somebody typed into the browser and will forget to update.
func (s *Server) handleRigOptions(w http.ResponseWriter, r *http.Request) {
	type kind struct {
		Kind    string   `json:"kind"`
		Drivers []string `json:"drivers"`
	}
	out := struct {
		Kinds    []kind   `json:"kinds"`
		Modes    []string `json:"modes"`
		Editable bool     `json:"editable"`
	}{
		Modes:    []string{"dimmer", "rgb", "rgbw"},
		Editable: s.rigPath != "",
	}
	for _, k := range rig.Kinds() {
		out.Kinds = append(out.Kinds, kind{Kind: k, Drivers: rig.DriversFor(k)})
	}
	writeJSON(w, http.StatusOK, out)
}
