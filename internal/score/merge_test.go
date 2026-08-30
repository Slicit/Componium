package score

import (
	"testing"
	"time"
)

func tc(d time.Duration) Timecode { return Timecode(d) }

func curveTrack(name string, points ...Point) Track {
	return Track{Instrument: name, Type: TrackCurve, Interpolation: Linear, Points: points}
}

func cueTrack(name string, cues ...CueSpec) Track {
	return Track{Instrument: name, Type: TrackCue, Cues: cues}
}

func at(d time.Duration, v float64) Point {
	return Point{T: tc(d), Value: map[string]float64{"intensity": v}}
}

func TestMergeRefusesNothing(t *testing.T) {
	if _, err := Merge(nil); err == nil {
		t.Fatal("merging nothing should be an error, not an empty score")
	}
}

func TestMergeJoinsOneTrackAcrossParts(t *testing.T) {
	a := &Score{Tracks: []Track{curveTrack("shake.seat", at(0, 0.1), at(10*time.Second, 0.2))}}
	b := &Score{Tracks: []Track{curveTrack("shake.seat", at(20*time.Second, 0.3))}}

	out, err := Merge([]*Score{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tracks) != 1 {
		t.Fatalf("want one track, got %d", len(out.Tracks))
	}
	if got := len(out.Tracks[0].Points); got != 3 {
		t.Fatalf("want three points, got %d", got)
	}
	if out.Tracks[0].Points[2].T != tc(20*time.Second) {
		t.Errorf("last point is at %v", out.Tracks[0].Points[2].T)
	}
}

func TestMergeKeepsPartsInTimeOrderWhateverOrderTheyArriveIn(t *testing.T) {
	late := &Score{Tracks: []Track{curveTrack("a", at(30*time.Second, 0.9))}}
	early := &Score{Tracks: []Track{curveTrack("a", at(1*time.Second, 0.1))}}

	out, err := Merge([]*Score{late, early})
	if err != nil {
		t.Fatal(err)
	}
	pts := out.Tracks[0].Points
	if pts[0].T != tc(1*time.Second) || pts[1].T != tc(30*time.Second) {
		t.Errorf("points came out unsorted: %v then %v", pts[0].T, pts[1].T)
	}
}

func TestMergeCollectsSeparateInstruments(t *testing.T) {
	a := &Score{Tracks: []Track{curveTrack("shake.seat", at(0, 0.1))}}
	b := &Score{Tracks: []Track{curveTrack("wind.main", at(10*time.Second, 0.2))}}

	out, _ := Merge([]*Score{a, b})
	if len(out.Tracks) != 2 {
		t.Fatalf("want two tracks, got %d", len(out.Tracks))
	}
	// Sorted by name, so the merged file reads the same way every time.
	if out.Tracks[0].Instrument != "shake.seat" || out.Tracks[1].Instrument != "wind.main" {
		t.Errorf("tracks are not in a stable order: %s then %s",
			out.Tracks[0].Instrument, out.Tracks[1].Instrument)
	}
}

func TestMergeDropsAPointDuplicatedAtABoundary(t *testing.T) {
	// The range before ends at 20s; the range after holds its value at 20s.
	a := &Score{Tracks: []Track{curveTrack("a", at(10*time.Second, 0.2), at(20*time.Second, 0.5))}}
	b := &Score{Tracks: []Track{curveTrack("a", at(20*time.Second, 0.7), at(30*time.Second, 0.9))}}

	out, _ := Merge([]*Score{a, b})
	pts := out.Tracks[0].Points
	if len(pts) != 3 {
		t.Fatalf("want three points, got %d", len(pts))
	}
	// The later part wins: it was analysing that moment rather than carrying a
	// value in from before it.
	if pts[1].Value["intensity"] != 0.7 {
		t.Errorf("the boundary point kept %v, wanted the later part's 0.7",
			pts[1].Value["intensity"])
	}
}

func TestMergeDropsACueNominatedTwiceFromTheSameEvidence(t *testing.T) {
	// A lead in means two neighbouring ranges can see the same event.
	gust := CueSpec{T: tc(20 * time.Second), Action: "gust"}
	a := &Score{Tracks: []Track{cueTrack("wind.main", gust)}}
	b := &Score{Tracks: []Track{cueTrack("wind.main", gust)}}

	out, _ := Merge([]*Score{a, b})
	if got := len(out.Tracks[0].Cues); got != 1 {
		t.Fatalf("the same gust was kept %d times", got)
	}
}

func TestMergeKeepsTwoDifferentActionsAtOneMoment(t *testing.T) {
	a := &Score{Tracks: []Track{cueTrack("light.event",
		CueSpec{T: tc(20 * time.Second), Action: "flash"})}}
	b := &Score{Tracks: []Track{cueTrack("light.event",
		CueSpec{T: tc(20 * time.Second), Action: "strobe"})}}

	out, _ := Merge([]*Score{a, b})
	if got := len(out.Tracks[0].Cues); got != 2 {
		t.Fatalf("two different actions at one moment collapsed to %d", got)
	}
}

func TestMergeKeepsTwoOfTheSameActionFarEnoughApart(t *testing.T) {
	a := &Score{Tracks: []Track{cueTrack("wind.main",
		CueSpec{T: tc(20 * time.Second), Action: "gust"},
		CueSpec{T: tc(21 * time.Second), Action: "gust"})}}

	out, _ := Merge([]*Score{a})
	if got := len(out.Tracks[0].Cues); got != 2 {
		t.Fatalf("two gusts a second apart collapsed to %d", got)
	}
}

func TestMergeJoinsCalmAcrossABoundary(t *testing.T) {
	a := &Score{Calm: []Region{{From: tc(10 * time.Second), To: tc(30 * time.Second)}}}
	b := &Score{Calm: []Region{{From: tc(30 * time.Second), To: tc(50 * time.Second)}}}

	out, _ := Merge([]*Score{a, b})
	if len(out.Calm) != 1 {
		t.Fatalf("a calm stretch across a boundary came out as %d regions", len(out.Calm))
	}
	if out.Calm[0].From != tc(10*time.Second) || out.Calm[0].To != tc(50*time.Second) {
		t.Errorf("joined region is %v..%v", out.Calm[0].From, out.Calm[0].To)
	}
}

func TestMergeLeavesSeparateCalmStretchesSeparate(t *testing.T) {
	a := &Score{Calm: []Region{{From: tc(10 * time.Second), To: tc(20 * time.Second)}}}
	b := &Score{Calm: []Region{{From: tc(40 * time.Second), To: tc(50 * time.Second)}}}

	out, _ := Merge([]*Score{a, b})
	if len(out.Calm) != 2 {
		t.Fatalf("two unrelated calm stretches became %d", len(out.Calm))
	}
}

func TestMergeTakesTheFilmsDurationNotTheSumOfItsParts(t *testing.T) {
	whole := tc(90 * time.Minute)
	a := &Score{Meta: Meta{Media: Media{Duration: whole}},
		Tracks: []Track{curveTrack("a", at(0, 0.1))}}
	b := &Score{Meta: Meta{Media: Media{Duration: whole}},
		Tracks: []Track{curveTrack("a", at(60*time.Minute, 0.2))}}

	out, _ := Merge([]*Score{a, b})
	if out.Meta.Media.Duration != whole {
		t.Errorf("duration came out %v, wanted the film's %v",
			out.Meta.Media.Duration, whole)
	}
}

func TestMergeKeepsACurveAndCuesForOneInstrumentApart(t *testing.T) {
	// One instrument may legitimately have both. A light with an ambient wash
	// under it and flashes on top is the ordinary case — Cues() and Curves()
	// are separate streams the conductor drives independently — and refusing
	// it made merging reject its own input on a single chunk of Sintel.
	a := &Score{Tracks: []Track{
		curveTrack("light.ambient", at(0, 0.1), at(10*time.Second, 0.4)),
		cueTrack("light.ambient", CueSpec{T: tc(5 * time.Second), Action: "flash"}),
	}}
	b := &Score{Tracks: []Track{
		curveTrack("light.ambient", at(20*time.Second, 0.9)),
		cueTrack("light.ambient", CueSpec{T: tc(25 * time.Second), Action: "flash"}),
	}}

	out, err := Merge([]*Score{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tracks) != 2 {
		t.Fatalf("want a curve and a cue track, got %d tracks", len(out.Tracks))
	}

	var curve, cue *Track
	for i := range out.Tracks {
		if out.Tracks[i].Type == TrackCurve {
			curve = &out.Tracks[i]
		} else {
			cue = &out.Tracks[i]
		}
	}
	if curve == nil || cue == nil {
		t.Fatalf("one of the two kinds is missing: %+v", out.Tracks)
	}
	if len(curve.Points) != 3 {
		t.Errorf("the curve has %d points across two parts", len(curve.Points))
	}
	if len(cue.Cues) != 2 {
		t.Errorf("the cue track has %d cues across two parts", len(cue.Cues))
	}
}

func TestMergeDoesNotMixOneInstrumentsCuesIntoAnothers(t *testing.T) {
	a := &Score{Tracks: []Track{
		cueTrack("light.ambient", CueSpec{T: tc(time.Second), Action: "flash"}),
		cueTrack("light.event", CueSpec{T: tc(2 * time.Second), Action: "flash"}),
	}}
	out, _ := Merge([]*Score{a})
	if len(out.Tracks) != 2 {
		t.Fatalf("two instruments collapsed into %d tracks", len(out.Tracks))
	}
	for _, tr := range out.Tracks {
		if len(tr.Cues) != 1 {
			t.Errorf("%s ended up with %d cues", tr.Instrument, len(tr.Cues))
		}
	}
}

func TestMergeDoesNotRewriteItsInputs(t *testing.T) {
	a := &Score{Tracks: []Track{curveTrack("a", at(0, 0.1))}}
	b := &Score{Tracks: []Track{curveTrack("a", at(10*time.Second, 0.2))}}

	Merge([]*Score{a, b})
	if got := len(a.Tracks[0].Points); got != 1 {
		t.Errorf("merging appended to one of its inputs: it now has %d points", got)
	}
}

func TestMergeSurvivesANilPart(t *testing.T) {
	a := &Score{Tracks: []Track{curveTrack("a", at(0, 0.1))}}
	out, err := Merge([]*Score{a, nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tracks) != 1 {
		t.Errorf("want one track, got %d", len(out.Tracks))
	}
}
