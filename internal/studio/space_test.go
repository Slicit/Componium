package studio

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/score"
)

/* Opening a score and saving it must not change what it says.
 *
 * The studio round trips a score through a wire format the editor understands,
 * and anything that format does not carry comes back empty and is written out
 * as the default. That is how a curve authored in hue, saturation and intensity
 * came back declaring rgb: nothing on the page ever showed the field, nothing
 * about the save looked wrong, and the only symptom was a strip that stayed
 * black through a whole film while every counter on both sides reported
 * success.
 *
 * A field the editor does not expose is still a field the editor can destroy.
 */

const hsiScore = `# Componium score.
[score]
componium = "0.1"
title = "Sunrise"

[score.media]
duration = "00:02:00.000"
fps = 24.0

[[track]]
instrument = "light.ambient"
type = "curve"
interpolation = "linear"
space = "hsi"

[[track.points]]
t = "00:00:00.000"
[track.points.value]
h = 0.87
s = 0.9
i = 0.1

[[track.points]]
t = "00:00:20.000"
[track.points.value]
h = 0.12
s = 0.5
i = 0.8
`

// withScore is a studio holding one score, and the path it writes to.
func withScore(t *testing.T, body string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: path})
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestSavingAScoreKeepsItsColourSpace(t *testing.T) {
	/* Open it, save it back unchanged, and it must still say what it said.
	 * This is the exact sequence that corrupted a real film: no edit was
	 * needed, only a visit. */
	s, path := withScore(t, hsiScore)

	got := do(t, s, "GET", "/api/score", "")
	if got.Code != http.StatusOK {
		t.Fatalf("open said %d: %s", got.Code, got.Body.String())
	}
	// The page is handed the space, so a page that reads it can show it and a
	// page that only echoes it cannot lose it.
	var wire struct {
		Tracks []struct {
			Instrument string `json:"instrument"`
			Space      string `json:"space"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Tracks) != 1 || wire.Tracks[0].Space != "hsi" {
		t.Fatalf("the page was told %+v", wire.Tracks)
	}

	saved := do(t, s, "PUT", "/api/score", got.Body.String())
	if saved.Code != http.StatusOK {
		t.Fatalf("save said %d: %s", saved.Code, saved.Body.String())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `space = "hsi"`) {
		t.Errorf("the file no longer says it is hsi:\n%s", after)
	}
	if strings.Contains(string(after), `space = "rgb"`) {
		t.Error("an hsi track was relabelled rgb, which is how a light goes dark")
	}
}

func TestTheValuesStillReachAFixtureAfterASave(t *testing.T) {
	/* The consequence, rather than the declaration. A saved score has to still
	 * produce red, green and blue, because that is the only thing a light
	 * driver reads and the whole reason the declaration matters.
	 *
	 * Worth knowing what this does and does not catch: it passes even with
	 * the round trip broken, because resolve reads the keys now rather than
	 * believing the header. It is the second line, and the test above is the
	 * first. Said out loud because a test that looks like a guard and is not
	 * one is how this whole fault stayed hidden. */
	s, path := withScore(t, hsiScore)
	got := do(t, s, "GET", "/api/score", "")
	if w := do(t, s, "PUT", "/api/score", got.Body.String()); w.Code != http.StatusOK {
		t.Fatalf("save said %d: %s", w.Code, w.Body.String())
	}

	reopened, err := score.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	curves := reopened.Curves()
	if len(curves) != 1 {
		t.Fatalf("%d curve tracks after a save", len(curves))
	}
	v := curves[0].ValueAt(10 * time.Second) // ten seconds in, mid ramp
	for _, k := range []string{"r", "g", "b"} {
		if _, ok := v[k]; !ok {
			t.Fatalf("no %q after a save: this reaches a strip with nothing it "+
				"can read, and the strip stays dark", k)
		}
	}
}

func TestAPageThatSendsNoSpaceDoesNotDestroyIt(t *testing.T) {
	/* An older page, or any client that does not know about the field. It must
	 * be unable to relabel a track by omission, because omission is exactly
	 * what caused this and the field is still one nothing displays. */
	s, path := withScore(t, hsiScore)
	do(t, s, "GET", "/api/score", "")

	// The same score, with the space stripped out of the payload.
	silent := `{"title":"Sunrise","tracks":[{"instrument":"light.ambient",` +
		`"type":"curve","points":[{"t":0,"value":{"h":0.87,"s":0.9,"i":0.1}},` +
		`{"t":20,"value":{"h":0.12,"s":0.5,"i":0.8}}]}]}`
	if w := do(t, s, "PUT", "/api/score", silent); w.Code != http.StatusOK {
		t.Fatalf("save said %d: %s", w.Code, w.Body.String())
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `space = "hsi"`) {
		t.Errorf("a client that said nothing about the space changed it:\n%s", after)
	}
}
