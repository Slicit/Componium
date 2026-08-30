package studio

import (
	"fmt"
	"os"
	"strings"
)

// note describes the run that produced a score, for the person choosing which
// two versions to compare six weeks from now.
//
// Free text, because it is read rather than parsed. What matters is that the
// things which actually change a score's character are in it: whether the
// vision seam was on, which model answered, how often it looked, and how many
// pieces the film was cut into.
func (j *Jobs) note(chunks int) string {
	parts := []string{fmt.Sprintf("%d chunk%s", chunks, plural(chunks))}

	if os.Getenv("COMPONIUM_CALM") == "off" {
		parts = append(parts, "not quieted")
	}

	if cmd := os.Getenv("COMPONIUM_VLM_COMMAND"); cmd != "" {
		vision := "vision"
		if model := os.Getenv("COMPONIUM_VLM_MODEL"); model != "" {
			vision += " " + model
		}
		// How often it looked, which is what decides coverage. The cap is
		// mentioned only when there is one: 0 means the grid keeps its own
		// spacing, and writing "0 frames" describes a run that looked at
		// nothing rather than one that looked at everything.
		if every := os.Getenv("COMPONIUM_VLM_EVERY"); every != "" {
			vision += ", every " + every + "s"
		}
		if n := os.Getenv("COMPONIUM_VLM_FRAMES"); n != "" && n != "0" {
			vision += ", at most " + n + " frames"
		}
		parts = append(parts, vision)
	} else {
		// Said explicitly rather than left out. "no vision" and "somebody
		// forgot to write down whether vision was on" look identical if the
		// absence of the words is what carries the meaning.
		parts = append(parts, "no vision")
	}

	return strings.Join(parts, " · ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
