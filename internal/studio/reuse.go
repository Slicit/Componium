package studio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Looking at a film with a model is the expensive part, and it is the one part
// that does not need doing twice.
//
// Everything else in an analysis is a conclusion drawn from signals that can be
// measured again in minutes. What a model made of two thousand frames is not:
// it is minutes of a GPU, and the answer does not change when a threshold does.
// So a description, once written, is kept and reused, and the pass that made it
// runs only when there is nothing to reuse or when somebody asks for it again.
//
// Reusing it is not merely skipping work. A rebuild with the vision pass off
// would otherwise produce a score with no vision cues at all — the labels only
// exist inside that pass — so the kept description is applied to the new score
// afterwards, by the same remap that lets a mapping be changed by hand.

// hasDescription reports whether this film has already been looked at.
func (j *Jobs) hasDescription(film string) bool {
	return fileExists(j.SeenPath(film))
}

// remapScript is the pass that turns a description into cues.
func (j *Jobs) remapScript() string {
	return filepath.Join(filepath.Dir(j.composer), "remap.py")
}

// runRemap applies a kept description to a freshly built score.
//
// Reported and never fatal, like the calm pass: the score exists and is valid
// without it, and it is better to hand back a score missing its vision cues
// than to lose the analysis that produced everything else.
func (j *Jobs) runRemap(ctx context.Context, film, out string) (string, error) {
	script := j.remapScript()
	seen := out + seenSuffix
	if !fileExists(script) || !fileExists(seen) {
		return "", nil
	}

	tmp := out + ".remap.part"
	args := []string{script, out, "-o", tmp, "--seen", seen}
	args = append(args, j.devices...)

	cmd := exec.CommandContext(ctx, j.python, args...)
	cmd.Dir = filepath.Dir(j.composer)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var said []string
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "wrote ") {
			said = append(said, line)
		}
	}
	if err := cmd.Wait(); err != nil {
		os.Remove(tmp)
		detail := strings.TrimSpace(strings.Join(said, "; "))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s", detail)
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if len(said) > 0 {
		return said[len(said)-1], nil
	}
	return "", nil
}

// ForgetDescription throws away what a model saw, so the next analysis looks
// again.
//
// Separate from resetting the analysis, and deliberately not part of it.
// Resetting is for a plan that is suspect and costs minutes to redo; this is
// for a description that is suspect and costs a GPU. Somebody who wants the
// second almost always wants the first as well, and nobody wants the second by
// accident.
func (j *Jobs) ForgetDescription(film string) error {
	if err := os.Remove(j.SeenPath(film)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// lookAgain reports whether this job asked for a fresh look, and clears the
// request so that resuming an interrupted run does not pay for it twice.
func (j *Jobs) lookAgain(film string) bool {
	j.mu.Lock()
	job, ok := j.jobs[jobKey(JobAnalyse, film)]
	want := ok && job.LookAgain
	if want {
		job.LookAgain = false
	}
	j.mu.Unlock()
	if want {
		_ = j.ForgetDescription(film)
		j.mu.Lock()
		j.save()
		j.mu.Unlock()
	}
	return want
}
