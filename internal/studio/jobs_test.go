package studio

import (
	"os"
	"path/filepath"
	"testing"
)

// A restart used to lose every job silently. A film that had been analysing
// for ten minutes came back reporting nothing at all, which reads as "it never
// started" rather than "it was killed" — and the difference matters, because
// one of those means press the button again and the other means wait.
func TestJobsSurviveARestart(t *testing.T) {
	dir := t.TempDir()

	first := NewJobs("", dir, dir)
	// Reach past Enqueue: enqueuing would start the worker, and this is about
	// what is written down, not about running anything.
	first.mu.Lock()
	first.jobs[jobKey(JobAnalyse, "a.mkv")] = &Job{
		Kind: JobAnalyse, Film: "a.mkv", State: JobRunning, Progress: 0.45, Label: "decoding",
	}
	first.jobs[jobKey(JobPrepare, "a.mkv")] = &Job{
		Kind: JobPrepare, Film: "a.mkv", State: JobQueued, Label: "waiting",
	}
	first.jobs[jobKey(JobAnalyse, "b.mkv")] = &Job{
		Kind: JobAnalyse, Film: "b.mkv", State: JobDone, Progress: 1, Label: "done",
	}
	first.jobs[jobKey(JobAnalyse, "c.mkv")] = &Job{
		Kind: JobAnalyse, Film: "c.mkv", State: JobFailed, Label: "failed", Error: "boom",
	}
	first.save()
	first.mu.Unlock()

	if _, err := os.Stat(filepath.Join(dir, ".jobs.json")); err != nil {
		t.Fatalf("no state file written: %v", err)
	}

	second := NewJobs("", dir, dir)
	got := second.Snapshot()

	// Anything in flight is interrupted, not resumed and not forgotten. Not
	// resumed because there is nothing to resume from: the composer writes its
	// score at the end, and a half finished encode is a partial file to throw
	// away rather than append to.
	if j := got[jobKey(JobAnalyse, "a.mkv")]; j.State != JobInterrupted {
		t.Errorf("running analysis came back as %q, want %q", j.State, JobInterrupted)
	}
	if j := got[jobKey(JobPrepare, "a.mkv")]; j.State != JobInterrupted {
		t.Errorf("queued prepare came back as %q, want %q", j.State, JobInterrupted)
	}
	// And how far it had got is still there, because "interrupted at 45%" is a
	// more useful thing to be told than "interrupted".
	if j := got[jobKey(JobAnalyse, "a.mkv")]; j.Progress != 0.45 {
		t.Errorf("progress lost: %v", j.Progress)
	}

	// Finished work is untouched either way.
	if j := got[jobKey(JobAnalyse, "b.mkv")]; j.State != JobDone {
		t.Errorf("done job came back as %q", j.State)
	}
	if j := got[jobKey(JobAnalyse, "c.mkv")]; j.State != JobFailed || j.Error != "boom" {
		t.Errorf("failed job came back as %q / %q", j.State, j.Error)
	}
}

// Kind is part of the key. Analysis and preview are independent work on the
// same film and both can legitimately be outstanding; keying on the film alone
// silently drops one of them.
func TestJobKindsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	j := NewJobs("", dir, dir)

	j.mu.Lock()
	j.jobs[jobKey(JobAnalyse, "film.mkv")] = &Job{Kind: JobAnalyse, Film: "film.mkv", State: JobDone}
	j.jobs[jobKey(JobPrepare, "film.mkv")] = &Job{Kind: JobPrepare, Film: "film.mkv", State: JobRunning}
	j.mu.Unlock()

	got := j.Snapshot()
	if len(got) != 2 {
		t.Fatalf("two kinds of job on one film collapsed to %d", len(got))
	}
	if got[jobKey(JobAnalyse, "film.mkv")].State != JobDone {
		t.Error("analysis job was overwritten by the prepare job")
	}
	if got[jobKey(JobPrepare, "film.mkv")].State != JobRunning {
		t.Error("prepare job was overwritten by the analysis job")
	}
}

// A corrupt or truncated state file must not stop the studio starting. It is a
// convenience, not a source of truth, and losing it costs one queue.
func TestJobsToleratesRubbishState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".jobs.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := NewJobs("", dir, dir)
	if n := len(j.Snapshot()); n != 0 {
		t.Errorf("expected no jobs from a corrupt file, got %d", n)
	}
}

func TestPendingCountsOnlyLiveWork(t *testing.T) {
	dir := t.TempDir()
	j := NewJobs("", dir, dir)
	j.mu.Lock()
	j.jobs["1"] = &Job{State: JobQueued}
	j.jobs["2"] = &Job{State: JobRunning}
	j.jobs["3"] = &Job{State: JobDone}
	j.jobs["4"] = &Job{State: JobFailed}
	j.jobs["5"] = &Job{State: JobInterrupted}
	j.mu.Unlock()

	if got := j.Pending(); got != 2 {
		t.Errorf("Pending = %d, want 2", got)
	}
}
