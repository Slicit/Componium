package studio

import (
	"os"
	"testing"
)

// Reading back what the model said.
//
// The file is written a line at a time by something that can be interrupted, so
// the shapes that matter are the damaged ones: a half written last line, a
// blank, a line that is not JSON at all. None of those is a reason to refuse
// the thousands of good lines above them, because the whole point of keeping
// the description is that it survives the run that made it.

func writeSeen(t *testing.T, j *Jobs, film, body string) {
	t.Helper()
	if err := os.WriteFile(j.SeenPath(film), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSeen(t *testing.T) {
	j, _ := newJobs(t)
	const film = "film.mkv"
	writeSeen(t, j, film, `{"t":3,"labels":["water","scene-calm"],"seen":"A beach."}
{"t":1,"labels":["dust"],"seen":"Sand thrown up."}
`)

	got, err := j.ReadSeen(film)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d observations, want 2", len(got))
	}
	// In time order, whatever order the chunks happened to be joined in.
	if got[0].T != 1 || got[1].T != 3 {
		t.Errorf("out of order: %v", []float64{got[0].T, got[1].T})
	}
	if got[0].Seen != "Sand thrown up." {
		t.Errorf("lost the sentence: %q", got[0].Seen)
	}
	if len(got[1].Labels) != 2 || got[1].Labels[0] != "water" {
		t.Errorf("lost the labels: %v", got[1].Labels)
	}
}

func TestReadSeenSurvivesAnInterruptedWrite(t *testing.T) {
	j, _ := newJobs(t)
	const film = "film.mkv"
	// A last line cut in half, which is what an interrupted run leaves.
	writeSeen(t, j, film, `{"t":1,"labels":["dust"],"seen":"One."}

{"t":2,"labels":["fire"],"seen":"Two."}
{"t":3,"labels":["wa`)

	got, err := j.ReadSeen(film)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d observations, want the 2 whole ones", len(got))
	}
}

func TestReadSeenOnAFilmNobodyHasLookedAt(t *testing.T) {
	j, _ := newJobs(t)
	if _, err := j.ReadSeen("never.mkv"); err == nil {
		t.Error("expected an error for a film with no description")
	}
}

func TestHasSeen(t *testing.T) {
	j, _ := newJobs(t)
	const film = "film.mkv"

	if j.HasSeen(film) {
		t.Error("said there was a description before one was written")
	}
	writeSeen(t, j, film, `{"t":1,"labels":["dust"]}`+"\n")
	if !j.HasSeen(film) {
		t.Error("did not see the description that is there")
	}
}

func TestHasSeenIgnoresAnEmptyFile(t *testing.T) {
	// A run that created the file and then found nothing to say leaves this.
	// Offering to review an empty description is offering a blank page.
	j, _ := newJobs(t)
	const film = "film.mkv"
	writeSeen(t, j, film, "")
	if j.HasSeen(film) {
		t.Error("an empty description counted as a description")
	}
}
