package studio

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

// Chunk is one range of a film, and what happened when it was analysed.
//
// The state is per chunk rather than per film because that is the whole point:
// a film that stopped half way through has half a film of finished work, and
// the only thing standing between that and never doing it again is a record of
// which halves are which.
type Chunk struct {
	Index int `json:"index"`
	// From and To are seconds into the film. To is exclusive, and the last
	// chunk's To is the film's duration.
	From float64 `json:"from"`
	To   float64 `json:"to"`
	// State is one of queued, running, done or failed. A chunk is only ever
	// marked done once its partial score is on disk, so a done chunk is known
	// good and is never redone.
	State   JobState `json:"state"`
	Error   string   `json:"error,omitempty"`
	Seconds float64  `json:"seconds,omitempty"`
}

// Duration is how much film this chunk covers.
func (c Chunk) Duration() time.Duration {
	return time.Duration((c.To - c.From) * float64(time.Second))
}

const (
	// chunkTargetBytes is how much of the file a chunk should cover.
	//
	// Bytes rather than minutes because the work is decoding, and decoding is
	// paid for per byte. A hundred megabytes is small enough that losing one
	// costs a couple of minutes and large enough that the fixed cost of
	// starting four ffmpeg processes disappears against it.
	chunkTargetBytes = 100 << 20

	// The exchange rate between bytes and seconds is not a constant. Measured
	// across this library it varies by a factor of ten — 100MB is fifteen
	// minutes of one film and ninety seconds of another — so the byte target
	// picks a duration and the duration is then held to something sensible.
	// Unclamped, the same rule gives one film eight chunks and another
	// seventy-three, and seventy-three is mostly the overhead of starting.
	minChunk = 5 * time.Minute
	maxChunk = 20 * time.Minute
)

// planChunks cuts a film into ranges to analyse one at a time.
//
// A film shorter than one chunk comes back as a single chunk covering it,
// rather than as no chunks: everything downstream then has one path instead of
// two, and a short film resumes for free like any other.
func planChunks(size int64, duration time.Duration) []Chunk {
	if duration <= 0 {
		return nil
	}

	span := maxChunk
	if size > 0 {
		perSecond := float64(size) / duration.Seconds()
		if perSecond > 0 {
			span = time.Duration(chunkTargetBytes / perSecond * float64(time.Second))
		}
	}
	if span < minChunk {
		span = minChunk
	}
	if span > maxChunk {
		span = maxChunk
	}

	total := duration.Seconds()
	step := span.Seconds()
	count := int(math.Ceil(total / step))
	if count < 1 {
		count = 1
	}

	out := make([]Chunk, 0, count)
	for i := 0; i < count; i++ {
		// Both edges are computed the same way rather than one from the
		// other, so chunk i ends on exactly the float chunk i+1 starts on.
		// Adding a step to a start instead leaves a gap in the last bits, and
		// the boundary rules — a curve holding its value, a cue nominated
		// twice — are all equality tests on that number.
		from := float64(i) * step
		to := float64(i+1) * step
		if i == count-1 {
			// The last chunk runs to the end. Taken from the duration rather
			// than from the arithmetic so rounding cannot leave a sliver of
			// film analysed by nobody.
			to = total
		}
		out = append(out, Chunk{Index: i, From: from, To: to, State: JobQueued})
	}
	return out
}

// resumeAt is the chunk to start from when continuing an interrupted analysis.
//
// One before the first chunk that is not done, which is what was asked for. A
// done chunk is known good — it is only marked done once its partial is
// written — so this is not needed for correctness. What it buys is not having
// to reason about how far a killed process got, for the price of one chunk of
// repeated work, and that is a good trade for something whose entire job is to
// be trusted after a crash.
func resumeAt(chunks []Chunk) int {
	first := len(chunks)
	for i, c := range chunks {
		if c.State != JobDone {
			first = i
			break
		}
	}
	if first == 0 || first >= len(chunks) {
		// Nothing done, or everything done. Either way there is no earlier
		// chunk to step back to.
		if first >= len(chunks) {
			return len(chunks)
		}
		return 0
	}
	return first - 1
}

// chunksDone counts the chunks that will not be run again.
func chunksDone(chunks []Chunk) int {
	n := 0
	for _, c := range chunks {
		if c.State == JobDone {
			n++
		}
	}
	return n
}

// partialDir is where a film's finished chunks wait for the rest of them.
//
// Under the scores directory rather than a temporary one, because the whole
// point is to survive the studio stopping, and a directory the system may
// clear on reboot is the wrong place for the only copy of twenty minutes of
// work.
func (j *Jobs) partialDir(film string) string {
	base := strings.TrimSuffix(film, filepath.Ext(film))
	return filepath.Join(j.scores, ".partial", base)
}

// partialPath is where one chunk's score is written.
func (j *Jobs) partialPath(film string, index int) string {
	return filepath.Join(j.partialDir(film), fmt.Sprintf("chunk-%03d.componium", index))
}
