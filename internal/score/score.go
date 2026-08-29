package score

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/Slicit/componium/internal/instrument"
)

// Version is the score format this build writes and understands.
const Version = "0.1"

// Score is one timeline bound to one piece of media.
type Score struct {
	Meta   Meta    `toml:"score"`
	Tracks []Track `toml:"track"`
}

// Meta identifies the score and the media it belongs to.
type Meta struct {
	Componium string `toml:"componium"`
	Title     string `toml:"title"`
	Media     Media  `toml:"media"`
}

// Media binds a score to content rather than to a filename, so that a score
// follows a film across rips and renames. That binding is what makes a shared
// score library possible.
type Media struct {
	Duration Timecode `toml:"duration"`
	Hash     string   `toml:"hash"`
	FPS      float64  `toml:"fps"`
}

// TrackType distinguishes the two shapes a track can take.
type TrackType string

const (
	// TrackCue holds discrete events.
	TrackCue TrackType = "cue"
	// TrackCurve holds a continuous channel, sampled and interpolated.
	TrackCurve TrackType = "curve"
)

// Interpolation is how values between curve points are found.
type Interpolation string

const (
	// Linear ramps between points.
	Linear Interpolation = "linear"
	// Step holds each point's value until the next one.
	Step Interpolation = "step"
)

// Track is one instrument's timeline.
type Track struct {
	Instrument    string        `toml:"instrument"`
	Type          TrackType     `toml:"type"`
	Interpolation Interpolation `toml:"interpolation"`
	Cues          []CueSpec     `toml:"cues"`
	Points        []Point       `toml:"points"`
}

// CueSpec is one discrete event in a cue track.
type CueSpec struct {
	T                 Timecode           `toml:"t"`
	Action            string             `toml:"action"`
	Params            map[string]float64 `toml:"params"`
	Duration          Span               `toml:"duration"`
	RequiredPrecision Span               `toml:"required_precision"`
}

// Point is one sample in a curve track.
type Point struct {
	T     Timecode           `toml:"t"`
	Value map[string]float64 `toml:"value"`
}

// Load reads and validates a score file.
func Load(path string) (*Score, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse reads and validates a score from bytes.
func Parse(b []byte) (*Score, error) {
	var s Score
	if _, err := toml.Decode(string(b), &s); err != nil {
		return nil, fmt.Errorf("score: %w", err)
	}
	if err := s.normalise(); err != nil {
		return nil, err
	}
	return &s, nil
}

// normalise applies defaults, sorts every track by time, and rejects anything
// that cannot be played.
//
// Sorting here rather than trusting the file means a hand edited score with a
// cue inserted in the wrong place still plays correctly, which is the common
// case and a miserable thing to debug.
func (s *Score) normalise() error {
	if s.Meta.Componium == "" {
		return fmt.Errorf("score: missing componium version field")
	}
	if s.Meta.Componium != Version {
		return fmt.Errorf("score: version %q, this build understands %q", s.Meta.Componium, Version)
	}
	if len(s.Tracks) == 0 {
		return fmt.Errorf("score: no tracks")
	}

	for i := range s.Tracks {
		t := &s.Tracks[i]
		if t.Instrument == "" {
			return fmt.Errorf("score: track %d has no instrument", i)
		}
		if t.Type == "" {
			// Infer, since it is unambiguous from the contents.
			if len(t.Points) > 0 {
				t.Type = TrackCurve
			} else {
				t.Type = TrackCue
			}
		}
		switch t.Type {
		case TrackCue:
			if len(t.Points) > 0 {
				return fmt.Errorf("score: track %q is a cue track but has curve points", t.Instrument)
			}
			sort.SliceStable(t.Cues, func(a, b int) bool { return t.Cues[a].T < t.Cues[b].T })
			for j, c := range t.Cues {
				if c.T < 0 {
					return fmt.Errorf("score: track %q cue %d is before the start of the media", t.Instrument, j)
				}
				if c.Action == "" {
					return fmt.Errorf("score: track %q cue at %s has no action", t.Instrument, c.T)
				}
			}
		case TrackCurve:
			if len(t.Cues) > 0 {
				return fmt.Errorf("score: track %q is a curve track but has cues", t.Instrument)
			}
			if len(t.Points) < 2 {
				return fmt.Errorf("score: track %q is a curve with fewer than two points", t.Instrument)
			}
			if t.Interpolation == "" {
				t.Interpolation = Linear
			}
			if t.Interpolation != Linear && t.Interpolation != Step {
				return fmt.Errorf("score: track %q has unknown interpolation %q", t.Instrument, t.Interpolation)
			}
			sort.SliceStable(t.Points, func(a, b int) bool { return t.Points[a].T < t.Points[b].T })
		default:
			return fmt.Errorf("score: track %q has unknown type %q", t.Instrument, t.Type)
		}
	}
	return nil
}

// Cues flattens every cue track into the form the conductor loads.
func (s *Score) Cues() []instrument.Cue {
	var out []instrument.Cue
	for _, t := range s.Tracks {
		if t.Type != TrackCue {
			continue
		}
		for _, c := range t.Cues {
			out = append(out, instrument.Cue{
				At:                c.T.Duration(),
				Instrument:        t.Instrument,
				Action:            c.Action,
				Params:            c.Params,
				RequiredPrecision: c.RequiredPrecision.Duration(),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Curves returns the curve tracks, ready to be sampled.
func (s *Score) Curves() []Track {
	var out []Track
	for _, t := range s.Tracks {
		if t.Type == TrackCurve {
			out = append(out, t)
		}
	}
	return out
}

// Instruments lists every instrument the score addresses.
func (s *Score) Instruments() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range s.Tracks {
		if !seen[t.Instrument] {
			seen[t.Instrument] = true
			out = append(out, t.Instrument)
		}
	}
	sort.Strings(out)
	return out
}

// Save writes a score back to disk.
//
// Round tripping matters more than it looks: the studio loads a score, a
// person edits it, and it is written back. Anything the writer drops is
// silently destroyed, which is why the round trip has a test of its own.
func (s *Score) Save(path string) error {
	b, err := s.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Marshal renders a score as TOML.
func (s *Score) Marshal() ([]byte, error) {
	if err := s.normalise(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("# Componium score.\n\n")
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("score: %w", err)
	}
	return buf.Bytes(), nil
}
