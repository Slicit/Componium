package studio

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// What the model saw, kept beside the score.
//
// The description pass is the only one that costs a GPU and a decode, and the
// only one that cannot be repeated once the film has been analysed and put
// away. Everything after it — which cue a label becomes, whether a splash
// needs corroborating, where the film is calm — is a conclusion drawn from it,
// and conclusions are cheap to draw again. So the observations outlive the run
// that made them, and a mapping can be changed and tried against a feature in
// seconds rather than in half an hour of decoding.
//
// One JSON object per line, which makes a partial file a valid one: the thing
// writing it may be interrupted, and half a description is worth keeping.

// seenSuffix is what the composer appends to the score path it was given.
const seenSuffix = ".seen.jsonl"

// SeenPath is where a film's observations live once the chunks are joined.
func (j *Jobs) SeenPath(film string) string {
	return j.ScorePath(film) + seenSuffix
}

// mergeSeen joins the observation files the chunks wrote, in time order.
//
// Failing to join them is reported and never fatal. The score is the product;
// this is the working out, and losing the working out is not worth losing an
// analysis over.
func (j *Jobs) mergeSeen(film, out string) (int, error) {
	dir := j.partialDir(film)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), seenSuffix) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return 0, nil
	}
	// By name, which is by chunk index: the partials are numbered with a fixed
	// width so that sorting them as strings sorts them as numbers, and the
	// observations inherit that.
	sort.Strings(names)

	var all []string
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) != "" {
				all = append(all, line)
			}
		}
	}
	if len(all) == 0 {
		return 0, nil
	}

	target := out + seenSuffix
	if err := os.WriteFile(target, []byte(strings.Join(all, "\n")+"\n"), 0o644); err != nil {
		return 0, err
	}
	return len(all), nil
}
