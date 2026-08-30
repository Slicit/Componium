package studio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Slicit/componium/internal/score"
)

// partial writes one chunk's score the way the composer would, so these tests
// exercise the real parser rather than a hand-built struct.
func partial(t *testing.T, j *Jobs, film string, index int, body string) {
	t.Helper()
	path := j.partialPath(film, index)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chunkScore(from, to string, points string) string {
	return `[score]
componium = "0.1"
title = "test"

[score.media]
duration = "00:20:00.000"

[[track]]
instrument = "shake.seat"
type = "curve"
interpolation = "linear"
points = [
` + points + `
]
`
}

func newJobs(t *testing.T) (*Jobs, string) {
	t.Helper()
	dir := t.TempDir()
	return &Jobs{scores: dir, jobs: map[string]*Job{}}, dir
}

func TestMergePartialsJoinsThePiecesInOrder(t *testing.T) {
	j, dir := newJobs(t)
	const film = "film.mkv"

	// Written out of order on purpose: the merge sorts by the numbering.
	partial(t, j, film, 1, chunkScore("", "",
		`  { t = "00:10:00.000", value = { intensity = 0.5000 } },
  { t = "00:12:00.000", value = { intensity = 0.5000 } },`))
	partial(t, j, film, 0, chunkScore("", "",
		`  { t = "00:00:00.000", value = { intensity = 0.1000 } },
  { t = "00:05:00.000", value = { intensity = 0.1000 } },`))
	partial(t, j, film, 2, chunkScore("", "",
		`  { t = "00:15:00.000", value = { intensity = 0.9000 } },
  { t = "00:20:00.000", value = { intensity = 0.9000 } },`))

	out := filepath.Join(dir, "film.componium")
	if err := j.mergePartials(film, out, "test"); err != nil {
		t.Fatal(err)
	}

	merged, err := score.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Tracks) != 1 {
		t.Fatalf("want one track, got %d", len(merged.Tracks))
	}
	pts := merged.Tracks[0].Points
	if len(pts) != 6 {
		t.Fatalf("want six points, two from each piece, got %d", len(pts))
	}
	if pts[0].Value["intensity"] != 0.1 || pts[len(pts)-1].Value["intensity"] != 0.9 {
		t.Errorf("the pieces came out in the wrong order: %v", pts)
	}
	for i := 1; i < len(pts); i++ {
		if pts[i].T < pts[i-1].T {
			t.Fatalf("point %d at %v comes before point %d at %v",
				i, pts[i].T, i-1, pts[i-1].T)
		}
	}
}

func TestMergePartialsNumbersPastNineSortCorrectly(t *testing.T) {
	// A feature is more than ten pieces, and "chunk-10" sorts before "chunk-2"
	// as a string. The fixed width numbering is what stops that, and this is
	// the test that says so.
	j, dir := newJobs(t)
	const film = "long.mkv"

	partial(t, j, film, 2, chunkScore("", "",
		`  { t = "00:02:00.000", value = { intensity = 0.2000 } },
  { t = "00:03:00.000", value = { intensity = 0.2000 } },`))
	partial(t, j, film, 10, chunkScore("", "",
		`  { t = "00:10:00.000", value = { intensity = 0.9000 } },
  { t = "00:11:00.000", value = { intensity = 0.9000 } },`))

	out := filepath.Join(dir, "long.componium")
	if err := j.mergePartials(film, out, "test"); err != nil {
		t.Fatal(err)
	}
	merged, _ := score.Load(out)
	pts := merged.Tracks[0].Points
	if pts[0].Value["intensity"] != 0.2 {
		t.Errorf("chunk 10 sorted before chunk 2: first point is %v", pts[0].Value)
	}
}

func TestMergePartialsClearsUpOnlyAfterWriting(t *testing.T) {
	j, dir := newJobs(t)
	const film = "film.mkv"
	partial(t, j, film, 0, chunkScore("", "",
		`  { t = "00:00:00.000", value = { intensity = 0.1000 } },
  { t = "00:05:00.000", value = { intensity = 0.1000 } },`))

	out := filepath.Join(dir, "film.componium")
	if err := j.mergePartials(film, out, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.partialDir(film)); !os.IsNotExist(err) {
		t.Error("the finished pieces were left behind after the score was written")
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("the score was not written: %v", err)
	}
}

func TestMergePartialsKeepsThePiecesWhenTheMergeFails(t *testing.T) {
	// The pieces are the only copy of the work until the score exists.
	// Throwing them away on a failure turns a bad write into a lost hour.
	j, _ := newJobs(t)
	const film = "film.mkv"
	partial(t, j, film, 0, chunkScore("", "",
		`  { t = "00:00:00.000", value = { intensity = 0.1000 } },
  { t = "00:05:00.000", value = { intensity = 0.1000 } },`))

	// A path that cannot be written, because its parent is a file.
	blocker := filepath.Join(j.scores, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := j.mergePartials(film, filepath.Join(blocker, "out.componium"), "test"); err == nil {
		t.Fatal("writing into a file should have failed")
	}
	if _, err := os.Stat(j.partialPath(film, 0)); err != nil {
		t.Error("the finished pieces were thrown away when the merge failed")
	}
}

func TestMergePartialsRefusesWhenThereIsNothingToJoin(t *testing.T) {
	j, dir := newJobs(t)
	err := j.mergePartials("never-analysed.mkv", filepath.Join(dir, "out.componium"), "test")
	if err == nil {
		t.Fatal("merging a film with no pieces should be an error")
	}
}

func TestResetThrowsAwayThePiecesAndTheState(t *testing.T) {
	j, _ := newJobs(t)
	const film = "film.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{
		Kind: JobAnalyse, Film: film, State: JobFailed, Progress: 0.6,
		Chunks: []Chunk{{Index: 0, State: JobDone}, {Index: 1, State: JobFailed}},
	}
	partial(t, j, film, 0, chunkScore("", "",
		`  { t = "00:00:00.000", value = { intensity = 0.1000 } },
  { t = "00:05:00.000", value = { intensity = 0.1000 } },`))

	if err := j.ResetAnalysis(film); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.partialDir(film)); !os.IsNotExist(err) {
		t.Error("reset left the finished pieces on disk")
	}
	job := j.jobs[jobKey(JobAnalyse, film)]
	if len(job.Chunks) != 0 {
		t.Errorf("reset left %d chunks of state behind", len(job.Chunks))
	}
	if job.Progress != 0 {
		t.Errorf("reset left the progress at %v", job.Progress)
	}
	if job.State == JobQueued {
		t.Error("reset marked the film queued; nothing is queued until it is asked for")
	}
}

func TestResetIsQuietAboutAFilmWithNothingToReset(t *testing.T) {
	j, _ := newJobs(t)
	if err := j.ResetAnalysis("never-touched.mkv"); err != nil {
		t.Errorf("resetting an untouched film should be a no-op, got %v", err)
	}
}

func TestChunkStateSurvivesBeingReadBack(t *testing.T) {
	// Chunk state is only worth anything if it outlives the process, and the
	// job file is how it does. This is the round trip.
	dir := t.TempDir()
	j := &Jobs{scores: dir, jobs: map[string]*Job{}}
	const film = "film.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{Kind: JobAnalyse, Film: film, State: JobRunning}
	j.setChunks(film, []Chunk{
		{Index: 0, From: 0, To: 900, State: JobDone, Seconds: 61.5},
		{Index: 1, From: 900, To: 1800, State: JobQueued},
	})

	back := &Jobs{scores: dir, jobs: map[string]*Job{}}
	back.load()

	got := back.chunksOf(film)
	if len(got) != 2 {
		t.Fatalf("read back %d chunks, wanted 2", len(got))
	}
	if got[0].State != JobDone || got[0].Seconds != 61.5 || got[0].To != 900 {
		t.Errorf("the finished chunk came back as %+v", got[0])
	}
	if resumeAt(got) != 0 {
		t.Errorf("resume from %d; with chunk 0 done and 1 queued it should step "+
			"back to 0", resumeAt(got))
	}
}

func TestMergeIgnoresTheObservationsBesideThePartials(t *testing.T) {
	/* The vision pass writes its observations into the same directory, and
	 * merging used to take every file in it — so a JSON lines file was handed
	 * to a TOML parser and a finished analysis failed at the last step with
	 * "expected '.' or '=', but got '{'". The work was all done by then. */
	j, dir := newJobs(t)
	const film = "film.mkv"
	partial(t, j, film, 0, chunkScore("", "",
		`  { t = "00:00:00.000", value = { intensity = 0.1000 } },
  { t = "00:05:00.000", value = { intensity = 0.1000 } },`))
	seen := j.partialPath(film, 0) + seenSuffix
	if err := os.WriteFile(seen, []byte(obsA), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "film.componium")
	if err := j.mergePartials(film, out, "test"); err != nil {
		t.Fatalf("the observations file broke the merge: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no score was written: %v", err)
	}
}
