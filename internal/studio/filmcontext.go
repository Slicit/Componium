package studio

import (
	"os"
	"path/filepath"
	"strings"
)

// What a film is, in the operator's own words.
//
// The model is shown one frame at a time and told nothing about where it came
// from, which is why its descriptions read the same for every film: "a man in
// a military uniform stands in a dimly lit room" is what you get when the
// answer "a hangar deck" was never available. A line of context — a genre, a
// sentence of synopsis, the name of a ship — is enough for it to name what it
// is looking at.
//
// It reaches the sentence and nothing else. That is not a detail: the prompt
// tells the model, deliberately, not to infer from context, because a model
// allowed to reason from the film's genre finds explosions in frames that have
// none. It is the same fault as emphasising a label and getting more of that
// label, which was measured and reverted. So the context is background for the
// description a person reads, and the labels that drive a fogger stay evidence
// about the frame in front of it.
//
// Beside the score rather than beside the film: it belongs to the analysis, it
// is edited far more often than a film is, and the media directory is somewhere
// people drop large files rather than somewhere the studio owns.

const contextSuffix = ".context"

// contextLimit is generous for a paragraph and small enough that the thing on
// the other end of the seam is still being asked about a frame rather than
// being told a story.
const contextLimit = 2000

// ContextPath is where a film's context lives.
func (j *Jobs) ContextPath(film string) string {
	return j.ScorePath(film) + contextSuffix
}

// ReadContext returns what has been said about this film, or "".
//
// A missing file is the normal case and not an error: most films have nothing
// said about them and are analysed exactly as they were before.
func (j *Jobs) ReadContext(film string) string {
	body, err := os.ReadFile(j.ContextPath(film))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// WriteContext records what a film is. Empty removes it.
func (j *Jobs) WriteContext(film, text string) error {
	text = strings.TrimSpace(text)
	if len(text) > contextLimit {
		text = strings.TrimSpace(text[:contextLimit])
	}
	path := j.ContextPath(film)
	if text == "" {
		// Removing it has to be possible, and has to leave no file behind:
		// an empty file and no file must mean the same thing to ReadContext,
		// but only one of them is honest about there being nothing there.
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text+"\n"), 0o644)
}
