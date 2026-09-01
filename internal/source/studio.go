package source

import (
	"sync"
	"time"
)

// A player that is a browser tab.
//
// The studio's playhead lives in the page and the rig lives in the server, so
// something has to carry one to the other. This is that: the page reports where
// it is, and this answers the show loop's question as any other player would.
//
// It is a TimeSource and nothing more, which is the point. Everything about
// turning a jittery external position into a usable clock already exists for
// mpv, and none of it needed to be thought about again: clock discipline,
// latency compensation, the curve driver, the safety supervisor. The studio
// needed a source, not a timing stack.
//
// Two properties matter more than the rest.
//
// It interpolates between reports. The page reports at the film's frame rate,
// around 24 a second, and the show loop asks 200 times a second; answering with
// a position that only moves 24 times a second would quantise every cue to a
// frame boundary in the wrong direction. A real player's position advances
// between polls, and so does this.
//
// It goes quiet when nobody is reporting. A browser tab can be closed, put to
// sleep or driven into a tunnel, and none of those look different from here.
// After Stale with no word, this stops claiming to know where playback is,
// which stops the conductor dispatching. What to do about that is the caller's
// decision and a deliberate one; see the studio's live controller.
type Studio struct {
	// Stale is how long a report is trusted for. Zero means DefaultStale.
	Stale time.Duration

	// Now is injectable so the whole thing can be tested against a clock that
	// does not have to actually pass.
	Now func() time.Time

	mu      sync.Mutex
	at      time.Duration
	when    time.Time
	playing bool
	frame   time.Duration
	got     bool
}

// DefaultStale is generous against a browser and mean against a fan.
//
// The page reports every frame while playing and a few times a second while
// paused, so a second of silence is already dozens of missed reports. It is not
// shorter because a garbage collection or a busy laptop should not stop a show,
// and not longer because this is how long a fan keeps running after the tab
// that was driving it has gone.
const DefaultStale = time.Second

func (s *Studio) Name() string { return "studio" }

// Report records where the page says it is.
func (s *Studio) Report(at time.Duration, playing bool, frame time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at < 0 {
		at = 0
	}
	s.at, s.playing, s.when, s.got = at, playing, s.now(), true
	if frame > 0 {
		s.frame = frame
	}
}

// Forget drops the position, as though nothing had ever reported.
func (s *Studio) Forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got, s.playing = false, false
}

func (s *Studio) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Studio) stale() time.Duration {
	if s.Stale > 0 {
		return s.Stale
	}
	return DefaultStale
}

// Silent reports whether nobody has said anything recently.
func (s *Studio) Silent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.got || s.now().Sub(s.when) > s.stale()
}

// Position answers where playback is, interpolating since the last report.
func (s *Studio) Position() (time.Duration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.got {
		return 0, false, nil
	}
	since := s.now().Sub(s.when)
	if since > s.stale() {
		// Not an error. A player with nothing to say is an ordinary state and
		// the show loop already knows what to do with it.
		return 0, false, nil
	}
	if !s.playing {
		return s.at, true, nil
	}
	if since < 0 {
		since = 0
	}
	return s.at + since, true, nil
}

// FrameInterval is the film's, as the page reported it.
func (s *Studio) FrameInterval() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frame, s.frame > 0
}

// Close is here to satisfy the interface. There is no connection to release:
// the page reports over the same HTTP the studio already serves.
func (s *Studio) Close() error { return nil }
