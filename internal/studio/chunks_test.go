package studio

import (
	"path/filepath"
	"testing"
	"time"
)

const mb = 1 << 20

func TestPlanCoversTheWholeFilmWithNoGaps(t *testing.T) {
	chunks := planChunks(853*mb, 124*time.Minute)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if chunks[0].From != 0 {
		t.Errorf("first chunk starts at %v, not the start of the film", chunks[0].From)
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i].From != chunks[i-1].To {
			t.Fatalf("chunk %d starts at %v but %d ended at %v — a gap",
				i, chunks[i].From, i-1, chunks[i-1].To)
		}
	}
	want := (124 * time.Minute).Seconds()
	if got := chunks[len(chunks)-1].To; got != want {
		t.Errorf("the last chunk ends at %v, the film ends at %v", got, want)
	}
}

func TestPlanSizesFromBytesNotMinutes(t *testing.T) {
	// Rebel Moon: 853MB over 2h04 is about 15 minutes to the 100MB.
	sparse := planChunks(853*mb, 124*time.Minute)
	// Wanted: 7.6GB over 1h50 is about 90 seconds to the 100MB, so the clamp
	// takes over and gives the minimum.
	dense := planChunks(7622*mb, 110*time.Minute)

	if len(dense) <= len(sparse) {
		t.Errorf("the file ten times the bitrate got %d chunks and the other %d; "+
			"sizing is not following the bytes", len(dense), len(sparse))
	}
}

func TestPlanHoldsChunksToASensibleLength(t *testing.T) {
	// A film so dense that the byte target asks for ninety second chunks.
	dense := planChunks(20000*mb, 110*time.Minute)
	for _, c := range dense {
		if d := c.Duration(); d < minChunk-time.Second && c.Index != len(dense)-1 {
			t.Fatalf("chunk %d is %v, shorter than the floor of %v", c.Index, d, minChunk)
		}
	}

	// And one so sparse the target would ask for the whole film at once.
	sparse := planChunks(10*mb, 180*time.Minute)
	for _, c := range sparse {
		if d := c.Duration(); d > maxChunk+time.Second {
			t.Fatalf("chunk %d is %v, longer than the ceiling of %v", c.Index, d, maxChunk)
		}
	}
}

func TestPlanGivesAShortFilmOneChunk(t *testing.T) {
	chunks := planChunks(60*mb, 75*time.Second)
	if len(chunks) != 1 {
		t.Fatalf("a 75 second film came out as %d chunks", len(chunks))
	}
	if chunks[0].From != 0 || chunks[0].To != 75 {
		t.Errorf("the one chunk covers %v..%v, not the whole film", chunks[0].From, chunks[0].To)
	}
}

func TestPlanRefusesAFilmOfNoLength(t *testing.T) {
	// Rather than one chunk of nothing, which would be run and would fail.
	if got := planChunks(60*mb, 0); got != nil {
		t.Errorf("a film of no duration produced %d chunks", len(got))
	}
}

func TestPlanSurvivesAnUnknownFileSize(t *testing.T) {
	// ffprobe answers and stat does not, which is rare and must not divide by
	// zero. Falling back to the longest sensible chunk is the safe answer.
	chunks := planChunks(0, 60*time.Minute)
	if len(chunks) != 3 {
		t.Fatalf("want three twenty minute chunks, got %d", len(chunks))
	}
}

func TestPlanStartsEveryChunkQueued(t *testing.T) {
	for _, c := range planChunks(853*mb, 124*time.Minute) {
		if c.State != JobQueued {
			t.Fatalf("chunk %d starts as %q", c.Index, c.State)
		}
	}
}

func TestResumeStartsAtTheBeginningWhenNothingIsDone(t *testing.T) {
	chunks := planChunks(853*mb, 124*time.Minute)
	if got := resumeAt(chunks); got != 0 {
		t.Errorf("resume from %d, wanted 0", got)
	}
}

func TestResumeStepsBackOneFromTheFirstUnfinishedChunk(t *testing.T) {
	chunks := planChunks(853*mb, 124*time.Minute)
	for i := 0; i < 4; i++ {
		chunks[i].State = JobDone
	}
	chunks[4].State = JobFailed

	if got := resumeAt(chunks); got != 3 {
		t.Errorf("resume from %d; wanted 3, the chunk before the failure", got)
	}
}

func TestResumeDoesNotStepBackPastTheStart(t *testing.T) {
	chunks := planChunks(853*mb, 124*time.Minute)
	chunks[0].State = JobFailed
	if got := resumeAt(chunks); got != 0 {
		t.Errorf("resume from %d, wanted 0", got)
	}
}

func TestResumeIsPastTheEndWhenEverythingIsDone(t *testing.T) {
	chunks := planChunks(853*mb, 124*time.Minute)
	for i := range chunks {
		chunks[i].State = JobDone
	}
	if got := resumeAt(chunks); got != len(chunks) {
		t.Errorf("resume from %d, wanted %d — there is nothing left to do",
			got, len(chunks))
	}
}

func TestResumeIgnoresLaterDoneChunks(t *testing.T) {
	// A chunk after the gap being done does not make the gap done.
	chunks := planChunks(853*mb, 124*time.Minute)
	chunks[0].State = JobDone
	chunks[1].State = JobDone
	chunks[2].State = JobQueued
	chunks[3].State = JobDone

	if got := resumeAt(chunks); got != 1 {
		t.Errorf("resume from %d, wanted 1", got)
	}
}

func TestChunksDoneCounts(t *testing.T) {
	chunks := planChunks(853*mb, 124*time.Minute)
	chunks[0].State = JobDone
	chunks[2].State = JobDone
	chunks[3].State = JobFailed
	if got := chunksDone(chunks); got != 2 {
		t.Errorf("counted %d done, wanted 2", got)
	}
}

func TestPartialPathIsNamedAfterTheFilmAndTheChunk(t *testing.T) {
	j := &Jobs{scores: "/scores"}
	got := j.partialPath("Wanted.2008.BluRay.mkv", 7)
	want := "/scores/.partial/Wanted.2008.BluRay/chunk-007.componium"
	if filepath.ToSlash(got) != want {
		t.Errorf("partial path is %q, wanted %q", got, want)
	}
}
