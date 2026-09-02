// Package store is where derived data lives.
//
// The split it exists to serve is ADR 0006: Postgres holds what the system
// derives, files hold what a person authored. So a score and a rig are not
// here and will not be. What is here is the analysis: observations a model
// wrote about a film, the queue that produced them, and in time the index of
// kept scores and the measurements from experiments. Everything in this
// package can be deleted and regenerated, which is the property that makes it
// safe to keep in a service that might be down.
//
// Two implementations from the first commit, and that is not architectural
// tidiness. Six hundred tests currently run with nothing installed, and an
// in-memory store is what keeps it that way: only the contract tests in
// storetest need a database, so a change to storage pulls those in and nothing
// else has to care.
package store

import (
	"context"
	"errors"
)

// ErrNoStore is what a caller gets when no database was configured.
//
// A distinct error rather than a nil store, because the studio has to keep
// working without one: a score is a file, so it opens, edits and saves as it
// always did, and only the parts that need derived data go quiet. Telling
// those two apart is the difference between a degraded studio and a broken
// one.
var ErrNoStore = errors.New("store: no database configured")

// An Observation is what a model said about one moment of a film.
//
// Time is seconds into the film, and it is film time rather than chunk time.
// That distinction is not pedantic: a chunk starting an hour in that reports
// its own clock files every observation under the opening minutes, which is a
// bug this project has had, and the primary key here is what makes it
// impossible to have twice.
type Observation struct {
	Film   string   `json:"film"`
	At     float64  `json:"t"`
	Place  string   `json:"place,omitempty"`
	Doing  string   `json:"doing,omitempty"`
	Seen   string   `json:"seen,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// Store is what the studio and the composer need from a database.
//
// Deliberately small and deliberately not generic. A repository interface with
// twenty methods nobody calls is a second schema to keep in step with the
// first; this grows a method when something needs one.
type Store interface {
	// Ping reports whether the database is reachable, for a studio that has to
	// say which of its features are available.
	Ping(ctx context.Context) error

	// SaveObservations writes observations, replacing any already recorded at
	// the same moment of the same film.
	//
	// Replacing rather than appending is the whole point. Analysis is resumed,
	// retried and re-run, and an append-only history of what the model said
	// the third time is not something anybody wants to read.
	SaveObservations(ctx context.Context, obs []Observation) error

	// Observations returns everything known about a film, in time order.
	Observations(ctx context.Context, film string) ([]Observation, error)

	// ForgetObservations drops a film's observations, for a rebuild that is
	// meant to start from nothing.
	ForgetObservations(ctx context.Context, film string) error

	// Films lists the films with observations, alphabetically.
	Films(ctx context.Context) ([]string, error)

	Close() error
}
