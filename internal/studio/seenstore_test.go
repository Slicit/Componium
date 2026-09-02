package studio

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Slicit/componium/internal/store"
	"github.com/Slicit/componium/internal/store/mem"
)

// Observations with a database and without one.
//
// Both are supported ways to run, which is the whole degradation story: a score
// is a file, so a studio with no database opens, edits and saves as it always
// did, and only what is derived goes somewhere less queryable. Both paths need
// testing or one of them rots.

const chunkOne = `{"t":1.0,"seen":"a cave","labels":["EFFECTS: none"]}
{"t":2.0,"seen":"torchlight","place":"a cave","doing":"walking"}
`

const chunkTwo = `{"t":10.0,"seen":"a forest"}
`

// seenJobs returns a Jobs writing into a temp scores directory, with the
// chunk files a run would have left behind.
func seenJobs(t *testing.T, st store.Store) (*Jobs, string) {
	t.Helper()
	scores := t.TempDir()
	j := NewJobs("", scores, "")
	if st != nil {
		j.SetStore(st)
	}
	const film = "sintel.mkv"
	dir := j.partialDir(film)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"0000" + seenSuffix: chunkOne,
		"0001" + seenSuffix: chunkTwo,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return j, film
}

func TestJoiningChunksIntoADatabase(t *testing.T) {
	st := mem.New()
	j, film := seenJobs(t, st)

	n, err := j.mergeSeen(film, j.ScorePath(film))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("joined %d observations", n)
	}

	// Keyed by the base name, not the container. An import reading
	// sintel.componium.seen.jsonl cannot know whether the film was an mkv or
	// an mp4, so both sides have to agree to forget.
	if FilmKey(film) != "sintel" {
		t.Fatalf("film key is %q", FilmKey(film))
	}
	got, err := st.Observations(context.Background(), FilmKey(film))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d in the store", len(got))
	}
	// In time order and across chunks, which is what the join is for.
	if got[0].At != 1 || got[2].At != 10 {
		t.Errorf("out of order: %v", []float64{got[0].At, got[1].At, got[2].At})
	}
	// And the fields the composer used to drop at this boundary.
	if got[1].Place != "a cave" || got[1].Doing != "walking" {
		t.Errorf("place and doing were lost: %+v", got[1])
	}
	if len(got[0].Labels) != 1 || got[0].Labels[0] != "EFFECTS: none" {
		t.Errorf("labels were lost: %v", got[0].Labels)
	}
}

func TestWithADatabaseNoFileIsWritten(t *testing.T) {
	// One or the other, never both, or the studio grows a second opinion about
	// what a model said and no way to tell which is older.
	st := mem.New()
	j, film := seenJobs(t, st)
	if _, err := j.mergeSeen(film, j.ScorePath(film)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.SeenPath(film)); !os.IsNotExist(err) {
		t.Errorf("wrote the file as well: %v", err)
	}
}

func TestWithoutADatabaseItIsStillAFile(t *testing.T) {
	j, film := seenJobs(t, nil)
	n, err := j.mergeSeen(film, j.ScorePath(film))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("joined %d", n)
	}
	if _, err := os.Stat(j.SeenPath(film)); err != nil {
		t.Fatalf("no file: %v", err)
	}
	got, err := j.ReadSeen(film)
	if err != nil || len(got) != 3 {
		t.Fatalf("%d observations, %v", len(got), err)
	}
}

func TestReadingBackThroughEitherPath(t *testing.T) {
	// The same three observations, the same shape, whichever way they were
	// kept. Anything that reads them should not be able to tell.
	for _, kept := range []struct {
		name  string
		store store.Store
	}{
		{"in a database", mem.New()},
		{"in a file", nil},
	} {
		t.Run(kept.name, func(t *testing.T) {
			j, film := seenJobs(t, kept.store)
			if _, err := j.mergeSeen(film, j.ScorePath(film)); err != nil {
				t.Fatal(err)
			}
			got, err := j.ReadSeen(film)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 {
				t.Fatalf("%d observations", len(got))
			}
			if got[0].T != 1 || got[2].T != 10 || got[2].Seen != "a forest" {
				t.Errorf("came back as %+v", got)
			}
			if !j.HasSeen(film) {
				t.Error("HasSeen said no")
			}
		})
	}
}

func TestAFilmNobodyLookedAt(t *testing.T) {
	for _, st := range []store.Store{mem.New(), nil} {
		j, _ := seenJobs(t, st)
		if j.HasSeen("never-analysed.mkv") {
			t.Error("claimed observations for a film with none")
		}
	}
}

func TestAHalfWrittenLineDoesNotLoseTheRest(t *testing.T) {
	/* The file is written a line at a time by something that can be
	 * interrupted, so a truncated last line is an ordinary way for a chunk to
	 * end and is not a reason to lose the several thousand above it. */
	st := mem.New()
	j, film := seenJobs(t, st)
	dir := j.partialDir(film)
	if err := os.WriteFile(filepath.Join(dir, "0002"+seenSuffix),
		[]byte("{\"t\":20.0,\"seen\":\"cut off"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := j.mergeSeen(film, j.ScorePath(film))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("kept %d, want the three whole ones", n)
	}
}
