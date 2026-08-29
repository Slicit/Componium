// Package score reads and writes Componium scores: timelines of cues and
// curves bound to one piece of media.
//
// A score carries two kinds of track, and the distinction is the reason the
// format exists at all. Cue tracks hold discrete events, a fog burst or a
// thunder flash. Curve tracks hold continuous channels, sway or wind speed or
// colour, which cannot be expressed as a list of moments without either
// lying or exploding in size.
package score

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Timecode is a position in the media, written as HH:MM:SS.mmm, MM:SS.mmm, or
// a plain Go duration like 90s.
//
// Scores are hand edited by people who think in timecode, so the format has to
// accept what they will actually type.
type Timecode time.Duration

// ParseTimecode accepts "01:04:22.100", "04:22.1", "22", or "1h4m22s".
func ParseTimecode(s string) (Timecode, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty timecode")
	}
	if !strings.Contains(s, ":") {
		// Either a Go duration or a bare number of seconds.
		if d, err := time.ParseDuration(s); err == nil {
			return Timecode(d), nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("timecode %q: not HH:MM:SS.mmm, a duration, or seconds", s)
		}
		return Timecode(f * float64(time.Second)), nil
	}

	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("timecode %q: too many colons", s)
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, fmt.Errorf("timecode %q: %q is not a number", s, p)
		}
		if v < 0 {
			return 0, fmt.Errorf("timecode %q: negative component", s)
		}
		total = total*60 + v
	}
	return Timecode(total * float64(time.Second)), nil
}

// UnmarshalText lets TOML decode a timecode directly.
func (t *Timecode) UnmarshalText(b []byte) error {
	v, err := ParseTimecode(string(b))
	if err != nil {
		return err
	}
	*t = v
	return nil
}

// MarshalText writes the canonical HH:MM:SS.mmm form.
func (t Timecode) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

func (t Timecode) String() string {
	d := time.Duration(t)
	neg := d < 0
	if neg {
		d = -d
	}
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	ms := (d - s*time.Second) / time.Millisecond
	out := fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
	if neg {
		return "-" + out
	}
	return out
}

// Duration converts to the type the rest of Componium uses.
func (t Timecode) Duration() time.Duration { return time.Duration(t) }

// Span is a length of time, written as a Go duration such as "4s" or "250ms".
type Span time.Duration

func (s *Span) UnmarshalText(b []byte) error {
	d, err := time.ParseDuration(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("duration %q: %w", b, err)
	}
	*s = Span(d)
	return nil
}

func (s Span) MarshalText() ([]byte, error) { return []byte(time.Duration(s).String()), nil }

func (s Span) Duration() time.Duration { return time.Duration(s) }
