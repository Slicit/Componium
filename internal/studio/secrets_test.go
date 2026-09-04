package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Slicit/componium/internal/cip"
)

/* Going live against a board that has a secret.
 *
 * The fault this covers: configuring a board worked and going live did not,
 * because those are two different files. The Boards page sent the stored
 * secret; the conductor built its instruments from the rig, which had none, so
 * every datagram arrived unsigned and the board refused it in silence. The only
 * reason anybody found out was the refusal log added the same day.
 *
 * The rig deliberately does not carry the secret. A rig is a file you commit.
 */

func rigNaming(t *testing.T, dir, addr string) string {
	t.Helper()
	path := filepath.Join(dir, "rig.toml")
	body := `[rig]
name = "test"

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "cip"
addr = "` + addr + `"

[[instrument]]
id = "light.ambient"
kind = "light"
driver = "virtual"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGoingLiveUsesTheSecretFromTheBoardsFile(t *testing.T) {
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	dir := t.TempDir()
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	boardsPath := filepath.Join(dir, "boards.toml")

	s, err := New(Options{
		Score:  score,
		Rig:    rigNaming(t, dir, n.Addr()),
		Boards: boardsPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The rig above names the board and says nothing about how to authenticate
	// to it. This is where that comes from.
	w := do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"`+secret+`"}]}`)
	if w.Code != 200 {
		t.Fatalf("saving the board said %d: %s", w.Code, w.Body.String())
	}

	w = do(t, s, "POST", "/api/live", `{"armed":true}`)
	if w.Code != 200 {
		t.Fatalf("go live said %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "no hello") {
		t.Fatalf("the conductor could not reach a board it has the secret for: %s",
			w.Body.String())
	}
}

func TestGoingLiveWithoutTheSecretIsRefused(t *testing.T) {
	/* The state the studio was actually in: a board that has a secret, a rig
	 * that does not, and nothing anywhere that knows one. It has to fail, and
	 * the mutation check depends on it failing. */
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	dir := t.TempDir()
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := New(Options{
		Score:  score,
		Rig:    rigNaming(t, dir, n.Addr()),
		Boards: filepath.Join(dir, "boards.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}

	w := do(t, s, "POST", "/api/live", `{"armed":true}`)
	if w.Code == 200 {
		t.Fatal("went live against a board that ignores unsigned traffic")
	}
}

func TestAnEntryWithItsOwnSecretKeepsIt(t *testing.T) {
	/* An installation that wants its rig self contained is still allowed one.
	 * The boards file is where secrets can live, not where they must. */
	const own = "the rig's own"
	n := startNode(t, own)

	dir := t.TempDir()
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rig.toml")
	body := `[rig]
name = "test"

[[instrument]]
id = "wind.main"
kind = "wind"
driver = "cip"
addr = "` + n.Addr() + `"
secret = "` + own + `"

[[instrument]]
id = "light.ambient"
kind = "light"
driver = "virtual"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	boardsPath := filepath.Join(dir, "boards.toml")
	s, err := New(Options{Score: score, Rig: path, Boards: boardsPath})
	if err != nil {
		t.Fatal(err)
	}
	// A board of the same address with a different secret, to prove the entry's
	// own is the one that wins rather than merely the one that is there.
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"the wrong one"}]}`)

	w := do(t, s, "POST", "/api/live", `{"armed":true}`)
	if w.Code != 200 {
		t.Fatalf("go live said %d: %s", w.Code, w.Body.String())
	}
}

func TestTheSecretIsNeverWrittenIntoARig(t *testing.T) {
	/* A rig is a file you commit, in a repository that is public. Whatever else
	 * changes, this must not: saving a rig must never produce a secret in it. */
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	dir := t.TempDir()
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	rigPath := rigNaming(t, dir, n.Addr())
	s, err := New(Options{Score: score, Rig: rigPath, Boards: filepath.Join(dir, "boards.toml")})
	if err != nil {
		t.Fatal(err)
	}
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"`+secret+`"}]}`)

	// Save the rig back through the studio, the way the Devices page does.
	w := do(t, s, "PUT", "/api/rig", `{"instruments":[
		{"id":"wind.main","kind":"wind","driver":"cip","addr":"`+n.Addr()+`"}]}`)
	if w.Code != 200 {
		t.Fatalf("saving the rig said %d: %s", w.Code, w.Body.String())
	}
	written, err := os.ReadFile(rigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), secret) {
		t.Fatalf("a secret was written into the rig file:\n%s", written)
	}
}

var _ = cip.Version
