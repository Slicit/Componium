// Package storetest is the contract every store implementation has to satisfy.
//
// It exists because an in-memory stand-in is only worth anything if it behaves
// like the thing it stands in for. Run against both, it is what lets six
// hundred other tests use the fast one without wondering whether they are
// testing a fiction.
//
// It is also the whole answer to "which tests does a storage change pull in".
// These, and nothing else.
package storetest

import (
	"context"
	"testing"

	"github.com/Slicit/componium/internal/store"
)

// Run puts a store through the contract. fresh must return an empty store.
func Run(t *testing.T, fresh func(t *testing.T) store.Store) {
	t.Helper()

	t.Run("an empty store knows nothing", func(t *testing.T) {
		s := fresh(t)
		got, err := s.Observations(context.Background(), "sintel")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("found %d observations in an empty store", len(got))
		}
		films, err := s.Films(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(films) != 0 {
			t.Errorf("found films: %v", films)
		}
	})

	t.Run("what goes in comes back", func(t *testing.T) {
		s := fresh(t)
		ctx := context.Background()
		in := []store.Observation{
			{Film: "sintel", At: 12.5, Place: "a forest", Doing: "walking",
				Seen: "a figure among trees", Labels: []string{"EFFECTS: none", "SCENE: forest"}},
			{Film: "sintel", At: 3.0, Place: "a cave", Seen: "torchlight"},
		}
		if err := s.SaveObservations(ctx, in); err != nil {
			t.Fatal(err)
		}
		got, err := s.Observations(ctx, "sintel")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d observations", len(got))
		}
		// In time order, because everything that reads these walks a film
		// forwards and the alternative is every caller sorting.
		if got[0].At != 3.0 || got[1].At != 12.5 {
			t.Fatalf("out of order: %v", []float64{got[0].At, got[1].At})
		}
		second := got[1]
		if second.Place != "a forest" || second.Doing != "walking" ||
			second.Seen != "a figure among trees" {
			t.Errorf("fields did not survive: %+v", second)
		}
		if len(second.Labels) != 2 || second.Labels[0] != "EFFECTS: none" {
			t.Errorf("labels did not survive: %v", second.Labels)
		}
	})

	t.Run("saving the same moment twice replaces it", func(t *testing.T) {
		/* The behaviour this schema exists for. Analysis is resumed, retried
		 * and re-run; observations that stacked instead of replacing turned
		 * 459 distinct moments into 3720 rows and needed a repair script. */
		s := fresh(t)
		ctx := context.Background()
		first := []store.Observation{{Film: "sintel", At: 10, Seen: "a cave"}}
		second := []store.Observation{{Film: "sintel", At: 10, Seen: "a forest"}}
		if err := s.SaveObservations(ctx, first); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveObservations(ctx, second); err != nil {
			t.Fatal(err)
		}
		got, err := s.Observations(ctx, "sintel")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("%d rows for one moment", len(got))
		}
		if got[0].Seen != "a forest" {
			t.Errorf("kept the older answer: %q", got[0].Seen)
		}
	})

	t.Run("films do not see each other", func(t *testing.T) {
		s := fresh(t)
		ctx := context.Background()
		err := s.SaveObservations(ctx, []store.Observation{
			{Film: "sintel", At: 1, Seen: "a"},
			{Film: "wanted", At: 1, Seen: "b"},
		})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := s.Observations(ctx, "sintel")
		if len(got) != 1 || got[0].Seen != "a" {
			t.Errorf("sintel got %v", got)
		}
		films, _ := s.Films(ctx)
		if len(films) != 2 || films[0] != "sintel" || films[1] != "wanted" {
			t.Errorf("films: %v", films)
		}
	})

	t.Run("forgetting one film leaves the others", func(t *testing.T) {
		s := fresh(t)
		ctx := context.Background()
		s.SaveObservations(ctx, []store.Observation{
			{Film: "sintel", At: 1, Seen: "a"},
			{Film: "wanted", At: 1, Seen: "b"},
		})
		if err := s.ForgetObservations(ctx, "sintel"); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Observations(ctx, "sintel"); len(got) != 0 {
			t.Errorf("sintel survived: %v", got)
		}
		if got, _ := s.Observations(ctx, "wanted"); len(got) != 1 {
			t.Errorf("wanted did not: %v", got)
		}
	})

	t.Run("forgetting a film that was never there is fine", func(t *testing.T) {
		// A rebuild of something never analysed is an ordinary thing to ask.
		s := fresh(t)
		if err := s.ForgetObservations(context.Background(), "nothing"); err != nil {
			t.Errorf("complained: %v", err)
		}
	})

	t.Run("saving nothing is fine", func(t *testing.T) {
		// A chunk the model had nothing to say about.
		s := fresh(t)
		if err := s.SaveObservations(context.Background(), nil); err != nil {
			t.Errorf("complained: %v", err)
		}
	})

	t.Run("an observation with nothing but a time", func(t *testing.T) {
		// Every text field is optional; the model often answers one of three.
		s := fresh(t)
		ctx := context.Background()
		if err := s.SaveObservations(ctx, []store.Observation{{Film: "f", At: 1}}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Observations(ctx, "f")
		if err != nil || len(got) != 1 {
			t.Fatalf("%v, %v", got, err)
		}
		if got[0].Place != "" || len(got[0].Labels) != 0 {
			t.Errorf("invented content: %+v", got[0])
		}
	})

	t.Run("a stored observation does not change underneath its writer", func(t *testing.T) {
		/* A caller that reuses a buffer between chunks, which the composer
		 * does, would otherwise find its earlier writes rewritten. Only the
		 * in-memory store can get this wrong, which is exactly why the
		 * contract has to ask. */
		s := fresh(t)
		ctx := context.Background()
		labels := []string{"EFFECTS: none"}
		if err := s.SaveObservations(ctx, []store.Observation{
			{Film: "f", At: 1, Seen: "first", Labels: labels},
		}); err != nil {
			t.Fatal(err)
		}
		labels[0] = "EFFECTS: fire"
		got, _ := s.Observations(ctx, "f")
		if len(got) != 1 || got[0].Labels[0] != "EFFECTS: none" {
			t.Errorf("the stored copy moved: %v", got)
		}
	})

	t.Run("a reachable store answers a ping", func(t *testing.T) {
		if err := fresh(t).Ping(context.Background()); err != nil {
			t.Errorf("ping: %v", err)
		}
	})
}
