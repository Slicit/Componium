package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seenFile(t *testing.T, j *Jobs, film string, index int, lines ...string) {
	t.Helper()
	path := j.partialPath(film, index) + seenSuffix
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const obsA = `{"t":10.0,"labels":["smoke"],"seen":"Smoke drifts across a courtyard."}`
const obsB = `{"t":950.0,"labels":["dust"],"seen":"A ship lands, kicking up dust."}`
const obsC = `{"t":1900.0,"labels":[],"seen":"Two people talking in a kitchen."}`

func TestSeenTravelsWithTheScore(t *testing.T) {
	j, dir := newJobs(t)
	const film = "film.mkv"
	seenFile(t, j, film, 0, obsA)
	out := filepath.Join(dir, "film.componium")

	n, err := j.mergeSeen(film, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("merged %d observations, wanted 1", n)
	}
	body, err := os.ReadFile(out + seenSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "courtyard") {
		t.Errorf("the sentence did not survive: %q", body)
	}
}

func TestSeenIsJoinedInChunkOrder(t *testing.T) {
	// A feature is more than ten pieces, and chunk-10 sorts before chunk-2 as
	// a string. The fixed width numbering is what stops that.
	j, dir := newJobs(t)
	const film = "film.mkv"
	seenFile(t, j, film, 2, obsB)
	seenFile(t, j, film, 10, obsC)
	seenFile(t, j, film, 0, obsA)

	out := filepath.Join(dir, "film.componium")
	if _, err := j.mergeSeen(film, out); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(out + seenSuffix)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want three lines, got %d", len(lines))
	}
	for i, want := range []string{"courtyard", "kicking up dust", "kitchen"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d is %q, wanted the one about %q", i, lines[i], want)
		}
	}
}

func TestSeenKeepsAFrameThatSawNothing(t *testing.T) {
	// "nothing here" is a fact about the film, and a later pass looking for
	// calm wants it as much as it wants the explosions.
	j, dir := newJobs(t)
	const film = "film.mkv"
	seenFile(t, j, film, 0, obsC)
	out := filepath.Join(dir, "film.componium")
	n, _ := j.mergeSeen(film, out)
	if n != 1 {
		t.Errorf("an observation with no labels was dropped")
	}
}

func TestSeenSkipsBlankLines(t *testing.T) {
	// A partial file is a valid one, and one interrupted mid-write may end
	// anywhere.
	j, dir := newJobs(t)
	const film = "film.mkv"
	seenFile(t, j, film, 0, obsA, "", "   ", obsB)
	out := filepath.Join(dir, "film.componium")
	n, _ := j.mergeSeen(film, out)
	if n != 2 {
		t.Errorf("counted %d observations, wanted 2", n)
	}
}

func TestSeenWritesNothingWhenThereIsNothing(t *testing.T) {
	// Analysis without a vision model is the normal case, and it must not
	// leave an empty file implying a pass that never ran.
	j, dir := newJobs(t)
	const film = "film.mkv"
	if err := os.MkdirAll(j.partialDir(film), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "film.componium")
	n, err := j.mergeSeen(film, out)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(out + seenSuffix); !os.IsNotExist(err) {
		t.Error("an empty observations file was written")
	}
}

func TestSeenPathSitsBesideTheScore(t *testing.T) {
	j := &Jobs{scores: "/scores"}
	got := filepath.ToSlash(j.SeenPath("Wanted.2008.BluRay.mkv"))
	want := "/scores/Wanted.2008.BluRay.componium.seen.jsonl"
	if got != want {
		t.Errorf("observations path is %q, wanted %q", got, want)
	}
}
