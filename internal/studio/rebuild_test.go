package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Rebuilding a film that finished cleanly.
//
// Reported as a rebuild failing with something about a folder. The shape of it:
// a successful analysis deletes its partials, because they are working files
// and the score is the product — but it leaves the chunk record saying every
// chunk is done. Press Rebuild, which does not reset, and the run has nothing
// to do: every chunk is already done, so none is run, and the merge is handed
// a directory that was tidied away when the last run succeeded.

func TestRebuildAfterASuccessfulRun(t *testing.T) {
	j, dir := newJobs(t)
	const film = "film.mkv"

	// The state a finished analysis leaves: chunks done, partials gone.
	j.jobs[jobKey(JobAnalyse, film)] = &Job{
		Kind: JobAnalyse, Film: film, State: JobDone,
		Chunks: []Chunk{{Index: 0, From: 0, To: 100, State: JobDone}},
	}
	if _, err := os.Stat(j.partialDir(film)); !os.IsNotExist(err) {
		t.Fatal("the fixture should start with no partials")
	}

	// Nothing left to run, which is what a rebuild discovers.
	if got := resumeAt(j.chunksOf(film)); got != 1 {
		t.Fatalf("resume from %d; with the only chunk done it is past the end", got)
	}

	// And then the merge is asked for pieces that are not there.
	err := j.mergePartials(film, filepath.Join(dir, "film.componium"), "test")
	if err == nil {
		t.Fatal("expected the merge to fail, since there is nothing to merge")
	}
	if !strings.Contains(err.Error(), "reading the finished pieces") {
		t.Fatalf("failed with %q, which is not the error users are seeing", err)
	}
}

func TestRebuildingStartsOver(t *testing.T) {
	// The fix: a rebuild of a film whose chunks are all done starts the plan
	// again rather than finding nothing to do. Resetting is still how you
	// throw away a description; this is only about the chunks.
	j, _ := newJobs(t)
	const film = "film.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{
		Kind: JobAnalyse, Film: film, State: JobDone,
		Chunks: []Chunk{
			{Index: 0, From: 0, To: 100, State: JobDone},
			{Index: 1, From: 100, To: 200, State: JobDone},
		},
	}

	j.replanIfFinished(film)

	chunks := j.chunksOf(film)
	if len(chunks) != 0 {
		t.Fatalf("a finished plan survived a rebuild: %+v", chunks)
	}
}

func TestAnUnfinishedPlanIsLeftAloneToResume(t *testing.T) {
	// The whole point of the chunk record is that an interrupted run is
	// continued rather than repeated. Only a plan with nothing left to do is
	// thrown away.
	j, _ := newJobs(t)
	const film = "film.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{
		Kind: JobAnalyse, Film: film, State: JobInterrupted,
		Chunks: []Chunk{
			{Index: 0, From: 0, To: 100, State: JobDone},
			{Index: 1, From: 100, To: 200, State: JobInterrupted},
		},
	}

	j.replanIfFinished(film)

	if got := len(j.chunksOf(film)); got != 2 {
		t.Fatalf("an unfinished plan was thrown away: %d chunks left", got)
	}
}
