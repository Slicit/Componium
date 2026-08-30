package studio

import (
	"bufio"
	"context"
	"encoding/json"
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
	// JobRunning is being worked on now.
	JobRunning JobState = "running"
	// JobDone finished and wrote its output.
	JobDone JobState = "done"
	// JobFailed did not.
	JobFailed JobState = "failed"
	// JobInterrupted was queued or running when the studio stopped.
	//
	// This exists because the alternative is worse than useless. Job state
	// lives in memory; restarting the container used to drop it silently, so a
	// film that had been analysing for ten minutes came back reporting nothing
	// at all, and one that was half done came back looking untouched. The only
	// honest thing a restarted studio can say about a job it was running is
	// that it no longer is.
	JobInterrupted JobState = "interrupted"
)

// JobKind is what a job does to a film.
type JobKind string

const (
	// JobAnalyse runs the composer and writes a score.
	JobAnalyse JobKind = "analyse"
	// JobPrepare writes a browser-playable copy beside the film.
	JobPrepare JobKind = "prepare"
)

// Job is one piece of work on one film.
type Job struct {
	Kind     JobKind  `json:"kind"`
	Film     string   `json:"film"`
	State    JobState `json:"state"`
	Progress float64  `json:"progress"`
	Label    string   `json:"label"`
	Error    string   `json:"error,omitempty"`
	Started  string   `json:"started,omitempty"`
	Seconds  float64  `json:"seconds,omitempty"`
	// Chunks is the analysis broken into ranges, and how far each got.
	//
	// On the job rather than beside it because it is persisted with the job,
	// and it is persisted because the thing it protects against is the studio
	// stopping. Empty for a prepare, and for an analysis that has not been
	// planned yet.
	Chunks []Chunk `json:"chunks,omitempty"`
	// Version is the id of the score this run kept, so the editor can open
	// the thing that just finished rather than guessing it is the newest.
	Version string `json:"version,omitempty"`
	// Limit stops the analysis after this many seconds of film, for looking
	// at the first quarter of an hour of something rather than waiting for
	// all of it. Zero means the whole film.
	Limit float64 `json:"limit,omitempty"`
	// LookAgain asks for the film to be shown to the model again, throwing
	// away what it said last time. Off unless asked: it is minutes of a GPU,
	// and the answer does not change when a threshold does.
	LookAgain bool `json:"lookAgain,omitempty"`
}

// jobKey identifies a job. Kind is part of it because a film can legitimately
// have an analysis and a preview build outstanding at the same time, and
// keying on the film alone silently loses one of them.
func jobKey(kind JobKind, film string) string {
	return string(kind) + ":" + film
}

// Jobs runs work over films, one at a time.
//
// One at a time on purpose. The composer is CPU bound and single threaded,
// ffmpeg saturates what is left, and the machine this runs on is usually
// small; two of these in parallel finish no sooner together than they would
// one after the other, and make both progress numbers meaningless.
type Jobs struct {
	composer string
	scores   string
	python   string
	mediaDir string
	// devices names the instrument each kind of effect should be addressed
	// to, from the rig the studio is holding. Empty when there is no rig, and
	// the composer then falls back to its own defaults.
	devices []string

	mu      sync.Mutex
	jobs    map[string]*Job
	queue   []string
	running bool
}

// NewJobs prepares a runner. composer is the path to compose.py, scores is the
// directory generated scores are written to.
//
// The composer path is made absolute here. It arrives relative when it was
// found by guessing, and the runner sets the working directory to the
// composer's own folder so its sibling modules import; a relative path then
// resolves against that folder and doubles, which is a confusing way to be
// told a file does not exist.
// WithDevices records which instrument each kind of effect belongs to.
//
// Set after construction rather than passed in, because the rig is optional
// and a studio without one still analyses films perfectly well.
func (j *Jobs) WithDevices(args []string) *Jobs {
	j.devices = append([]string(nil), args...)
	return j
}

func NewJobs(composer, scores, mediaDir string) *Jobs {
	if composer != "" {
		if abs, err := filepath.Abs(composer); err == nil {
			composer = abs
		}
	}
	j := &Jobs{
		composer: composer,
		mediaDir: mediaDir,
		scores:   scores,
		python:   pythonPath(),
		jobs:     map[string]*Job{},
	}
	j.load()
	return j
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

// --- persistence ---

// stateFile is where job state survives a restart. It lives with the scores
// because that is the directory that is already expected to be durable.
func (j *Jobs) stateFile() string {
	if j.scores == "" {
		return ""
	}
	return filepath.Join(j.scores, ".jobs.json")
}

// load restores what was known before the last stop, marking anything that was
// still in flight as interrupted rather than resuming it.
//
// Not resuming is deliberate. The composer writes its score at the end, so a
// half finished analysis has left nothing to continue from, and an interrupted
// ffmpeg has left a partial file that must be thrown away rather than appended
// to. Restarting the work is the only correct recovery, and it should be the
// operator's decision because it costs minutes to hours.
func (j *Jobs) load() {
	path := j.stateFile()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]*Job
	if err := json.Unmarshal(b, &saved); err != nil {
		return
	}
	for k, job := range saved {
		if job == nil {
			continue
		}
		if job.State == JobQueued || job.State == JobRunning {
			job.State = JobInterrupted
			job.Label = "interrupted by a restart"
		}
		// A chunk that was mid-flight is not running any more either, and
		// leaving it saying so puts a spinner in the library against nothing.
		// The state it goes to matters less than it being honest: anything
		// other than done is redone, so this changes what is displayed and not
		// what is worked on.
		for i := range job.Chunks {
			if job.Chunks[i].State == JobRunning || job.Chunks[i].State == JobQueued {
				job.Chunks[i].State = JobInterrupted
			}
		}
		j.jobs[k] = job
	}
}

// save writes job state out. The caller holds the lock.
//
// Called on state changes only, not on every progress update: progress moves
// several times a second and is worth nothing after a restart, while the
// difference between "running" and "failed" is worth everything.
func (j *Jobs) save() {
	path := j.stateFile()
	if path == "" {
		return
	}
	b, err := json.MarshalIndent(j.jobs, "", "  ")
	if err != nil {
		return
	}
	// Write and rename, so a stop midway through leaves the previous state
	// rather than a truncated file that will not parse.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

// --- queueing ---

// Enqueue schedules work, unless the same work is already queued or running.
// EnqueueLimited queues an analysis of only the first seconds of a film.
//
// Useful for judging a change without paying for a feature to find out: the
// resulting score is a real score that happens to be short, and the history
// records how much of the film it covers so nobody compares a quarter of an
// hour against two hours and calls it a regression.
func (j *Jobs) EnqueueLimited(film string, seconds float64) *Job {
	job := j.Enqueue(JobAnalyse, film)
	j.update(JobAnalyse, film, true, func(x *Job) { x.Limit = seconds })
	job.Limit = seconds
	return job
}

func (j *Jobs) Enqueue(kind JobKind, film string) *Job {
	j.mu.Lock()
	defer j.mu.Unlock()

	key := jobKey(kind, film)
	if existing, ok := j.jobs[key]; ok {
		if existing.State == JobQueued || existing.State == JobRunning {
			return existing
		}
	}
	job := &Job{Kind: kind, Film: film, State: JobQueued, Label: "waiting"}
	// Carry the chunk record over from the previous attempt. Queueing a film
	// again is how you resume it, so a fresh job that dropped the record of
	// what was already finished would make resuming mean starting over — the
	// finished pieces would still be on disk, and would still be redone.
	// Reset is the way to discard them, and it is the only way.
	if existing, ok := j.jobs[key]; ok {
		job.Chunks = existing.Chunks
	}
	j.jobs[key] = job
	j.queue = append(j.queue, key)
	j.save()
	j.pump()
	return job
}

// pump starts the next job if nothing is running. The caller holds the lock.
func (j *Jobs) pump() {
	if j.running || len(j.queue) == 0 {
		return
	}
	key := j.queue[0]
	j.queue = j.queue[1:]
	job := j.jobs[key]
	if job == nil {
		j.pump()
		return
	}
	j.running = true
	go j.run(job.Kind, job.Film)
}

// Snapshot returns every job, keyed by kind and film, for the UI.
func (j *Jobs) Snapshot() map[string]Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make(map[string]Job, len(j.jobs))
	for k, v := range j.jobs {
		out[k] = *v
	}
	return out
}

// Pending reports how many jobs are waiting or running, so the UI can say
// whether the queue is busy without walking it.
func (j *Jobs) Pending() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := 0
	for _, job := range j.jobs {
		if job.State == JobQueued || job.State == JobRunning {
			n++
		}
	}
	return n
}

func (j *Jobs) update(kind JobKind, film string, persist bool, fn func(*Job)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if job, ok := j.jobs[jobKey(kind, film)]; ok {
		fn(job)
	}
	if persist {
		j.save()
	}
}

func (j *Jobs) fail(kind JobKind, film string, err error) {
	j.update(kind, film, true, func(job *Job) {
		job.State = JobFailed
		job.Label = "failed"
		job.Error = err.Error()
	})
}

// run does one job, then starts the next.
func (j *Jobs) run(kind JobKind, film string) {
	defer func() {
		j.mu.Lock()
		j.running = false
		j.pump()
		j.mu.Unlock()
	}()

	started := time.Now()
	j.update(kind, film, true, func(job *Job) {
		job.State = JobRunning
		job.Label = "starting"
		job.Progress = 0
		job.Error = ""
		job.Started = started.UTC().Format(time.RFC3339)
	})

	var err error
	switch kind {
	case JobPrepare:
		err = j.runPrepare(film)
	default:
		err = j.runAnalyse(film)
	}
	if err != nil {
		j.fail(kind, film, err)
		return
	}

	j.update(kind, film, true, func(job *Job) {
		job.State = JobDone
		job.Progress = 1
		job.Label = "done"
		job.Seconds = time.Since(started).Seconds()
	})
}

// runAnalyse runs the composer over one film, parsing its progress as it goes.
// runPrepare writes a browser-playable copy of one film.
func (j *Jobs) runPrepare(film string) error {
	if !ffmpegAvailable() {
		return fmt.Errorf("ffmpeg and ffprobe are not installed")
	}
	in := filepath.Join(j.mediaDir, film)
	info, err := probe(in)
	if err != nil {
		return err
	}

	plan := planPreview(info)
	if !plan.Needed {
		j.update(JobPrepare, film, false, func(job *Job) {
			job.Label = plan.Why
		})
		return nil
	}

	j.update(JobPrepare, film, false, func(job *Job) {
		if plan.CopyVideo {
			job.Label = "remuxing: " + plan.Why
		} else {
			job.Label = "re-encoding: " + plan.Why
		}
	})

	out := filepath.Join(j.mediaDir, previewName(film))
	// Build into a partial name and rename on success, so an interrupted run
	// never leaves something that looks like a finished preview. The media
	// listing ignores anything that is not .preview.mp4, so the partial file
	// is invisible until it is complete.
	tmp := out + ".part"

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", plan.ffmpegArgs(in, tmp)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// ffmpeg's own diagnostics go to stderr and its progress to stdout, so
	// both have to be drained or the pipe fills and the encode stalls.
	var tail []string
	var tailMu sync.Mutex
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			tailMu.Lock()
			tail = append(tail, sc.Text())
			if len(tail) > 6 {
				tail = tail[1:]
			}
			tailMu.Unlock()
		}
	}()

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		seconds, ok := parseFFmpegProgress(sc.Text())
		if !ok || info.Duration <= 0 {
			continue
		}
		fraction := seconds / info.Duration
		if fraction > 1 {
			fraction = 1
		}
		j.update(JobPrepare, film, false, func(job *Job) { job.Progress = fraction })
	}

	if err := cmd.Wait(); err != nil {
		os.Remove(tmp)
		tailMu.Lock()
		detail := strings.TrimSpace(strings.Join(tail, "; "))
		tailMu.Unlock()
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("ffmpeg: %s", detail)
	}
	return os.Rename(tmp, out)
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
