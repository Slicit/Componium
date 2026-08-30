package studio

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/score"
)

func made(duration time.Duration, tracks ...score.Track) *score.Score {
	return &score.Score{
		Meta: score.Meta{
			Componium: "0.1",
			Media:     score.Media{Duration: score.Timecode(duration)},
		},
		Tracks: tracks,
	}
}

func curve(name string, at ...time.Duration) score.Track {
	t := score.Track{Instrument: name, Type: score.TrackCurve, Interpolation: score.Linear}
	for _, d := range at {
		t.Points = append(t.Points, score.Point{
			T: score.Timecode(d), Value: map[string]float64{"intensity": 0.5}})
	}
	return t
}

func cues(name string, at ...time.Duration) score.Track {
	t := score.Track{Instrument: name, Type: score.TrackCue}
	for _, d := range at {
		t.Cues = append(t.Cues, score.CueSpec{T: score.Timecode(d), Action: "hit"})
	}
	return t
}

func TestKeepWritesAScoreAndItsNotes(t *testing.T) {
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	sc := made(2*time.Hour, curve("shake.seat", 0, time.Hour))

	v, err := j.Keep("film.mkv", sc, "vision on")
	if err != nil {
		t.Fatal(err)
	}
	if v.ID == "" {
		t.Fatal("no id")
	}
	for _, ext := range []string{".componium", ".json"} {
		if _, err := os.Stat(filepath.Join(j.historyDir("film.mkv"), v.ID+ext)); err != nil {
			t.Errorf("%s not written: %v", ext, err)
		}
	}
	if v.Note != "vision on" {
		t.Errorf("note came back %q", v.Note)
	}
}

func TestKeepDoesNotTouchTheLivePath(t *testing.T) {
	// The conductor, the player and the deploy all name the live path
	// directly. Keeping a version must be invisible to them.
	dir := t.TempDir()
	j := &Jobs{scores: dir, jobs: map[string]*Job{}}
	live := j.ScorePath("film.mkv")
	if err := os.WriteFile(live, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Keep("film.mkv", made(time.Hour, curve("a", 0, time.Minute)), ""); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(live)
	if string(body) != "original" {
		t.Errorf("the live score was rewritten: %q", body)
	}
}

func TestVersionsComeBackNewestFirst(t *testing.T) {
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	dir := j.historyDir("film.mkv")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20260101-120000", "20260830-090000", "20260315-235959"} {
		if err := os.WriteFile(filepath.Join(dir, id+".componium"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := j.Versions("film.mkv")
	if len(got) != 3 {
		t.Fatalf("want three versions, got %d", len(got))
	}
	if got[0].ID != "20260830-090000" {
		t.Errorf("newest is %s", got[0].ID)
	}
	if got[2].ID != "20260101-120000" {
		t.Errorf("oldest is %s", got[2].ID)
	}
}

func TestAVersionWithNoNotesIsStillListed(t *testing.T) {
	// A score you cannot read the notes for is still a score worth loading.
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	dir := j.historyDir("film.mkv")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "20260830-090000.componium"), []byte("x"), 0o644)

	if got := j.Versions("film.mkv"); len(got) != 1 {
		t.Fatalf("a version without a sidecar was dropped: %v", got)
	}
}

func TestAFilmWithNoHistoryListsNothing(t *testing.T) {
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	if got := j.Versions("never.mkv"); len(got) != 0 {
		t.Errorf("got %d versions for a film never analysed", len(got))
	}
}

func TestTwoVersionsInOneSecondBothSurvive(t *testing.T) {
	// The id is a timestamp to the second, and this exists precisely to stop
	// one run replacing another.
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	sc := made(time.Hour, curve("a", 0, time.Minute))
	first, err := j.Keep("film.mkv", sc, "one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := j.Keep("film.mkv", sc, "two")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("both runs took the id %s", first.ID)
	}
	if got := len(j.Versions("film.mkv")); got != 2 {
		t.Errorf("want two versions, got %d", got)
	}
}

func TestVersionPathRefusesAnIdItNeverWrote(t *testing.T) {
	// The id reaches this from a query string, so a path that climbs out of
	// the directory must not be expressible.
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	j.Keep("film.mkv", made(time.Hour, curve("a", 0, time.Minute)), "")

	for _, bad := range []string{"../../etc/passwd", "..", "nonsense", ""} {
		if _, ok := j.VersionPath("film.mkv", bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestVersionPathFindsOneItDidWrite(t *testing.T) {
	j := &Jobs{scores: t.TempDir(), jobs: map[string]*Job{}}
	v, _ := j.Keep("film.mkv", made(time.Hour, curve("a", 0, time.Minute)), "")
	path, ok := j.VersionPath("film.mkv", v.ID)
	if !ok {
		t.Fatal("could not find the version just written")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path does not exist: %v", err)
	}
}

func TestSummaryReportsCoverageOfAPartialScore(t *testing.T) {
	// Fifteen minutes of a two hour film: a legitimate thing to review, and
	// only reviewable fairly if the listing says that is what it is.
	sc := made(2*time.Hour, curve("shake.seat", 0, 15*time.Minute))
	v := summarise(sc, "id", "")
	if v.From != 0 || v.To != 900 {
		t.Errorf("coverage came out %v..%v", v.From, v.To)
	}
	if v.Duration != 7200 {
		t.Errorf("film duration came out %v", v.Duration)
	}
	if v.Complete {
		t.Error("fifteen minutes of a two hour film was called complete")
	}
}

func TestSummaryCallsAWholeFilmComplete(t *testing.T) {
	sc := made(2*time.Hour, curve("shake.seat", 0, 2*time.Hour-30*time.Second))
	if !summarise(sc, "id", "").Complete {
		t.Error("a score reaching the end was called partial")
	}
}

func TestSummaryCountsPerInstrument(t *testing.T) {
	// "shake went from 900 cues to 40" is the whole story of a change.
	sc := made(time.Hour,
		curve("light.ambient", 0, time.Minute, 2*time.Minute),
		cues("shake.seat", 10*time.Second, 20*time.Second))
	v := summarise(sc, "id", "")
	if v.Cues != 2 || v.Points != 3 {
		t.Errorf("totals came out cues=%d points=%d", v.Cues, v.Points)
	}
	if len(v.Tracks) != 2 || v.Tracks[0].Instrument != "light.ambient" {
		t.Errorf("tracks came out %v", v.Tracks)
	}
	if v.Tracks[1].Cues != 2 {
		t.Errorf("shake cue count came out %d", v.Tracks[1].Cues)
	}
}

func TestSummaryOfAnEmptyScoreDoesNotClaimCoverage(t *testing.T) {
	v := summarise(made(time.Hour), "id", "")
	if v.From != 0 || v.To != 0 {
		t.Errorf("an empty score claimed to cover %v..%v", v.From, v.To)
	}
}

func TestLabelSaysHowMuchOfTheFilmAPartialCovers(t *testing.T) {
	v := Version{ID: "20260830-142530", From: 0, To: 900, Duration: 7434, Cues: 12}
	label := v.Label()
	for _, want := range []string{"15m00s", "123m54s", "12 cues"} {
		if !containsSub(label, want) {
			t.Errorf("label %q does not mention %q", label, want)
		}
	}
}

func TestLabelOfACompleteScoreDoesNotNagAboutCoverage(t *testing.T) {
	v := Version{ID: "20260830-142530", From: 0, To: 7400, Duration: 7434, Cues: 94, Complete: true}
	if containsSub(v.Label(), " of ") {
		t.Errorf("a complete score was labelled with coverage: %q", v.Label())
	}
}

func containsSub(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
