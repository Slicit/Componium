package studio

import (
	"testing"
)

// These two exist because of the same fault seen from two sides, found by
// interrupting a real two hour analysis after two of its nine pieces had
// finished and then asking for it again. The pieces were on disk and the
// record said they were done — and queueing the film built a fresh job that
// dropped the record, so "resume" silently meant "start over".

func TestQueueingAFilmAgainKeepsWhatIsAlreadyFinished(t *testing.T) {
	j := &Jobs{jobs: map[string]*Job{}}
	const film = "feature.mkv"

	j.jobs[jobKey(JobAnalyse, film)] = &Job{
		Kind: JobAnalyse, Film: film, State: JobInterrupted,
		Chunks: []Chunk{
			{Index: 0, State: JobDone, Seconds: 147.2},
			{Index: 1, State: JobDone, Seconds: 151.7},
			{Index: 2, State: JobInterrupted},
		},
	}

	job := j.Enqueue(JobAnalyse, film)
	if len(job.Chunks) != 3 {
		t.Fatalf("queueing again left %d chunks; the record of finished work is gone",
			len(job.Chunks))
	}
	if job.Chunks[0].State != JobDone || job.Chunks[1].State != JobDone {
		t.Errorf("the finished pieces came back as %s and %s",
			job.Chunks[0].State, job.Chunks[1].State)
	}
	if got := resumeAt(job.Chunks); got != 1 {
		t.Errorf("resume from %d; with two done it should step back to 1", got)
	}
}

func TestQueueingAPrepareIsUnaffected(t *testing.T) {
	// Chunks belong to an analysis. A prepare has none and must not gain any.
	j := &Jobs{jobs: map[string]*Job{}}
	job := j.Enqueue(JobPrepare, "feature.mkv")
	if len(job.Chunks) != 0 {
		t.Errorf("a prepare came back with %d chunks", len(job.Chunks))
	}
}

func TestARestartLeavesNoChunkClaimingToBeRunning(t *testing.T) {
	dir := t.TempDir()
	j := &Jobs{scores: dir, jobs: map[string]*Job{}}
	const film = "feature.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{Kind: JobAnalyse, Film: film, State: JobRunning}
	j.setChunks(film, []Chunk{
		{Index: 0, State: JobDone},
		{Index: 1, State: JobRunning},
		{Index: 2, State: JobQueued},
	})

	back := &Jobs{scores: dir, jobs: map[string]*Job{}}
	back.load()

	got := back.chunksOf(film)
	if got[0].State != JobDone {
		t.Errorf("the finished piece came back as %s", got[0].State)
	}
	for _, c := range got[1:] {
		if c.State == JobRunning {
			t.Errorf("chunk %d still claims to be running after a restart", c.Index)
		}
	}
	// And it still resumes to the same place: anything not done is redone.
	if want := 0; resumeAt(got) != want {
		t.Errorf("resume from %d, wanted %d", resumeAt(got), want)
	}
}

func TestResetThenQueueStartsFromNothing(t *testing.T) {
	dir := t.TempDir()
	j := &Jobs{scores: dir, jobs: map[string]*Job{}}
	const film = "feature.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{
		Kind: JobAnalyse, Film: film, State: JobFailed,
		Chunks: []Chunk{{Index: 0, State: JobDone}, {Index: 1, State: JobFailed}},
	}

	if err := j.ResetAnalysis(film); err != nil {
		t.Fatal(err)
	}
	job := j.Enqueue(JobAnalyse, film)
	if len(job.Chunks) != 0 {
		t.Errorf("after a reset, queueing carried %d chunks forward", len(job.Chunks))
	}
}
