package studio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Quieting the parts of a film that are not doing anything.
//
// The last thing the analysis does, and the only one that can decide it has
// nothing to do. It needs the description — it is reading what a model made of
// each moment as well as what the signals measured — so it runs when there is
// one and is skipped when there is not, which is the same as saying it runs
// when a vision model was configured.
//
// A separate process rather than part of the composer because it is a separate
// pass: it reads a finished score and writes a finished score, touches no film
// and takes no time, and the whole point of it being cheap is that it can be
// run again by hand with a different budget.

// calmScript is the pass, beside the composer that produced the score.
func (j *Jobs) calmScript() string {
	return filepath.Join(filepath.Dir(j.composer), "calm.py")
}

// runCalm quiets a finished score in place.
//
// Every failure here is reported and none is fatal. The score is already
// written and already valid; a film that ends up busier than it should be is a
// worse score, not a lost one, and losing a two hour analysis over the last
// step of it would be indefensible.
func (j *Jobs) runCalm(ctx context.Context, film, out string) (string, error) {
	if os.Getenv("COMPONIUM_CALM") == "off" {
		return "", nil
	}
	seen := out + seenSuffix
	if !fileExists(seen) {
		// No description, so nothing to read the film's character from. The
		// signals alone would decide it, and they disagree with each other
		// about which of them is trustworthy.
		return "", nil
	}
	script := j.calmScript()
	if !fileExists(script) {
		return "", nil
	}

	// Written beside the score and moved into place, so an interrupted pass
	// cannot leave a half-written score where a whole one was.
	tmp := out + ".calm.part"
	args := []string{script, out, "-o", tmp, "--seen", seen}
	if floor := os.Getenv("COMPONIUM_CALM_FLOOR"); floor != "" {
		args = append(args, "--floor", floor)
	}

	cmd := exec.CommandContext(ctx, j.python, args...)
	cmd.Dir = filepath.Dir(j.composer)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	// The pass says what it did on stderr, and the last thing it says is worth
	// keeping: how much of the film it quieted.
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
	return "quieted", nil
}

// calmTimeout is generous for something that reads two files. It exists so a
// wedged interpreter cannot hold the single worker after the analysis itself
// has finished.
const calmTimeout = 5 * time.Minute
