package studio

import (
	"os"
	"strings"
	"testing"
)

// What a film is, kept beside its score.
//
// Small enough to be obvious and worth testing anyway, because the empty case
// is load-bearing: a film with nothing said about it must be analysed exactly
// as it was before this existed, and "" and "no file" have to mean the same
// thing at every level or a rebuild quietly changes what the model is told.

func TestContextRoundTrips(t *testing.T) {
	j, _ := newJobs(t)
	const film = "film.mkv"

	if got := j.ReadContext(film); got != "" {
		t.Errorf("a film nobody has described came back with %q", got)
	}
	if err := j.WriteContext(film, "Space opera. Soldiers, ships, a moon."); err != nil {
		t.Fatal(err)
	}
	if got := j.ReadContext(film); got != "Space opera. Soldiers, ships, a moon." {
		t.Errorf("read back %q", got)
	}
}

func TestContextIsTrimmed(t *testing.T) {
	// It ends up in a prompt. Leading blank lines there are noise the model
	// has to read past.
	j, _ := newJobs(t)
	const film = "film.mkv"
	if err := j.WriteContext(film, "\n\n  Space opera.  \n\n"); err != nil {
		t.Fatal(err)
	}
	if got := j.ReadContext(film); got != "Space opera." {
		t.Errorf("read back %q", got)
	}
}

func TestClearingContextLeavesNoFile(t *testing.T) {
	// An empty file and no file must mean the same thing to ReadContext, and
	// only one of them is honest about there being nothing there.
	j, _ := newJobs(t)
	const film = "film.mkv"
	if err := j.WriteContext(film, "Space opera."); err != nil {
		t.Fatal(err)
	}
	if err := j.WriteContext(film, "   "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.ContextPath(film)); !os.IsNotExist(err) {
		t.Error("clearing the context left a file behind")
	}
	if got := j.ReadContext(film); got != "" {
		t.Errorf("cleared context read back as %q", got)
	}
}

func TestClearingSomethingThatWasNeverThere(t *testing.T) {
	j, _ := newJobs(t)
	if err := j.WriteContext("film.mkv", ""); err != nil {
		t.Errorf("clearing nothing was an error: %v", err)
	}
}

func TestContextIsCapped(t *testing.T) {
	// The thing on the other end is being asked about a frame, not told a
	// story. A synopsis that runs to pages would crowd out the frame itself.
	j, _ := newJobs(t)
	const film = "film.mkv"
	if err := j.WriteContext(film, strings.Repeat("a", contextLimit*2)); err != nil {
		t.Fatal(err)
	}
	if got := len(j.ReadContext(film)); got > contextLimit {
		t.Errorf("kept %d characters, cap is %d", got, contextLimit)
	}
}

func TestContextSitsBesideTheScore(t *testing.T) {
	j, _ := newJobs(t)
	const film = "film.mkv"
	if got := j.ContextPath(film); got != j.ScorePath(film)+".context" {
		t.Errorf("context path is %q", got)
	}
}
