package studio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// JobState is where a build has got to.
type JobState string

const (
	// JobQueued is waiting for the one worker.
	JobQueued JobState = "queued"
	// JobRunning is being analysed now.
	JobRunning JobState = "running"
	// JobDone finished and wrote a score.
	JobDone JobState = "done"
	// JobFailed did not.
	JobFailed JobState = "failed"
)

// Job is one film being analysed.
type Job struct {
	Film     string   `json:"film"`
	State    JobState `json:"state"`
	Progress float64  `json:"progress"`
	Label    string   `json:"label"`
	Error    string   `json:"error,omitempty"`
	Started  string   `json:"started,omitempty"`
	Seconds  float64  `json:"seconds,omitempty"`
}

// Jobs runs the composer over films, one at a time.
//
// One at a time on purpose. The composer is CPU bound and single threaded, and
// the machine this runs on is usually small; two analyses in parallel finish
// no sooner together than they would one after the other, and make the
// progress meaningless.
type Jobs struct {
	composer string
	scores   string
	python   string
	mediaDir string

	mu      sync.Mutex
	jobs    map[string]*Job
	queue   []string
	running bool
}

// NewJobs prepares a runner. composer is the path to compose.py, scores is the
// directory generated scores are written to.
func NewJobs(composer, scores, mediaDir string) *Jobs {
	return &Jobs{
		composer: composer,
		mediaDir: mediaDir,
		scores:   scores,
		python:   pythonPath(),
		jobs:     map[string]*Job{},
	}
}

func pythonPath() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "python3"
}

// Available reports whether analysis can run at all. A studio started without
// a composer beside it should say so rather than offering a button that does
// nothing.
func (j *Jobs) Available() bool {
	if j.composer == "" {
		return false
	}
	_, err := os.Stat(j.composer)
	return err == nil
}

// ScorePath is where a film's generated score lives.
//
// Named after the film rather than hashed, so the directory stays legible and
// a person can delete one by hand.
func (j *Jobs) ScorePath(film string) string {
	base := strings.TrimSuffix(film, filepath.Ext(film))
	return filepath.Join(j.scores, base+".componium")
}

// Enqueue schedules a film, unless it is already queued or running.
func (j *Jobs) Enqueue(film string) *Job {
	j.mu.Lock()
	defer j.mu.Unlock()

	if existing, ok := j.jobs[film]; ok {
		if existing.State == JobQueued || existing.State == JobRunning {
			return existing
		}
	}
	job := &Job{Film: film, State: JobQueued, Label: "waiting"}
	j.jobs[film] = job
	j.queue = append(j.queue, film)
	j.pump()
	return job
}

// pump starts the next job if nothing is running. The caller holds the lock.
func (j *Jobs) pump() {
	if j.running || len(j.queue) == 0 {
		return
	}
	film := j.queue[0]
	j.queue = j.queue[1:]
	j.running = true
	go j.run(film)
}

// Snapshot returns every job, for the UI.
func (j *Jobs) Snapshot() map[string]Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]Job, len(j.jobs))
	for k, v := range j.jobs {
		out[k] = *v
	}
	return out
}

func (j *Jobs) update(film string, fn func(*Job)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job, ok := j.jobs[film]; ok {
		fn(job)
	}
}

// run analyses one film, parsing the composer's progress as it goes.
func (j *Jobs) run(film string) {
	defer func() {
		j.mu.Lock()
		j.running = false
		j.pump()
		j.mu.Unlock()
	}()

	started := time.Now()
	j.update(film, func(job *Job) {
		job.State = JobRunning
		job.Label = "starting"
		job.Started = started.UTC().Format(time.RFC3339)
	})

	out := j.ScorePath(film)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		j.fail(film, err)
		return
	}

	// A generous ceiling rather than none. A wedged ffmpeg would otherwise
	// hold the single worker forever and every other film behind it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	cmd := exec.CommandContext(ctx, j.python, j.composer,
		filepath.Join(j.mediaDir, film),
		"-o", out,
		"--motion-id", "motion.platform",
	)
	cmd.Dir = filepath.Dir(j.composer)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		j.fail(film, err)
		return
	}
	if err := cmd.Start(); err != nil {
		j.fail(film, err)
		return
	}

	var lastLines []string
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if fraction, label, ok := parseProgress(line); ok {
			j.update(film, func(job *Job) {
				job.Progress = fraction
				job.Label = label
			})
			continue
		}
		// Keep the tail, so a failure can say what the composer was
		// complaining about rather than just that it failed.
		lastLines = append(lastLines, line)
		if len(lastLines) > 6 {
			lastLines = lastLines[1:]
		}
	}

	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(strings.Join(lastLines, "; "))
		if detail == "" {
			detail = err.Error()
		}
		j.fail(film, fmt.Errorf("%s", detail))
		return
	}

	j.update(film, func(job *Job) {
		job.State = JobDone
		job.Progress = 1
		job.Label = "done"
		job.Seconds = time.Since(started).Seconds()
	})
}

func (j *Jobs) fail(film string, err error) {
	j.update(film, func(job *Job) {
		job.State = JobFailed
		job.Label = "failed"
		job.Error = err.Error()
	})
}

// parseProgress reads a "PROGRESS 0.450 decoding colour" line.
func parseProgress(line string) (float64, string, bool) {
	if !strings.HasPrefix(line, "PROGRESS ") {
		return 0, "", false
	}
	rest := strings.TrimPrefix(line, "PROGRESS ")
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		return 0, "", false
	}
	f, err := strconv.ParseFloat(rest[:sp], 64)
	if err != nil {
		return 0, "", false
	}
	return f, strings.TrimSpace(rest[sp+1:]), true
}
