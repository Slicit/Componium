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
// vision seam was on, which model answered, and how many pieces the film was
// cut into — that last one because the keyframe budget is per chunk, so the
// same setting means very different coverage on a short film and a feature.
func (j *Jobs) note(chunks int) string {
	parts := []string{fmt.Sprintf("%d chunk%s", chunks, plural(chunks))}

	if cmd := os.Getenv("COMPONIUM_VLM_COMMAND"); cmd != "" {
		vision := "vision"
		if model := os.Getenv("COMPONIUM_VLM_MODEL"); model != "" {
			vision += " " + model
		}
		if n := os.Getenv("COMPONIUM_VLM_FRAMES"); n != "" {
			vision += ", " + n + " frames per chunk"
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
