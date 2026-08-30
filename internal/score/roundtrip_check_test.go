package score

import (
	"os"
	"testing"
)

// A one-off harness: point it at a file with COMPONIUM_CHECK and it reports
// whether the parser accepts it. Skipped unless asked.
func TestCheckAFileByHand(t *testing.T) {
	path := os.Getenv("COMPONIUM_CHECK")
	if path == "" {
		t.Skip("set COMPONIUM_CHECK to a score path")
	}
	sc, err := Load(path)
	if err != nil {
		t.Fatalf("REFUSED: %v", err)
	}
	t.Logf("loads: %q, %d tracks", sc.Meta.Title, len(sc.Tracks))
	for _, tr := range sc.Tracks {
		n := len(tr.Points)
		src := ""
		if tr.Type == TrackCue {
			n = len(tr.Cues)
			if n > 0 {
				src = tr.Cues[0].Source
			}
		}
		t.Logf("  %-16s %-6s %4d  %s", tr.Instrument, tr.Type, n, src)
	}
}
