// Package virtual provides an instrument that does nothing physical and
// records everything it was told.
//
// This is not a test fixture. Most people who want to work on Componium will
// never own a motion platform, and every instrument kind is expected to have a
// virtual implementation so that the whole system can be run, developed and
// demonstrated with no hardware at all.
package virtual

import (
	"sync"

	"github.com/Slicit/componium/internal/instrument"
)

// Instrument records dispatches instead of acting on them.
type Instrument struct {
	manifest instrument.Manifest

	mu        sync.Mutex
	received  []instrument.Dispatch
	failWith  error
	failAfter int
}

// New returns a virtual instrument answering to the given manifest.
func New(m instrument.Manifest) *Instrument {
	return &Instrument{manifest: m}
}

// FailAfter makes the instrument return err on every dispatch after the first
// n, so that failure handling can be exercised.
func (i *Instrument) FailAfter(n int, err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.failAfter = n
	i.failWith = err
}

func (i *Instrument) Manifest() instrument.Manifest { return i.manifest }

func (i *Instrument) Dispatch(d instrument.Dispatch) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failWith != nil && len(i.received) >= i.failAfter {
		return i.failWith
	}
	i.received = append(i.received, d)
	return nil
}

// Received returns a copy of everything dispatched to this instrument.
func (i *Instrument) Received() []instrument.Dispatch {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]instrument.Dispatch, len(i.received))
	copy(out, i.received)
	return out
}

// Count returns how many dispatches were received.
func (i *Instrument) Count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.received)
}
