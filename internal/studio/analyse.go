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

	j.clearSteps(film)
	j.beginStep(film, "preparing")

	source := j.analysisSource(film)
	info, err := probe(source)
	if err != nil {
		j.endSteps(film, JobFailed, err.Error())
		return err
	}

	limit := j.limitOf(film)
	chunks := j.chunksOf(film)
	if len(chunks) > 0 && !coversLimit(chunks, limit) {
		// The plan on file was made for a different amount of film. Keeping it
		// would silently analyse the wrong length — the far more surprising of
		// the two options, since the request that just arrived said otherwise.
		chunks = nil
		j.setChunks(film, nil)
	}
	if len(chunks) == 0 {
		var size int64
		if st, err := os.Stat(source); err == nil {
			size = st.Size()
		}
		length := info.Duration
		if limit > 0 && limit < length {
			// Scale the size down with the length. planChunks divides one by
			// the other to find the bitrate, and handing it the whole file's
			// size against a fraction of the film makes that fraction look
			// eight times denser than it is — so a fifteen minute run of a
			// feature planned as three five minute chunks instead of one.
			// Harmless, and wrong for a reason worth not leaving in place.
			if info.Duration > 0 {
				size = int64(float64(size) * (limit / info.Duration))
			}
			length = limit
		}
		chunks = planChunks(size, time.Duration(length*float64(time.Second)))
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

	// Looking at the film with a model is the expensive part and the one part
	// that does not need doing twice. A description already written is reused
	// unless somebody asks for a new one.
	look := !j.hasDescription(film) || j.lookAgain(film)
	if !look {
		j.update(JobAnalyse, film, false, func(job *Job) {
			job.Label = "reusing what the model already saw"
		})
		j.noteStep(film, "reusing what the model already saw")
	} else if os.Getenv("COMPONIUM_VLM_COMMAND") != "" {
		j.noteStep(film, "the model will look at this film")
	}

	start := resumeAt(chunks)
	if start < len(chunks) {
		if start > 0 {
			j.update(JobAnalyse, film, false, func(job *Job) {
				job.Label = fmt.Sprintf("resuming at chunk %d of %d", start+1, len(chunks))
			})
		}
		j.beginStep(film, "measuring the audio")
		peak, err := j.audioPeak(ctx, source)
		if err != nil {
			j.endSteps(film, JobFailed, err.Error())
			return err
		}

		for i := start; i < len(chunks); i++ {
			began := time.Now()
			j.beginStep(film, fmt.Sprintf("analysing %d of %d", i+1, len(chunks)))
			j.markChunk(film, i, JobRunning, "")
			// Only the last chunk of a run that reaches the film's own end is
			// open ended. Under a limit, every chunk has a far edge.
			openEnded := i == len(chunks)-1 && (limit <= 0 || limit >= info.Duration)
			err := j.runChunk(ctx, film, source, chunks[i], len(chunks), peak,
				openEnded, look)
			if err != nil {
				j.markChunk(film, i, JobFailed, err.Error())
				j.endSteps(film, JobFailed, err.Error())
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
	j.beginStep(film, "joining the pieces")
	if err := j.mergePartials(film, out, j.note(len(chunks))); err != nil {
		j.endSteps(film, JobFailed, err.Error())
		return err
	}

	if !look {
		// The labels only exist inside the vision pass, so a rebuild that
		// skipped it has no vision cues at all until the kept description is
		// applied to it.
		j.beginStep(film, "applying what the model saw")
		remapCtx, stopRemap := context.WithTimeout(context.Background(), calmTimeout)
		if said, err := j.runRemap(remapCtx, film, out); err != nil {
			j.update(JobAnalyse, film, false, func(job *Job) {
				job.Label = "scored, but the kept description did not apply: " + err.Error()
			})
		} else if said != "" {
			j.update(JobAnalyse, film, false, func(job *Job) { job.Label = said })
		}
		stopRemap()
	}

	// The last act: quiet the parts of the film that are not doing anything.
	// Reported and never fatal — the score is written and valid by now, and a
	// film busier than it should be is a worse score rather than a lost one.
	j.update(JobAnalyse, film, false, func(job *Job) {
		job.Label = "finding the quiet parts"
	})
	j.beginStep(film, "finding the quiet parts")
	calmCtx, stop := context.WithTimeout(context.Background(), calmTimeout)
	defer stop()
	if said, err := j.runCalm(calmCtx, film, out); err != nil {
		j.update(JobAnalyse, film, false, func(job *Job) {
			job.Label = "scored, but not quieted: " + err.Error()
		})
		j.endSteps(film, JobFailed, err.Error())
		return nil
	} else if said != "" {
		j.update(JobAnalyse, film, true, func(job *Job) {
			job.Label = said
		})
		j.endSteps(film, "", said)
		return nil
	}
	j.endSteps(film, "", "")
	return nil
}

// runChunk analyses one range and writes its partial score.
// runChunk analyses one range and writes its partial score.
//
// openEnded says this chunk runs to the end of the film, in which case it is
// left without a --to: the end came from ffprobe, and asking for a range that
// stops a frame short of the duration would leave the tail analysed by nobody.
// A chunk that is merely last in a shortened run is not open ended, and saying
// otherwise sends it decoding to the end of the film — measured at chunk three
// of a fifteen minute run reading an hour and three quarters it had no use for.
func (j *Jobs) runChunk(ctx context.Context, film, source string, c Chunk,
	total int, peak float64, openEnded bool, look bool) error {

	partial := j.partialPath(film, c.Index)
	if err := os.MkdirAll(filepath.Dir(partial), 0o755); err != nil {
		return err
	}

	args := []string{
		j.composer, source,
		"-o", partial,
		"--motion-id", "motion.platform",
		"--hash-file", filepath.Join(j.mediaDir, film),
	}
	// Which fogger, which fan, which scent — from the rig rather than from the
	// composer's guesses, or a rig whose fogger is called fog.left gets every
	// smoke cue addressed to a fog.main that does not exist.
	args = append(args, j.devices...)
	// The vision seam, when one is configured.
	//
	// Off unless asked for, because it needs a model on the other end of it
	// and a studio that quietly failed to reach one on every frame of every
	// film would be slower for no benefit and say nothing about why.
	if cmd := os.Getenv("COMPONIUM_VLM_COMMAND"); cmd != "" && look {
		args = append(args, "--vlm-command", cmd)
		if n := os.Getenv("COMPONIUM_VLM_FRAMES"); n != "" {
			args = append(args, "--vlm-frames", n)
		}
	}
	args = append(args, []string{
		"--from", strconv.FormatFloat(c.From, 'f', 3, 64),
		"--audio-peak", strconv.FormatFloat(peak, 'f', 6, 64),
	}...)
	if !openEnded {
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
