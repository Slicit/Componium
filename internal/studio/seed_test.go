package studio

import (
	"os"
	"testing"
	"time"
)

func TestSeedKeepsAScoreThatPredatesTheHistory(t *testing.T) {
	// On the day history is switched on, every score in the library predates
	// it — and those are the ones whose behaviour prompted the work.
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	sc := made(time.Hour, curve("shake.seat", 0, 30*time.Minute))
	if err := sc.Save(j.ScorePath("film.mkv")); err != nil {
		t.Fatal(err)
	}

	if kept := j.SeedHistory([]string{"film.mkv"}); kept != 1 {
		t.Fatalf("seeded %d scores, wanted 1", kept)
	}
	versions := j.Versions("film.mkv")
	if len(versions) != 1 {
		t.Fatalf("history has %d versions", len(versions))
	}
	if versions[0].Note == "" {
		t.Error("the seeded version does not say where it came from")
	}
}

func TestSeedIsSafeToRunEveryStartup(t *testing.T) {
	// It runs on every start, so a second run must not pile up copies of the
	// same score.
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	sc := made(time.Hour, curve("a", 0, time.Minute))
	sc.Save(j.ScorePath("film.mkv"))

	j.SeedHistory([]string{"film.mkv"})
	if kept := j.SeedHistory([]string{"film.mkv"}); kept != 0 {
		t.Errorf("a second startup seeded %d more", kept)
	}
	if got := len(j.Versions("film.mkv")); got != 1 {
		t.Errorf("history grew to %d versions across two startups", got)
	}
}

func TestSeedSkipsAFilmWithNoScore(t *testing.T) {
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	if kept := j.SeedHistory([]string{"never-analysed.mkv"}); kept != 0 {
		t.Errorf("seeded %d scores for a film that has none", kept)
	}
}

func TestSeedSkipsAScoreItCannotRead(t *testing.T) {
	// Quietly: the library already shows the film as having no usable score,
	// and shouting about it on every startup helps nobody.
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	if err := os.WriteFile(j.ScorePath("broken.mkv"), []byte("not a score"), 0o644); err != nil {
		t.Fatal(err)
	}
	if kept := j.SeedHistory([]string{"broken.mkv"}); kept != 0 {
		t.Errorf("seeded %d unreadable scores", kept)
	}
}

func TestSeedLeavesAFilmThatAlreadyHasHistoryAlone(t *testing.T) {
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	sc := made(time.Hour, curve("a", 0, time.Minute))
	sc.Save(j.ScorePath("film.mkv"))
	j.Keep("film.mkv", sc, "an earlier run")

	if kept := j.SeedHistory([]string{"film.mkv"}); kept != 0 {
		t.Errorf("seeded %d over an existing history", kept)
	}
}

func TestCoversLimitReplansWhenTheLengthChanges(t *testing.T) {
	// A plan made for the whole film, now asked for fifteen minutes.
	whole := planChunks(853*mb, 124*time.Minute)
	if coversLimit(whole, 15*60) {
		t.Error("a two hour plan was accepted for a fifteen minute request")
	}
	// And the same request against a plan that already fits it.
	short := planChunks(853*mb, 15*time.Minute)
	if !coversLimit(short, 15*60) {
		t.Error("a fifteen minute plan was rejected for a fifteen minute request")
	}
}

func TestCoversLimitKeepsAPlanWhenNoLimitIsAsked(t *testing.T) {
	// Replanning here would throw away finished work every time somebody
	// pressed rebuild.
	whole := planChunks(853*mb, 124*time.Minute)
	if !coversLimit(whole, 0) {
		t.Error("an unlimited rebuild discarded an existing plan")
	}
}

func TestCoversLimitRejectsAnEmptyPlan(t *testing.T) {
	if coversLimit(nil, 900) {
		t.Error("no plan was treated as covering the request")
	}
}
