package studio

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Serving a firmware image to a browser that will flash it.
//
// Kept out of the embedded bundle on purpose. The image is a build artifact of
// a different toolchain, it is the best part of a megabyte, and it changes on
// its own schedule; committing it into the binary would tie a studio release
// to a firmware release for no reason either of them asked for. So it is a
// directory on disk, named at startup, exactly like the media and the scores.
//
// Empty is a legitimate state and the common one: most people run the studio
// with no firmware anywhere near it, and the page says so rather than failing.

// firmwareManifest is the file esp-web-tools reads. Written by the firmware
// build, not by this package, which only reports whether it is there.
const firmwareManifest = "manifest.json"

// handleFirmwareInfo says whether there is anything to flash, and how big it
// is, so the page can tell the difference between "not built" and "not
// configured" without the person having to guess.
func (s *Server) handleFirmwareInfo(w http.ResponseWriter, r *http.Request) {
	type reply struct {
		Available bool   `json:"available"`
		Why       string `json:"why,omitempty"`
		Manifest  string `json:"manifest,omitempty"`
		Name      string `json:"name,omitempty"`
		Bytes     int64  `json:"bytes,omitempty"`
	}
	say := func(v reply) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}

	if s.firmware == "" {
		say(reply{Why: "the studio was started without -firmware"})
		return
	}
	path := filepath.Join(s.firmware, firmwareManifest)
	if _, err := os.Stat(path); err != nil {
		say(reply{Why: "no manifest.json in " + s.firmware})
		return
	}

	out := reply{Available: true, Manifest: "/firmware/" + firmwareManifest}
	// The image itself, for a size worth showing. Read from the manifest so
	// this does not have to guess at a filename the firmware build chose.
	var m struct {
		Name   string `json:"name"`
		Builds []struct {
			Parts []struct {
				Path string `json:"path"`
			} `json:"parts"`
		} `json:"builds"`
	}
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &m) == nil {
		out.Name = m.Name
		// Every part, not the first one.
		//
		// The first part was the whole image while the firmware was
		// packaged as one blob at offset 0. It is written in pieces now, so
		// that nothing lands on the gap where the wifi credentials and the
		// configuration live, and the first piece is a 26KB bootloader.
		// What somebody wants to know is how much is about to be written.
		if len(m.Builds) > 0 {
			for _, part := range m.Builds[0].Parts {
				st, err := os.Stat(filepath.Join(s.firmware, filepath.Base(part.Path)))
				if err != nil {
					continue
				}
				out.Bytes += st.Size()
			}
		}
	}
	say(out)
}

// handleFirmwareFile serves the manifest and the images.
//
// Its own handler rather than a bare http.FileServer because a directory
// listing of a firmware folder is an invitation, and because a studio started
// without -firmware must answer 404 rather than serving the working directory.
func (s *Server) handleFirmwareFile(w http.ResponseWriter, r *http.Request) {
	if s.firmware == "" {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/firmware/")
	// One flat directory. No traversal, no subdirectories, no listings.
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	switch filepath.Ext(name) {
	case ".json", ".bin":
	default:
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.firmware, name))
}
