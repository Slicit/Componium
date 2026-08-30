package studio

import "time"

// What an analysis did, and how long each part of it took.
//
// An analysis is now several passes with very different costs — a decode
// measured in minutes, a model measured in minutes of a GPU, and two passes
// that read a file and finish before you have let go of the mouse. Without a
// record of them "it took nineteen minutes" is all anybody knows, and the
// question that actually gets asked is which part of it was the nineteen
// minutes.
//
// It is also the only place the reuse shows. A run that skipped the model
// looks, from the outside, exactly like a run that was quick; the difference
// is worth a line.

// Step is one part of an analysis.
type Step struct {
	Name string `json:"name"`
	// Started is when it began, so a person reading the list can see where a
	// run stalled rather than only how long it took in total.
	Started string  `json:"started"`
	Seconds float64 `json:"seconds"`
	// Note says what it did, when what it did is not obvious from the name:
	// how much of the film was quieted, whether the model was asked again.
	Note string `json:"note,omitempty"`
	// State is empty for a step that finished. A step that did not says so.
	State JobState `json:"state,omitempty"`
}

// beginStep closes whatever was running and opens a new one.
//
// The close is the important half. A step's duration is only known when the
// next one starts, so recording the start of each is enough to time all of
// them, and nothing has to remember to stop.
func (j *Jobs) beginStep(film, name string) {
	expect := j.expect(film)
	j.update(JobAnalyse, film, false, func(job *Job) {
		closeStep(job, "", "")
		job.Steps = append(job.Steps, Step{
			Name:    name,
			Started: time.Now().UTC().Format(time.RFC3339),
		})
		job.Progress = predict(job.Steps, expect, len(job.Chunks))
	})
}

// noteStep records what the running step turned out to do.
func (j *Jobs) noteStep(film, note string) {
	j.update(JobAnalyse, film, false, func(job *Job) {
		if len(job.Steps) > 0 {
			job.Steps[len(job.Steps)-1].Note = note
		}
	})
}

// endSteps closes the last step, with a state if it did not finish cleanly.
func (j *Jobs) endSteps(film string, state JobState, note string) {
	j.update(JobAnalyse, film, true, func(job *Job) {
		closeStep(job, state, note)
	})
}

// clearSteps starts the record over, for a run that is starting over.
func (j *Jobs) clearSteps(film string) {
	j.update(JobAnalyse, film, false, func(job *Job) { job.Steps = nil })
}

func closeStep(job *Job, state JobState, note string) {
	if len(job.Steps) == 0 {
		return
	}
	step := &job.Steps[len(job.Steps)-1]
	if step.Seconds > 0 || step.State != "" {
		// Already closed. Beginning a step twice without anything between
		// them should not rewrite the first one's duration.
		return
	}
	if began, err := time.Parse(time.RFC3339, step.Started); err == nil {
		step.Seconds = round3(time.Since(began).Seconds())
	}
	step.State = state
	if note != "" {
		step.Note = note
	}
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

// stepsOf is a copy of what a run has recorded so far.
func (j *Jobs) stepsOf(film string) []Step {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job, ok := j.jobs[jobKey(JobAnalyse, film)]; ok {
		return append([]Step(nil), job.Steps...)
	}
	return nil
}

// finishRecord copies the completed step list onto the version this run kept.
//
// Quietly: the score and its steps are both already on disk, and failing to
// tidy the record is not worth telling anybody about.
func (j *Jobs) finishRecord(film string) {
	j.mu.Lock()
	var id string
	if job, ok := j.jobs[jobKey(JobAnalyse, film)]; ok {
		id = job.Version
	}
	j.mu.Unlock()
	if id == "" {
		return
	}
	_ = j.Restep(film, id, j.stepsOf(film))
}

// Elapsed is how long every step took together, for a summary line.
func Elapsed(steps []Step) float64 {
	total := 0.0
	for _, s := range steps {
		total += s.Seconds
	}
	return round3(total)
}
