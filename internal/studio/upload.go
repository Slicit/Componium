package studio

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// safeName checks a filename supplied by a browser.
//
// The rule is that a name must be exactly a filename: no separators, no
// traversal, no hidden files, and an extension this studio can actually play.
// Rejecting rather than sanitising means there is no clever encoding that
// survives the check, because nothing is rewritten.
func safeName(name string) error {
	if name == "" {
		return fmt.Errorf("no name given")
	}
	if name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name must be a plain filename")
	}
	if strings.HasPrefix(name, ".") || strings.Contains(name, "..") {
		return fmt.Errorf("name must not start with a dot or contain ..")
	}
	if isPreview(name) {
		// Reserved, because a preview is generated rather than uploaded.
		// Accepting one would let an upload masquerade as another film's
		// prepared copy and be served in its place.
		return fmt.Errorf(".preview.mp4 is reserved for generated previews")
	}
	if !playable(name) {
		return fmt.Errorf("%s is not a video this studio can play", filepath.Ext(name))
	}
	return nil
}

// mediaDir is the directory films live in, or empty when a single file was
// given. Uploading and deleting only make sense against a directory.
func (s *Server) mediaDir() string {
	if s.media == "" {
		return ""
	}
	info, err := os.Stat(s.media)
	if err != nil || !info.IsDir() {
		return ""
	}
	return s.media
}

// handleUpload streams a film to disk.
//
// The body is the file, rather than a multipart form. Films are gigabytes, and
// streaming a raw body to a temporary file and renaming it is both simpler and
// bounded in memory, which multipart parsing is not by default.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := s.mediaDir()
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "the studio is not serving a media directory, so there is nowhere to upload to",
		})
		return
	}

	name := r.URL.Query().Get("name")
	if err := safeName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Written beside the target and renamed, so a failed or abandoned upload
	// never appears in the library as a playable film.
	partial := filepath.Join(dir, name+".part")
	f, err := os.Create(partial)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	written, err := io.Copy(f, r.Body)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(partial)
		if err == nil {
			err = closeErr
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if written == 0 {
		os.Remove(partial)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty upload"})
		return
	}
	if err := os.Rename(partial, filepath.Join(dir, name)); err != nil {
		os.Remove(partial)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"film": name, "size": written})
}

// handleDelete removes a film, and optionally the score generated from it.
//
// Only a name that appeared in the listing is ever removed, the same rule the
// media handler uses. Deleting is the one operation here that cannot be undone,
// so it will not act on a name it did not itself just offer.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := s.mediaDir()
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "the studio is not serving a media directory",
		})
		return
	}

	want := r.URL.Query().Get("file")
	found := false
	for _, f := range s.mediaFiles() {
		if f.Name == want {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "no such film", http.StatusNotFound)
		return
	}

	if err := os.Remove(filepath.Join(dir, want)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// The preview goes with the film, always and without being asked.
	//
	// Not optional like the score, because it is not anybody's work: it is a
	// derived copy that is useless without the film, and it is frequently the
	// larger of the two files. Deleting a film to reclaim space and leaving
	// several gigabytes of preview behind would defeat the only reason anyone
	// presses this button. A partial from an interrupted prepare goes too.
	preview := previewName(want)
	removedPreview := ""
	if err := os.Remove(filepath.Join(dir, preview)); err == nil {
		removedPreview = preview
	}
	os.Remove(filepath.Join(dir, preview+".part"))

	removedScore := ""
	if r.URL.Query().Get("score") == "1" {
		path := s.jobs.ScorePath(want)
		if err := os.Remove(path); err == nil {
			removedScore = filepath.Base(path)
			s.mu.Lock()
			// If the editor was showing that score, it is now looking at a
			// file that no longer exists. Reopen whatever is left.
			if s.path == path {
				s.sc = nil
				s.openFirstAvailable()
			}
			s.mu.Unlock()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": want, "score": removedScore, "preview": removedPreview,
	})
}
