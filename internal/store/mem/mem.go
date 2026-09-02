package mem

import (
	"context"
	"sort"
	"sync"

	"github.com/Slicit/componium/internal/store"
)

// An in-memory store, for tests and for nothing else.
//
// It exists so that the six hundred tests that touch data keep running with
// nothing installed. That is worth a hundred lines: a suite that needs a
// service is a suite people stop running locally, and a suite people stop
// running locally is a suite that stops being true.
//
// It is held to the same contract as Postgres, by the same tests, which is the
// only thing that makes it trustworthy as a stand-in.
type Store struct {
	mu sync.Mutex
	// by film, then by the moment observed, because replacing an observation
	// at a moment already seen is the behaviour that matters most.
	obs map[string]map[float64]store.Observation
}

func New() *Store {
	return &Store{obs: map[string]map[float64]store.Observation{}}
}

func (s *Store) Ping(context.Context) error { return nil }

func (s *Store) SaveObservations(_ context.Context, obs []store.Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range obs {
		film := s.obs[o.Film]
		if film == nil {
			film = map[float64]store.Observation{}
			s.obs[o.Film] = film
		}
		// Copied, and the slice with it: a caller that reuses its buffer
		// between chunks would otherwise find its earlier writes changing
		// underneath it, which is the sort of bug that only appears at scale.
		kept := o
		kept.Labels = append([]string(nil), o.Labels...)
		film[o.At] = kept
	}
	return nil
}

func (s *Store) Observations(_ context.Context, film string) ([]store.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.Observation, 0, len(s.obs[film]))
	for _, o := range s.obs[film] {
		o.Labels = append([]string(nil), o.Labels...)
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}

func (s *Store) ForgetObservations(_ context.Context, film string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.obs, film)
	return nil
}

func (s *Store) Films(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.obs))
	for film := range s.obs {
		out = append(out, film)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) Close() error { return nil }

var _ store.Store = (*Store)(nil)
