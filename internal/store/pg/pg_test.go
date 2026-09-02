package pg

import (
	"context"
	"os"
	"testing"

	"github.com/Slicit/componium/internal/store"
	"github.com/Slicit/componium/internal/store/storetest"
)

// The real one, against a real database, and only when there is one.
//
// Skipped rather than failed without COMPONIUM_TEST_DB, because the point of
// the two implementations is that `go test ./...` keeps working on a laptop
// with nothing installed. CI sets it; a contributor does not have to.
func TestContract(t *testing.T) {
	url := os.Getenv("COMPONIUM_TEST_DB")
	if url == "" {
		t.Skip("set COMPONIUM_TEST_DB to run the contract against Postgres")
	}
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
	url := os.Getenv("COMPONIUM_TEST_DB")
	if url == "" {
		t.Skip("set COMPONIUM_TEST_DB")
	}
	// Opening twice is what happens every time the studio restarts.
	for i := 0; i < 3; i++ {
		s, err := Open(context.Background(), url)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}
