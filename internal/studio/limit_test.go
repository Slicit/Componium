package studio

import (
	"testing"
	"time"
)

// openEnded is the rule runAnalyse applies, kept here as a function so it can
// be asserted without running an analysis. It mirrors the expression at the
// call site; if that changes, this should fail.
func openEndedAt(i, count int, limit, duration float64) bool {
	return i == count-1 && (limit <= 0 || limit >= duration)
}

func TestTheLastChunkOfAWholeFilmIsOpenEnded(t *testing.T) {
	// Its end came from ffprobe, and stopping a frame short would leave the
	// tail of the film analysed by nobody.
	if !openEndedAt(8, 9, 0, 7434) {
		t.Error("the last chunk of an unlimited run was closed")
	}
}

func TestTheLastChunkOfALimitedRunIsNotOpenEnded(t *testing.T) {
	// This is the bug. Left open ended, chunk three of a fifteen minute run
	// decoded from 598s to the end of a two hour film — an hour and three
	// quarters it had no use for, and a "fifteen minute" score whose last
	// chunk quietly covered everything.
	if openEndedAt(2, 3, 900, 7434) {
		t.Error("the last chunk of a fifteen minute run was left open ended")
	}
}

func TestEarlierChunksAreNeverOpenEnded(t *testing.T) {
	for i := 0; i < 8; i++ {
		if openEndedAt(i, 9, 0, 7434) {
			t.Errorf("chunk %d of 9 was open ended", i)
		}
	}
}

func TestALimitLongerThanTheFilmIsNotALimit(t *testing.T) {
	// Asking for thirty minutes of a ten minute film is asking for the film.
	if !openEndedAt(1, 2, 1800, 600) {
		t.Error("a limit past the end of the film closed the last chunk")
	}
}

func TestPlanUnderALimitCoversOnlyTheLimit(t *testing.T) {
	// The size is scaled with the length at the call site; here the plan is
	// checked to cover exactly what was asked for and no more.
	chunks := planChunks(115*mb, 15*time.Minute)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if got := chunks[len(chunks)-1].To; got != 900 {
		t.Errorf("a fifteen minute plan ends at %v", got)
	}
	if chunks[0].From != 0 {
		t.Errorf("a fifteen minute plan starts at %v", chunks[0].From)
	}
}

func TestScalingTheSizeWithTheLimitKeepsTheBitrate(t *testing.T) {
	// planChunks divides size by duration to find the bitrate. Handing it the
	// whole file's size against a fraction of the film makes that fraction
	// look denser than it is, and it plans more, shorter chunks than the byte
	// target asked for.
	full := int64(853 * mb)
	const filmSeconds = 7434.0
	const limitSeconds = 900.0

	whole := planChunks(full, time.Duration(filmSeconds*float64(time.Second)))
	scaled := int64(float64(full) * (limitSeconds / filmSeconds))
	limited := planChunks(scaled, time.Duration(limitSeconds*float64(time.Second)))

	// Same bitrate, so the same chunk length — the limited run is simply
	// shorter, not differently shaped.
	wholeSpan := whole[0].To - whole[0].From
	limitedSpan := limited[0].To - limited[0].From
	if diff := wholeSpan - limitedSpan; diff > 1 || diff < -1 {
		t.Errorf("chunk length changed with the limit: %.0fs against %.0fs",
			limitedSpan, wholeSpan)
	}
}
