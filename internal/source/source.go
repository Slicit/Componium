// Package source adapts media players into something the clock can read.
//
// A source does as little as possible: it answers where the player is, and it
// says how precisely it can be asked. Everything about turning those answers
// into a usable clock lives in internal/clock, so that adding a player means
// writing one small adapter rather than reasoning about timing again.
package source

import "time"

// TimeSource is a media player that can be asked for its position.
//
// Implementations are not required to be safe for concurrent use. The show
// loop calls them from one goroutine.
type TimeSource interface {
	// Name identifies the player, for logs and for componium doctor.
	Name() string

	// Position returns where playback currently is. ok is false when the
	// player has no position to give, which is normal while idle, loading or
	// between files, and is not an error.
	//
	// An error means the connection itself is in trouble.
	Position() (pos time.Duration, ok bool, err error)

	// FrameInterval returns the content's frame period, which the clock needs
	// because every discontinuity threshold is expressed in multiples of it.
	// ok is false when the player cannot say, in which case the caller should
	// fall back to a default rather than guessing per source.
	FrameInterval() (interval time.Duration, ok bool)

	// Close releases the connection.
	Close() error
}
