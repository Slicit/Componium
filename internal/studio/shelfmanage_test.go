package studio

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/* Making rigs, and taking work off this machine.
 *
 * A shelf that can only be chosen from is a shelf somebody has to fill over
 * ssh: the studio could manage a rig right up until the moment anybody wanted
 * a second one.
 *
 * Most of what is tested here is refusals, because every one of them is a way
 * to lose a file that describes what is wired to the mains.
 */

// onShelf is a studio holding a directory of rigs.
func onShelf(t *testing.T, rigs map[string]string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	shelf := filepath.Join(dir, "rigs")
	if err := os.MkdirAll(shelf, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range rigs {
		if err := os.WriteFile(filepath.Join(shelf, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: score, Rig: shelf})
	if err != nil {
		t.Fatal(err)
	}
	return s, shelf
}

func shelfFrom(t *testing.T, body []byte) []string {
	t.Helper()
	var got struct {
		Rigs []string `json:"rigs"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%v in %s", err, body)
	}
	return got.Rigs
}

func TestASecondRigCanBeMade(t *testing.T) {
	// The complaint: one rig on the shelf and no way to add another without a
	// shell on the machine.
	s, shelf := onShelf(t, map[string]string{"demo-rig.toml": trimRig})

	w := do(t, s, "POST", "/api/rigs/new", `{"name":"the-room"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	if got := shelfFrom(t, w.Body.Bytes()); len(got) != 2 {
		t.Errorf("the shelf holds %v", got)
	}
	// And it is a rig that loads, rather than an empty file somebody has to
	// repair before the studio will open again.
	if _, err := os.Stat(filepath.Join(shelf, "the-room.toml")); err != nil {
		t.Fatal(err)
	}
	if w := do(t, s, "POST", "/api/rigs", `{"rig":"the-room.toml"}`); w.Code != http.StatusOK {
		t.Errorf("the new rig will not open: %s", w.Body.String())
	}
}

func TestARigCanBeStartedFromTheOneAlreadyWorking(t *testing.T) {
	/* How a second rig actually comes to exist: the room as it stands, plus
	 * one change. Starting from blank means retyping every address. */
	s, shelf := onShelf(t, map[string]string{"demo-rig.toml": trimRig})

	w := do(t, s, "POST", "/api/rigs/new",
		`{"name":"experiment","from":"demo-rig.toml"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	text, err := os.ReadFile(filepath.Join(shelf, "experiment.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"light.ambient", "light.event", "wind.main"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("the copy is missing %s:\n%s", want, text)
		}
	}
	// Named for itself rather than for what it was copied from, or a shelf of
	// copies is a shelf of things all called the same thing.
	if !strings.Contains(string(text), `name = "experiment"`) {
		t.Errorf("the copy kept the original's name:\n%s", text)
	}
}

func TestARigIsNotOverwrittenByAccident(t *testing.T) {
	// Somebody typing a name that already exists means to open it, not to
	// erase what is behind it.
	s, _ := onShelf(t, map[string]string{"demo-rig.toml": trimRig})
	w := do(t, s, "POST", "/api/rigs/new", `{"name":"demo-rig"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("said %d: %s", w.Code, w.Body.String())
	}
}

func TestANameCannotEscapeTheShelf(t *testing.T) {
	/* This becomes a filename in a directory a conductor reads at startup, so
	 * a name that walks out of it is a rig played from somewhere nobody
	 * looked, or a file written over somewhere nobody expects. */
	s, shelf := onShelf(t, map[string]string{"demo-rig.toml": trimRig})
	for _, bad := range []string{
		"../escaped", "sub/dir", ".hidden", "", "   ",
		"with space", "semi;colon",
	} {
		body, _ := json.Marshal(map[string]string{"name": bad})
		w := do(t, s, "POST", "/api/rigs/new", string(body))
		if w.Code == http.StatusOK {
			t.Errorf("accepted %q", bad)
		}
	}
	// Nothing was written anywhere, including one directory up.
	entries, err := os.ReadDir(filepath.Dir(shelf))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "escaped") {
			t.Errorf("something landed outside the shelf: %s", e.Name())
		}
	}
}

func TestTheLastRigCannotBeRemoved(t *testing.T) {
	/* A shelf with nothing on it is a studio that will not open and a
	 * conductor that will not start, recoverable only by putting a file there
	 * by hand. Refusing is kinder than allowing it and explaining later. */
	s, _ := onShelf(t, map[string]string{"demo-rig.toml": trimRig})
	w := do(t, s, "POST", "/api/rigs/delete", `{"name":"demo-rig"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("said %d: %s", w.Code, w.Body.String())
	}
}

func TestRemovingTheRigInUseFallsBackRatherThanBreaking(t *testing.T) {
	// The studio has to still be holding a rig afterwards, whichever one went.
	s, _ := onShelf(t, map[string]string{
		"demo-rig.toml": trimRig, "spare.toml": trimRig,
	})
	do(t, s, "POST", "/api/rigs", `{"rig":"spare.toml"}`)

	w := do(t, s, "POST", "/api/rigs/delete", `{"name":"spare"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	if got := shelfFrom(t, w.Body.Bytes()); len(got) != 1 || got[0] != "demo-rig.toml" {
		t.Errorf("the shelf holds %v", got)
	}
	if w := do(t, s, "GET", "/api/rig", ""); w.Code != http.StatusOK {
		t.Errorf("the studio has no rig afterwards: %s", w.Body.String())
	}
}

func TestARigCanBeCarriedToAnotherMachine(t *testing.T) {
	/* The point of exporting: the machine that runs the room for real is not
	 * the machine somebody sets it up on. What comes back is the file itself,
	 * because a rig is TOML people hand edit and an interchange format of our
	 * own would be a second thing to keep correct. */
	s, _ := onShelf(t, map[string]string{"demo-rig.toml": trimRig})

	w := do(t, s, "GET", "/api/rigs/export?rig=demo-rig", "")
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "light.ambient") {
		t.Errorf("that is not the rig:\n%s", w.Body.String())
	}
	// Offered as a file rather than shown as a page, because the point is a
	// file on somebody's machine.
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "demo-rig.toml") {
		t.Errorf("it is not offered as a download: %q", cd)
	}
}

func TestAnImportedRigIsCheckedBeforeItIsWritten(t *testing.T) {
	/* A shelf is read by a conductor at startup. A file that parses only after
	 * it is on the shelf is a show that does not begin, found by whoever is
	 * standing in the room. */
	s, shelf := onShelf(t, map[string]string{"demo-rig.toml": trimRig})

	w := do(t, s, "POST", "/api/rigs/import?name=rubbish", "this is not toml {{{")
	if w.Code != http.StatusBadRequest {
		t.Errorf("said %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(shelf, "rubbish.toml")); err == nil {
		t.Error("it wrote the file anyway")
	}

	// One that would load is taken, and is on the shelf afterwards.
	if w := do(t, s, "POST", "/api/rigs/import?name=brought-in", trimRig); w.Code != http.StatusOK {
		t.Fatalf("a good rig was refused: %s", w.Body.String())
	}
	if got := shelfFrom(t, do(t, s, "GET", "/api/rigs", "").Body.Bytes()); len(got) != 2 {
		t.Errorf("the shelf holds %v", got)
	}
}

func TestAnImportDoesNotSilentlyReplaceWhatIsThere(t *testing.T) {
	// Dropping a file with a familiar name on a machine should not quietly
	// take out the rig that machine has been running.
	s, _ := onShelf(t, map[string]string{"demo-rig.toml": trimRig})

	w := do(t, s, "POST", "/api/rigs/import?name=demo-rig", trimRig)
	if w.Code != http.StatusConflict {
		t.Errorf("said %d: %s", w.Code, w.Body.String())
	}
	// Said explicitly, it goes through, because sometimes replacing is exactly
	// the intention.
	if w := do(t, s, "POST", "/api/rigs/import?name=demo-rig&replace=yes", trimRig); w.Code != http.StatusOK {
		t.Errorf("an explicit replace was refused: %s", w.Body.String())
	}
}

func TestAScoreCanBeDownloadedWhenItIsDone(t *testing.T) {
	// The other half of taking work off a machine. A score is the thing the
	// analysis spent an hour making.
	s, _ := onShelf(t, map[string]string{"demo-rig.toml": trimRig})

	w := do(t, s, "GET", "/api/score/export", "")
	if w.Code != http.StatusOK {
		t.Fatalf("said %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "[[track]]") {
		t.Errorf("that is not a score:\n%s", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, ".componium") {
		t.Errorf("it is not offered as a download: %q", cd)
	}
}

func TestAStudioWithOneRigFileSaysThereIsNoShelf(t *testing.T) {
	/* Started with -rig pointing at a file rather than a directory. Adding to
	 * a shelf that does not exist has to say so rather than inventing one
	 * beside somebody's file. */
	s, _ := withRig(t)
	for _, path := range []string{"/api/rigs/new", "/api/rigs/delete", "/api/rigs/import?name=x"} {
		w := do(t, s, "POST", path, `{"name":"anything"}`)
		if w.Code != http.StatusConflict {
			t.Errorf("%s said %d: %s", path, w.Code, w.Body.String())
		}
	}
	// Exporting still works, because there is exactly one rig to export.
	if w := do(t, s, "GET", "/api/rigs/export", ""); w.Code != http.StatusOK {
		t.Errorf("could not export the only rig: %s", w.Body.String())
	}
}
