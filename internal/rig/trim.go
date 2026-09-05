package rig

import (
	"sync"

	"github.com/Slicit/componium/internal/colour"
	"github.com/Slicit/componium/internal/instrument"
)

// Trimming what reaches a fixture, per instrument.
//
// Here rather than in the studio, and that is the whole point of where it
// lives. A trim compensates for a strip, so a show has to have it too: a
// correction that existed only in the studio would make a room look right in
// preview and wrong the moment it played, which is a worse state than not
// having the feature.
//
// So the rig carries it, `componium play` gets it by reading the file, and the
// studio moves it while a film plays by setting it here.

// trimmed wraps one instrument so that what reaches it is adjusted.
//
// A stop passes through untouched, and that is the safety property. The
// supervisor sits outside this and forces a fixture safe by dispatching an
// action of safe, which IsStop covers, so what it sends is never trimmed:
// a blackout stays a blackout however the sliders are set, and a light that
// could be brightened back on by a slider left at plus eighty would be a
// light that cannot be turned off.
//
// Worth being exact about, because the protection is the passthrough rather
// than the order of the wrappers. The rig wraps first and the supervisor
// wraps around it, so a safe travels through here on its way out.
type trimmed struct {
	inner instrument.Instrument
	id    string
	of    func(string) colour.Trim
}

func (t trimmed) Manifest() instrument.Manifest { return t.inner.Manifest() }

func (t trimmed) Dispatch(d instrument.Dispatch) error {
	if instrument.IsStop(d.Cue.Action) {
		return t.inner.Dispatch(d)
	}
	d.Cue.Params = t.of(t.id).Apply(d.Cue.Params)
	return t.inner.Dispatch(d)
}

var _ instrument.Instrument = trimmed{}

// trims is what each instrument is currently being trimmed by.
//
// Read per cue rather than captured when the rig is built, so that the studio
// can move a slider while a film plays and see the strip change. An instrument
// nobody has touched is absent rather than stored as a pair of zeroes.
type trims struct {
	mu sync.Mutex
	by map[string]colour.Trim
}

func (t *trims) get(id string) colour.Trim {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.by[id]
}

func (t *trims) set(id string, v colour.Trim) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if v.Zero() {
		delete(t.by, id)
		return
	}
	if t.by == nil {
		t.by = map[string]colour.Trim{}
	}
	t.by[id] = v
}

func (t *trims) all() map[string]colour.Trim {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]colour.Trim, len(t.by))
	for k, v := range t.by {
		out[k] = v
	}
	return out
}

// Trim is what one instrument is currently being adjusted by.
func (b *Built) Trim(id string) colour.Trim { return b.trims.get(id) }

// Trims is every adjustment currently in force.
func (b *Built) Trims() map[string]colour.Trim { return b.trims.all() }

// SetTrim adjusts one instrument from now on.
//
// Takes effect on the next cue, which is what makes it a knob rather than a
// setting: the studio moves these while a film is playing and the room answers.
func (b *Built) SetTrim(id string, t colour.Trim) {
	b.trims.set(id, colour.Trim{
		Brightness: colour.Clamp(t.Brightness),
		Saturation: colour.Clamp(t.Saturation),
	})
}
