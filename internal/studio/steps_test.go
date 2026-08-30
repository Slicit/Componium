package studio

import (
	"testing"
	"time"
)

func stepping(t *testing.T) (*Jobs, string) {
	t.Helper()
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	const film = "film.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{Kind: JobAnalyse, Film: film, State: JobRunning}
	return j, film
}

func TestEachStepIsTimedWhenTheNextBegins(t *testing.T) {
	// A step's duration is only known when the next one starts, so recording
	// the start of each is enough to time all of them and nothing has to
	// remember to stop.
	j, film := stepping(t)
	j.beginStep(film, "preparing")
	time.Sleep(12 * time.Millisecond)
	j.beginStep(film, "analysing")

	steps := j.stepsOf(film)
	if len(steps) != 2 {
		t.Fatalf("want two steps, got %d", len(steps))
	}
	if steps[0].Seconds <= 0 {
		t.Error("the first step was never timed")
	}
	if steps[1].Seconds != 0 {
		t.Error("the running step was timed before it finished")
	}
}

func TestTheLastStepIsTimedWhenTheRunEnds(t *testing.T) {
	j, film := stepping(t)
	j.beginStep(film, "finding the quiet parts")
	time.Sleep(12 * time.Millisecond)
	j.endSteps(film, "", "half the film")

	steps := j.stepsOf(film)
	if steps[0].Seconds <= 0 {
		t.Error("the last step was never timed")
	}
	if steps[0].Note != "half the film" {
		t.Errorf("the note came back %q", steps[0].Note)
	}
}

func TestEveryStepRecordsWhenItBegan(t *testing.T) {
	// So a person reading the list can see where a run stalled rather than
	// only how long it took in total.
	j, film := stepping(t)
	j.beginStep(film, "preparing")
	steps := j.stepsOf(film)
	if _, err := time.Parse(time.RFC3339, steps[0].Started); err != nil {
		t.Errorf("start time is %q: %v", steps[0].Started, err)
	}
}

func TestAFailedStepSaysSo(t *testing.T) {
	j, film := stepping(t)
	j.beginStep(film, "measuring the audio")
	j.endSteps(film, JobFailed, "ffmpeg went away")

	steps := j.stepsOf(film)
	if steps[0].State != JobFailed {
		t.Errorf("state came back %q", steps[0].State)
	}
	if steps[0].Note != "ffmpeg went away" {
		t.Errorf("note came back %q", steps[0].Note)
	}
}

func TestClosingTwiceDoesNotRewriteADuration(t *testing.T) {
	j, film := stepping(t)
	j.beginStep(film, "one")
	time.Sleep(10 * time.Millisecond)
	j.endSteps(film, "", "")
	first := j.stepsOf(film)[0].Seconds
	time.Sleep(10 * time.Millisecond)
	j.endSteps(film, "", "")
	if got := j.stepsOf(film)[0].Seconds; got != first {
		t.Errorf("a closed step was re-timed: %v then %v", first, got)
	}
}

func TestANoteLandsOnTheRunningStep(t *testing.T) {
	j, film := stepping(t)
	j.beginStep(film, "preparing")
	j.noteStep(film, "reusing what the model already saw")
	j.beginStep(film, "analysing")

	steps := j.stepsOf(film)
	if steps[0].Note != "reusing what the model already saw" {
		t.Errorf("the note landed on %q", steps[0].Note)
	}
	if steps[1].Note != "" {
		t.Error("the note leaked onto the next step")
	}
}

func TestStartingOverClearsTheRecord(t *testing.T) {
	j, film := stepping(t)
	j.beginStep(film, "one")
	j.beginStep(film, "two")
	j.clearSteps(film)
	if got := len(j.stepsOf(film)); got != 0 {
		t.Errorf("a fresh run began with %d steps already recorded", got)
	}
}

func TestElapsedAddsThemUp(t *testing.T) {
	steps := []Step{{Seconds: 1.5}, {Seconds: 2.25}, {Seconds: 0.125}}
	if got := Elapsed(steps); got != 3.875 {
		t.Errorf("elapsed came out %v", got)
	}
	if got := Elapsed(nil); got != 0 {
		t.Errorf("elapsed of nothing came out %v", got)
	}
}

func TestStepsSurviveBeingReadBack(t *testing.T) {
	// They are persisted with the job, so an interrupted run still says how
	// far it got and what it spent getting there.
	dir := t.TempDir()
	j := &Jobs{scores: dir, jobs: map[string]*Job{}}
	const film = "film.mkv"
	j.jobs[jobKey(JobAnalyse, film)] = &Job{Kind: JobAnalyse, Film: film, State: JobRunning}
	j.beginStep(film, "analysing 1 of 9")
	j.endSteps(film, "", "")

	back := &Jobs{scores: dir, jobs: map[string]*Job{}}
	back.load()
	steps := back.stepsOf(film)
	if len(steps) != 1 || steps[0].Name != "analysing 1 of 9" {
		t.Fatalf("read back %+v", steps)
	}
}

func TestAKeptVersionRemembersWhatTheRunCost(t *testing.T) {
	// The job is overwritten by the next run, and "why was that one slower"
	// is asked afterwards.
	j, film := stepping(t)
	j.beginStep(film, "analysing")
	j.endSteps(film, "", "")

	v, err := j.Keep(film, made(time.Hour, curve("a", 0, time.Minute)), "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Steps) != 1 {
		t.Fatalf("the version kept %d steps", len(v.Steps))
	}

	back := j.Versions(film)
	if len(back) != 1 || len(back[0].Steps) != 1 {
		t.Fatalf("the steps did not survive being read back: %+v", back)
	}
}
