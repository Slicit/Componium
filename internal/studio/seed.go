package studio

import (
	"os"

	"github.com/Slicit/componium/internal/score"
)

// SeedHistory keeps the scores that already exist, once.
//
// History only helps if there is something to compare against, and on the day
// it is switched on every score in the library predates it. Those are the
// baseline — the scores whose behaviour prompted the work — so they are worth
// more than the ones made after, not less.
//
// Runs on startup and does nothing for a film that already has history, so it
// is safe to call every time and needs nobody to remember to run it.
func (j *Jobs) SeedHistory(films []string) int {
	kept := 0
	for _, film := range films {
		if len(j.Versions(film)) > 0 {
			continue
		}
		path := j.ScorePath(film)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		sc, err := score.Load(path)
		if err != nil {
			// A score the studio cannot read is a score it cannot keep, and
			// saying so here would be shouting about it on every start. The
			// library already shows the film as having no usable score.
			continue
		}
		if _, err := j.Keep(film, sc, "kept when history was switched on"); err == nil {
			kept++
		}
	}
	return kept
}
