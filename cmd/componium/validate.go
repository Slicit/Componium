package main

import (
	"flag"
	"fmt"

	"github.com/Slicit/Componium/internal/rig"
	"github.com/Slicit/Componium/internal/score"
)

// validateCmd checks a score without playing it.
//
// The composer generates scores, and a generated score should be checkable
// before anyone points it at hardware. With -rig it also confirms that every
// instrument the score names actually exists in that installation.
func validateCmd(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	scorePath := fs.String("score", "", "score file (required)")
	rigPath := fs.String("rig", "", "optional rig file to check the score against")
	fs.Parse(args)

	if *scorePath == "" {
		return fmt.Errorf("-score is required")
	}
	sc, err := score.Load(*scorePath)
	if err != nil {
		return err
	}

	cues := sc.Cues()
	curves := sc.Curves()
	fmt.Printf("score      %s\n", *scorePath)
	fmt.Printf("title      %s\n", sc.Meta.Title)
	fmt.Printf("duration   %s\n", sc.Meta.Media.Duration)
	if sc.Meta.Media.Hash != "" {
		fmt.Printf("hash       %s\n", sc.Meta.Media.Hash)
	}
	fmt.Printf("tracks     %d cue, %d curve\n", len(sc.Tracks)-len(curves), len(curves))
	fmt.Printf("cues       %d\n", len(cues))

	var points int
	for _, c := range curves {
		points += len(c.Points)
		fmt.Printf("  curve %-16s %d points, %s to %s\n",
			c.Instrument, len(c.Points), c.Points[0].T, c.Points[len(c.Points)-1].T)
	}
	fmt.Printf("instruments %v\n", sc.Instruments())

	if *rigPath == "" {
		fmt.Println("\nvalid")
		return nil
	}

	rc, err := rig.Load(*rigPath)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, in := range rc.Instruments {
		have[in.ID] = true
	}
	var missing []string
	for _, id := range sc.Instruments() {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("rig %q lacks instruments the score needs: %v", *rigPath, missing)
	}
	fmt.Printf("\nvalid against %s (%s)\n", *rigPath, rc.Rig.Name)
	return nil
}
