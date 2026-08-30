package studio

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Slicit/componium/internal/score"
)

// --- chunk state ------------------------------------------------------------
//
// Chunk state lives on the job and is persisted with it, which is the entire
// point: the thing it protects against is the studio stopping, so a record
// that does not survive that protects against nothing.

func (j *Jobs) chunksOf(film string) []Chunk {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.jobs[jobKey(JobAnalyse, film)]
	if !ok {
		return nil
	}
	return append([]Chunk(nil), job.Chunks...)
}

func (j *Jobs) setChunks(film string, chunks []Chunk) {
	j.update(JobAnalyse, film, true, func(job *Job) {
		job.Chunks = append([]Chunk(nil), chunks...)
	})
}

func (j *Jobs) markChunk(film string, index int, state JobState, why string) {
	j.update(JobAnalyse, film, true, func(job *Job) {
		if index < 0 || index >= len(job.Chunks) {
			return
		}
		job.Chunks[index].State = state
		job.Chunks[index].Error = why
	})
}

// finishChunk records a chunk as done, and is deliberately the only thing that
// does. Called after the partial is on disk, never before, so "done" always
// means there is something to merge.
func (j *Jobs) finishChunk(film string, index int, seconds float64) {
	j.update(JobAnalyse, film, true, func(job *Job) {
		if index < 0 || index >= len(job.Chunks) {
			return
		}
		job.Chunks[index].State = JobDone
		job.Chunks[index].Error = ""
		job.Chunks[index].Seconds = seconds
	})
}

// ResetAnalysis throws away every partial and all chunk state for one film, so
// the next run starts from nothing.
//
// Wanted for the case where the partials are suspect rather than missing — a
// composer that changed its mind about something, a film replaced under the
// same name — which resume cannot help with by design, because resume trusts
// what is already there.
func (j *Jobs) ResetAnalysis(film string) error {
	j.update(JobAnalyse, film, true, func(job *Job) {
		job.Chunks = nil
		job.Progress = 0
		job.Label = ""
		job.Error = ""
		// Not JobQueued: nothing is queued until somebody asks for it. Saying
		// queued here would show a film as waiting for a worker that is never
		// going to pick it up.
		job.State = ""
	})
	if err := os.RemoveAll(j.partialDir(film)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- merging ----------------------------------------------------------------

// mergePartials joins a film's finished chunks into its score.
//
// The partials are removed only once the score is written. They are the only
// copy of the work until then, and deleting them first would turn a failure to
// write into a failure to have ever analysed the film.
func (j *Jobs) mergePartials(film, out, note string) error {
	dir := j.partialDir(film)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading the finished pieces: %w", err)
	}

	var names []string
	for _, e := range entries {
		// Only the scores. The observations the vision pass writes live in
		// this directory too, and handing a JSON lines file to a TOML parser
		// fails at the first brace — which is how this was found.
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".componium") {
			names = append(names, e.Name())
		}
	}
	// By name, which is by index: the files are numbered with a fixed width
	// so that sorting them as strings sorts them as numbers.
	sort.Strings(names)

	var parts []*score.Score
	for _, name := range names {
		sc, err := score.Load(dir + string(os.PathSeparator) + name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		parts = append(parts, sc)
	}
	if len(parts) == 0 {
		return fmt.Errorf("no finished pieces to join")
	}

	merged, err := score.Merge(parts)
	if err != nil {
		return err
	}
	if err := merged.Save(out); err != nil {
		return err
	}

	// The observations travel with the score. Reported rather than fatal: the
	// score is the product and this is the working out, and losing the working
	// out is not worth losing an analysis over.
	if n, err := j.mergeSeen(film, out); err != nil {
		j.update(JobAnalyse, film, false, func(job *Job) {
			job.Label = "kept the score, but not what the model saw: " + err.Error()
		})
	} else if n > 0 {
		j.update(JobAnalyse, film, false, func(job *Job) {
			job.Label = "done"
		})
	}
	// Kept before the partials are cleared, so a failure here still leaves
	// something to recover from. Failing to keep a version is reported and
	// not fatal: the score is the product, the history is a convenience, and
	// losing an analysis over a bookkeeping error would be a poor trade.
	if v, err := j.Keep(film, merged, note); err != nil {
		j.update(JobAnalyse, film, false, func(job *Job) {
			job.Label = "kept the score, but not a copy: " + err.Error()
		})
	} else {
		j.update(JobAnalyse, film, true, func(job *Job) {
			job.Version = v.ID
		})
	}
	return os.RemoveAll(dir)
}
