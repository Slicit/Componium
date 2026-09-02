package studio

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Slicit/componium/internal/store"
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

// FilmKey is how a film is named in the database.
//
// The base name, without the container extension, which is what the score path
// has always used: `sintel.mkv` and `sintel.mp4` share a score and now share
// their observations. That collision is old and deliberate, and the important
// thing is that both sides collide the same way. An import reading
// `sintel.componium.seen.jsonl` has no way to know which container it came
// from, so keying on anything else would file it under a name nobody asks for.
func FilmKey(film string) string {
	return strings.TrimSuffix(filepath.Base(film), filepath.Ext(film))
}

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

	if j.store != nil {
		obs, err := parseSeen(strings.Join(all, "\n"), FilmKey(film))
		if err != nil {
			return 0, err
		}
		if err := j.store.SaveObservations(context.Background(), obs); err != nil {
			return 0, err
		}
		// And not the file as well. One or the other, or the studio grows a
		// second opinion about what a model said and no way to tell which is
		// older.
		return len(obs), nil
	}

	target := out + seenSuffix
	if err := os.WriteFile(target, []byte(strings.Join(all, "\n")+"\n"), 0o644); err != nil {
		return 0, err
	}
	return len(all), nil
}

// parseSeen turns the joined JSONL into rows for a film.
//
// A line that will not parse is skipped rather than fatal, for the same reason
// the reader skips one: the file is written a line at a time by something that
// can be interrupted, and a half written line is not a reason to lose the
// several thousand above it.
func parseSeen(body, film string) ([]store.Observation, error) {
	var out []store.Observation
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var o Observation
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			continue
		}
		out = append(out, store.Observation{
			Film: film, At: o.T, Place: o.Place, Doing: o.Doing,
			Seen: o.Seen, Labels: o.Labels,
		})
	}
	return out, nil
}

// Observation is one moment the model was shown, and what it said about it.
//
// The time is counted from the start of the film, the labels are the closed
// vocabulary the composer can act on, and the sentence is what the model
// volunteered. The sentence drives nothing; it is there so a person reading
// this can tell a model that missed something from one that was never shown it,
// which is a distinction no count of labels can make.
type Observation struct {
	T      float64  `json:"t"`
	Labels []string `json:"labels,omitempty"`
	Seen   string   `json:"seen,omitempty"`
	// Where the film is and what is happening in it, as the scene pass reads
	// them. Carried since the schema has somewhere to put them; the composer
	// used to drop them at this boundary for no reason anybody could name.
	Place string `json:"place,omitempty"`
	Doing string `json:"doing,omitempty"`
}

// HasSeen reports whether a description is kept for this film.
//
// A stat rather than a read. The library asks this for every film every time it
// polls, and a feature analysed every two seconds has thousands of lines.
func (j *Jobs) HasSeen(film string) bool {
	if j.store != nil {
		has, err := j.store.HasObservations(context.Background(), FilmKey(film))
		return err == nil && has
	}
	info, err := os.Stat(j.SeenPath(film))
	return err == nil && !info.IsDir() && info.Size() > 0
}

// ReadSeen returns what the model said about a film, in time order.
//
// A line that will not parse is skipped rather than fatal. The file is written
// a line at a time by something that can be interrupted, so a half written last
// line is an ordinary way for this to end and is not a reason to refuse the
// several thousand lines above it.
func (j *Jobs) ReadSeen(film string) ([]Observation, error) {
	if j.store != nil {
		rows, err := j.store.Observations(context.Background(), FilmKey(film))
		if err != nil {
			return nil, err
		}
		out := make([]Observation, 0, len(rows))
		for _, r := range rows {
			out = append(out, Observation{
				T: r.At, Labels: r.Labels, Seen: r.Seen,
				Place: r.Place, Doing: r.Doing,
			})
		}
		return out, nil
	}

	body, err := os.ReadFile(j.SeenPath(film))
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var o Observation
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			continue
		}
		out = append(out, o)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].T < out[b].T })
	return out, nil
}
