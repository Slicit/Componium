package studio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// analysisSource is the file a chunk actually reads.
//
// The prepared copy when there is one, which is not an optimisation so much as
// the difference between a feature finishing and not. Measured on the H.265
// release in this library, decoding two minutes to the grayscale pass takes
// 25.0 seconds from the original and 4.7 from the prepared copy — the same
// frames, byte for byte, five times faster. Everything the analysis looks at
// is downscaled to 64x36 and 1kHz mono anyway, so there is nothing in the
// original for it to see that the copy has lost.
//
// The score still binds to the film rather than to the copy; see --hash-file.
func (j *Jobs) analysisSource(film string) string {
	if preview := filepath.Join(j.mediaDir, previewName(film)); fileExists(preview) {
		return preview
	}
	return filepath.Join(j.mediaDir, film)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// audioPeak measures the loudest window in the whole film, once.
//
// Every chunk is given it, because rms_windows normalises by the loudest
// window in whatever it is handed: per chunk that scales each chunk against
// its own peak, so a quiet chunk is amplified until it matches an action chunk
// and shake changes character at every boundary. Nothing fails, the score is
// simply wrong.
func (j *Jobs) audioPeak(ctx context.Context, source string) (float64, error) {
	cmd := exec.CommandContext(ctx, j.python, j.composer, source, "--probe-audio-peak")
	cmd.Dir = filepath.Dir(j.composer)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("measuring the audio: %w", err)
	}
	peak, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("measuring the audio: %w", err)
	}
	return peak, nil
}

// runAnalyse analyses one film, in pieces, resuming whatever is already done.
func (j *Jobs) runAnalyse(film string) error {
	out := j.ScorePath(film)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	source := j.analysisSource(film)
	info, err := probe(source)
	if err != nil {
		return err
	}

	chunks := j.chunksOf(film)
	if len(chunks) == 0 {
		var size int64
		if st, err := os.Stat(source); err == nil {
			size = st.Size()
		}
		chunks = planChunks(size, time.Duration(info.Duration*float64(time.Second)))
		if len(chunks) == 0 {
			return fmt.Errorf("%s has no duration; is it a playable file?", film)
		}
		j.setChunks(film, chunks)
	}

	// A generous ceiling rather than none, and per chunk rather than per film:
	// the whole point of chunking is that no single thing here runs for hours,
	// so a chunk that does is wedged.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	start := resumeAt(chunks)
	if start < len(chunks) {
		if start > 0 {
			j.update(JobAnalyse, film, false, func(job *Job) {
				job.Label = fmt.Sprintf("resuming at chunk %d of %d", start+1, len(chunks))
			})
		}
		peak, err := j.audioPeak(ctx, source)
		if err != nil {
			return err
		}

		for i := start; i < len(chunks); i++ {
			began := time.Now()
			j.markChunk(film, i, JobRunning, "")
			err := j.runChunk(ctx, film, source, chunks[i], len(chunks), peak)
			if err != nil {
				j.markChunk(film, i, JobFailed, err.Error())
				return fmt.Errorf("chunk %d of %d (%s to %s): %w",
					i+1, len(chunks), clock(chunks[i].From), clock(chunks[i].To), err)
			}
			j.finishChunk(film, i, time.Since(began).Seconds())
		}
	}

	j.update(JobAnalyse, film, false, func(job *Job) {
		job.Progress = 0.99
		job.Label = "joining the pieces"
	})
	return j.mergePartials(film, out)
}

// runChunk analyses one range and writes its partial score.
func (j *Jobs) runChunk(ctx context.Context, film, source string, c Chunk,
	total int, peak float64) error {

	partial := j.partialPath(film, c.Index)
	if err := os.MkdirAll(filepath.Dir(partial), 0o755); err != nil {
		return err
	}

	args := []string{
		j.composer, source,
		"-o", partial,
		"--motion-id", "motion.platform",
		"--hash-file", filepath.Join(j.mediaDir, film),
		"--from", strconv.FormatFloat(c.From, 'f', 3, 64),
		"--audio-peak", strconv.FormatFloat(peak, 'f', 6, 64),
	}
	// The last chunk is left open ended. Its end came from ffprobe, and
	// asking for a range that stops a frame short of the duration would leave
	// the tail of the film analysed by nobody.
	if c.Index < total-1 {
		args = append(args, "--to", strconv.FormatFloat(c.To, 'f', 3, 64))
	}

	cmd := exec.CommandContext(ctx, j.python, args...)
	cmd.Dir = filepath.Dir(j.composer)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var lastLines []string
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if fraction, label, ok := parseProgress(line); ok {
			// The bar counts the whole film, not this chunk: a bar that ran
			// to the end and started again eight times would be telling the
			// truth about the wrong thing.
			overall := (float64(c.Index) + fraction) / float64(total)
			j.update(JobAnalyse, film, false, func(job *Job) {
				job.Progress = overall
				job.Label = fmt.Sprintf("%s  ·  %d of %d", label, c.Index+1, total)
			})
			continue
		}
		lastLines = append(lastLines, line)
		if len(lastLines) > 6 {
			lastLines = lastLines[1:]
		}
	}

	if err := cmd.Wait(); err != nil {
		// A partial left behind by a failed run would be picked up as if it
		// were finished work. It is not: nothing here marks a chunk done until
		// its file is complete, and the file has to go with the state.
		os.Remove(partial)
		detail := strings.TrimSpace(strings.Join(lastLines, "; "))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s", detail)
	}
	return nil
}

// clock is a duration in minutes and seconds, for saying which chunk failed.
func clock(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
