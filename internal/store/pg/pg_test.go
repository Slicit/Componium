package pg

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Slicit/componium/internal/store"
	"github.com/Slicit/componium/internal/store/storetest"
)

// The real one, against a real database, and only when there is one.
//
// Skipped rather than failed without COMPONIUM_TEST_DB, because the point of
// the two implementations is that `go test ./...` keeps working on a laptop
// with nothing installed. CI sets it; a contributor does not have to.
// testDatabase returns the URL to test against, or skips.
//
// It empties whatever it is given, because every subtest has to start from
// nothing or they read each other's films. That makes this variable a loaded
// gun, so the name has to say it is a test database: pasting the wrong URL is
// otherwise one keystroke from deleting an afternoon of analysis. Rebuildable
// is not the same as free.
func testDatabase(t *testing.T) string {
	t.Helper()
	url := os.Getenv("COMPONIUM_TEST_DB")
	if url == "" {
		t.Skip("set COMPONIUM_TEST_DB to run the contract against Postgres")
	}
	if !strings.Contains(url, "test") {
		t.Fatalf("refusing to empty %q: this deletes every observation in the "+
			"database, so its name has to contain \"test\"", url)
	}
	return url
}

func TestContract(t *testing.T) {
	url := testDatabase(t)
	storetest.Run(t, func(t *testing.T) store.Store {
		s, err := Open(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		// Every subtest starts from nothing, or they read each other's films.
		films, err := s.Films(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, film := range films {
			if err := s.ForgetObservations(context.Background(), film); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

func TestNoUrlIsNotAnError(t *testing.T) {
	// A studio with no database still opens, edits and saves a score, because
	// those are files. Telling "not configured" apart from "broken" is the
	// difference between a degraded studio and one somebody thinks is broken.
	_, err := Open(context.Background(), "")
	if err != store.ErrNoStore {
		t.Errorf("got %v, want ErrNoStore", err)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	url := testDatabase(t)
	// Opening twice is what happens every time the studio restarts.
	for i := 0; i < 3; i++ {
		s, err := Open(context.Background(), url)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}
