package studio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Slicit/componium/internal/score"
)

// A score is kept, not replaced.
//
// Every analysis used to overwrite the last one, which makes tuning the
// composer a guessing game: you change a threshold, rerun, and the thing you
// are comparing against is gone. Worse, you cannot tell an improvement from a
// regression, because there is nothing left to regress from. So each run is
// written beside the ones before it, with enough recorded about it to be
// judged later by someone who was not there.
//
// The live path is untouched and still holds the newest, because the conductor,
// the player and the deploy all name it directly and none of them should have
// to learn about versions to keep working.

// historyDir is where a film's past scores live.
func (j *Jobs) historyDir(film string) string {
	base := strings.TrimSuffix(film, filepath.Ext(film))
	return filepath.Join(j.scores, ".history", base)
}

// Version is one score kept for comparison.
type Version struct {
	// ID is the timestamp it was made, and its filename. Sortable as a
	// string, which is what makes "the newest" a matter of sorting rather
	// than of reading every sidecar.
	ID   string `json:"id"`
	Made string `json:"made"`
	// From and To are the part of the film this score actually covers, in
	// seconds, and Duration is the film's own length. A score of the first
	// fifteen minutes of a feature is a legitimate thing to review, and the
	// only way to review it fairly is to know that is what it is.
	From     float64 `json:"from"`
	To       float64 `json:"to"`
	Duration float64 `json:"duration"`
	Complete bool    `json:"complete"`
	// Note is what produced it — whether the vision seam was on, and so on.
	// Free text on purpose: it is read by a person deciding which two
	// versions to compare, not by anything that has to parse it.
	Note   string        `json:"note,omitempty"`
	Tracks []TrackSummary `json:"tracks,omitempty"`
	// Steps is what the run that made this score did, and what each part of
	// it cost. Kept with the version rather than only on the job, because the
	// job is overwritten by the next run and the question "why was that one
	// slower" is asked afterwards.
	Steps []Step `json:"steps,omitempty"`
	Cues   int            `json:"cues"`
	Points int            `json:"points"`
}

// TrackSummary is one instrument's contribution, for telling two runs apart at
// a glance: "shake went from 900 cues to 40" is the whole story of a change.
type TrackSummary struct {
	Instrument string `json:"instrument"`
	Type       string `json:"type"`
	Cues       int    `json:"cues,omitempty"`
	Points     int    `json:"points,omitempty"`
}

// Label is how a version reads in a picker.
func (v Version) Label() string {
	when := v.ID
	if t, err := time.Parse(versionLayout, v.ID); err == nil {
		when = t.Format("2 Jan 15:04")
	}
	if v.Complete || v.Duration <= 0 {
		return fmt.Sprintf("%s · %d cues", when, v.Cues)
	}
	return fmt.Sprintf("%s · %s of %s · %d cues",
		when, clock(v.To-v.From), clock(v.Duration), v.Cues)
}

// versionLayout sorts as a string in the same order it sorts as a time, which
// is the only property this format is chosen for.
const versionLayout = "20060102-150405"

// summarise describes a score well enough to compare it with another one.
//
// Coverage is taken from the score rather than from what the run intended,
// because the two can differ — a run stopped early covers what it covered, not
// what it set out to — and the honest answer is the one that can be measured
// from the file itself.
func summarise(sc *score.Score, id, note string) Version {
	v := Version{
		ID:       id,
		Made:     time.Now().UTC().Format(time.RFC3339),
		Note:     note,
		Duration: sc.Meta.Media.Duration.Duration().Seconds(),
		From:     -1,
	}
	for _, t := range sc.Tracks {
		s := TrackSummary{Instrument: t.Instrument, Type: string(t.Type),
			Cues: len(t.Cues), Points: len(t.Points)}
		v.Cues += len(t.Cues)
		v.Points += len(t.Points)
		v.Tracks = append(v.Tracks, s)

		for _, c := range t.Cues {
			at := c.T.Duration().Seconds()
			if v.From < 0 || at < v.From {
				v.From = at
			}
			if at > v.To {
				v.To = at
			}
		}
		for _, p := range t.Points {
			at := p.T.Duration().Seconds()
			if v.From < 0 || at < v.From {
				v.From = at
			}
			if at > v.To {
				v.To = at
			}
		}
	}
	if v.From < 0 {
		v.From = 0
	}
	sort.Slice(v.Tracks, func(i, j int) bool {
		return v.Tracks[i].Instrument < v.Tracks[j].Instrument
	})

	// Complete within a chunk of the end, because the last cue in a film is
	// never at the very last frame and demanding that would call every score
	// partial.
	v.Complete = v.Duration <= 0 || v.To >= v.Duration-completeSlack
	return v
}

// completeSlack is how much of a film may have no cues in it and still count
// as analysed to the end. A minute is longer than any credits sequence worth
// scoring and shorter than anything anyone would call a gap.
const completeSlack = 60.0

// Keep writes a score to the history and returns what it recorded.
//
// The live path is written by the caller; this is only the copy kept for
// comparison. Failing to keep a version must never fail an analysis — the
// score is the product and the history is a convenience — so the caller is
// expected to log the error and carry on.
func (j *Jobs) Keep(film string, sc *score.Score, note string) (Version, error) {
	steps := j.stepsOf(film)
	dir := j.historyDir(film)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Version{}, err
	}

	id := time.Now().UTC().Format(versionLayout)
	// A second analysis inside the same second would otherwise overwrite the
	// first, which is precisely the thing this exists to stop.
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, id+".componium")); os.IsNotExist(err) {
			break
		}
		id = fmt.Sprintf("%s-%d", time.Now().UTC().Format(versionLayout), i)
	}

	if err := sc.Save(filepath.Join(dir, id+".componium")); err != nil {
		return Version{}, err
	}
	v := summarise(sc, id, note)
	v.Steps = steps
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return v, err
	}
	return v, os.WriteFile(filepath.Join(dir, id+".json"), body, 0o644)
}

// Restep rewrites a kept version's record of what the run cost.
//
// A version is kept as the score is written, which is part way through the run
// that made it: the passes that quiet the film and apply what the model saw
// come after, and so did not exist to be recorded. Rather than keep the score
// later — it is wanted on disk as early as possible, so an interrupted run
// still leaves one — the record is completed when the run is.
func (j *Jobs) Restep(film, id string, steps []Step) error {
	if id == "" || len(steps) == 0 {
		return nil
	}
	path := filepath.Join(j.historyDir(film), id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var v Version
	if err := json.Unmarshal(body, &v); err != nil {
		return err
	}
	v.Steps = steps
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// Versions lists a film's kept scores, newest first.
//
// A version whose sidecar is missing or unreadable is still listed, described
// by what can be seen from the outside. A score you cannot read the notes for
// is still a score worth loading, and dropping it from the list would make it
// invisible rather than merely undocumented.
func (j *Jobs) Versions(film string) []Version {
	dir := j.historyDir(film)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Version
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".componium") {
			continue
		}
		id := strings.TrimSuffix(name, ".componium")
		v := Version{ID: id}
		if body, err := os.ReadFile(filepath.Join(dir, id+".json")); err == nil {
			_ = json.Unmarshal(body, &v)
			v.ID = id
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].ID > out[k].ID })
	return out
}

// VersionPath is where one kept score lives.
//
// The id is checked against the listing rather than sanitised, so a path that
// climbs out of the directory is not something that has to be got right, it is
// something that cannot be expressed — the same rule the media handler uses.
func (j *Jobs) VersionPath(film, id string) (string, bool) {
	for _, v := range j.Versions(film) {
		if v.ID == id {
			return filepath.Join(j.historyDir(film), id+".componium"), true
		}
	}
	return "", false
}
