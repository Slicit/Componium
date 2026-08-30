package score

import (
	"fmt"
	"sort"
	"time"
)

// Merge joins scores covering consecutive parts of one film into one score.
//
// This exists because analysing a feature is tens of minutes of work that is
// worth nothing until it finishes, so the studio cuts one into ranges it can
// record as they land. Each range produces a complete, valid score that
// happens to be short — its cues are already in the film's own time — which is
// what makes joining them mostly a matter of putting them in order.
//
// Mostly, but not entirely. Three things need deciding:
//
// Duplicates at a boundary. Each range is analysed with a lead in, so two
// neighbours can nominate the same event from the same evidence. A cue that
// matches one already kept, on the same instrument, at the same moment, is
// dropped rather than played twice.
//
// Calm across a boundary. A quiet stretch spanning two ranges arrives as two
// regions that meet, and a rig told to relax twice with a seam in the middle
// is being told something the film did not say. Touching regions are joined.
//
// The film's own length. Each partial records the whole film's duration rather
// than its own, so the merged score does not have to add up its pieces to
// discover how long the film is — which would be trusting the least reliable
// number available.
// trackKey is what makes two tracks the same track.
//
// The type is half of it. One instrument may legitimately have both a curve
// and cues — a light with an ambient wash under it and flashes on top is the
// ordinary case, and Cues() and Curves() are separate streams that the
// conductor drives independently. Keying on the name alone made merging refuse
// its own input: a single chunk of Sintel came back as "light.ambient is a
// curve track in one part and a cue track in another", describing two tracks
// that were in the same part and both belonged there.
type trackKey struct {
	instrument string
	kind       TrackType
}

func Merge(parts []*Score) (*Score, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("nothing to merge")
	}

	// The first part carries the metadata. Every part was made from the same
	// film by the same run, so they agree; taking one rather than reconciling
	// them keeps this from inventing a resolution for a disagreement that
	// cannot happen.
	out := &Score{Meta: parts[0].Meta}

	// Keyed by instrument AND type, because one instrument may legitimately
	// have both. A light with an ambient curve under it and flash events on
	// top is the ordinary case, not a mistake: Cues() and Curves() are
	// separate streams and the conductor drives both. Keying on the name alone
	// made merging refuse its own input — a single chunk of Sintel, whose
	// light.ambient has a wash and whose semantic cues flash the same fixture,
	// came back as "a curve track in one part and a cue track in another".
	byTrack := map[trackKey]*Track{}
	var order []trackKey
	for _, part := range parts {
		if part == nil {
			continue
		}
		if d := part.Meta.Media.Duration; d > out.Meta.Media.Duration {
			out.Meta.Media.Duration = d
		}
		for _, t := range part.Tracks {
			key := trackKey{t.Instrument, t.Type}
			into, seen := byTrack[key]
			if !seen {
				// Copy, so merging does not quietly rewrite one of its inputs.
				fresh := t
				fresh.Cues = append([]CueSpec(nil), t.Cues...)
				fresh.Points = append([]Point(nil), t.Points...)
				byTrack[key] = &fresh
				order = append(order, key)
				continue
			}
			into.Cues = append(into.Cues, t.Cues...)
			into.Points = append(into.Points, t.Points...)
		}
		out.Calm = append(out.Calm, part.Calm...)
	}

	// Sorted so the merged file reads the same way every time. The key sorts
	// by instrument first and then by type, which puts an instrument's curve
	// and its cues next to each other.
	sort.Slice(order, func(i, k int) bool {
		if order[i].instrument != order[k].instrument {
			return order[i].instrument < order[k].instrument
		}
		return order[i].kind < order[k].kind
	})
	for _, key := range order {
		t := byTrack[key]
		sort.SliceStable(t.Cues, func(i, j int) bool { return t.Cues[i].T < t.Cues[j].T })
		sort.SliceStable(t.Points, func(i, j int) bool { return t.Points[i].T < t.Points[j].T })
		t.Cues = dedupeCues(t.Cues)
		t.Points = dedupePoints(t.Points)
		out.Tracks = append(out.Tracks, *t)
	}

	out.Calm = joinRegions(out.Calm)
	return out, nil
}

// sameMoment is how close two nominations have to be to be the same one.
//
// A frame at 24fps is 41ms and the analysis samples at 4Hz, so anything inside
// a tenth of a second came from the same evidence rather than from two things
// that happened in quick succession.
const sameMoment = 100 * time.Millisecond

func dedupeCues(cues []CueSpec) []CueSpec {
	if len(cues) < 2 {
		return cues
	}
	out := cues[:1]
	for _, c := range cues[1:] {
		last := out[len(out)-1]
		if c.Action == last.Action && c.T-last.T < Timecode(sameMoment) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// dedupePoints drops a point landing on one already there.
//
// The boundary is the case: a range holds its value at its own start, and the
// range before it ends at that same moment. Two points at one time is not
// wrong so much as undecidable — the curve has to pick one — and the later one
// wins because it belongs to the range that was analysing that moment properly
// rather than holding a value carried in from before it.
func dedupePoints(points []Point) []Point {
	if len(points) < 2 {
		return points
	}
	out := points[:0:0]
	for i, p := range points {
		if i+1 < len(points) && points[i+1].T == p.T {
			continue
		}
		out = append(out, p)
	}
	return out
}

// joinRegions merges calm regions that touch or overlap.
func joinRegions(in []Region) []Region {
	if len(in) < 2 {
		return in
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].From < in[j].From })
	out := []Region{in[0]}
	for _, r := range in[1:] {
		last := &out[len(out)-1]
		// Touching counts as overlapping. Two ranges meeting exactly at a
		// boundary is the normal case here, not a coincidence.
		if r.From <= last.To {
			if r.To > last.To {
				last.To = r.To
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
