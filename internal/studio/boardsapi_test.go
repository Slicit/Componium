package studio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

/* Remembering boards, over HTTP.
 *
 * The thing worth guarding here is the secret. It has to reach the boards file
 * and it must never come back out, because the whole reason for storing it is
 * that a browser should not have to hold it.
 */

func withBoards(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "boards.toml")
	// A score, because a studio needs something to open. Not what this
	// file is about, but a server with nothing in it will not start.
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: score, Boards: path})
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

// A software node, running, the way the cip package's own tests start one.
func startNode(t *testing.T, secret string) *cip.Node {
	t.Helper()
	// Carrying one device, because a board that announces nothing is a board
	// no rig can name, and the live tests below name one.
	n, err := cip.NewNode(cip.NodeConfig{
		Addr: "127.0.0.1:0", Secret: secret,
		Manifest: cip.Manifest{
			ID: "wind.main", Kind: "wind",
			LatencyMS: cip.Ms(1200 * time.Millisecond),
			SafeState: map[string]float64{"intensity": 0},
			Channels:  []cip.Channel{{Name: "intensity", Unit: "normalised", Range: [2]float64{0, 1}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go n.Run(ctx)
	t.Cleanup(func() { cancel(); n.Close() })
	return n
}

func do(t *testing.T, s *Server, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, url, nil)
	} else {
		r = httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestAnAttachedBoardIsRemembered(t *testing.T) {
	// The whole complaint: attaching one used to leave no trace at all.
	s, path := withBoards(t)

	w := do(t, s, "PUT", "/api/boards", `{"boards":[
		{"name":"bench","addr":"192.168.1.145","secret":"a secret","note":"the cracked case"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save said %d: %s", w.Code, w.Body.String())
	}

	var got struct {
		Editable bool `json:"editable"`
		Boards   []struct {
			Name      string `json:"name"`
			Addr      string `json:"addr"`
			Note      string `json:"note"`
			Secret    string `json:"secret"`
			HasSecret bool   `json:"hasSecret"`
		} `json:"boards"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Boards) != 1 {
		t.Fatalf("saved %d boards", len(got.Boards))
	}
	b := got.Boards[0]
	if b.Name != "bench" || b.Note != "the cracked case" {
		t.Errorf("came back as %+v", b)
	}
	// The port is not a thing anybody should have to type.
	if b.Addr != "192.168.1.145:5570" {
		t.Errorf("address came back as %q", b.Addr)
	}

	// And it is on disk, which is the part that survives the page closing.
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "bench") {
		t.Error("the file does not mention the board")
	}
}

func TestTheSecretNeverComesBackOut(t *testing.T) {
	/* The reason the shelf exists. If the page could read secrets back then
	 * storing them would have bought nothing: the string that authorises moving
	 * a relay onto a pin would still be going through a browser. */
	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"192.168.1.145","secret":"hunter2"}]}`)

	for _, w := range []*httptest.ResponseRecorder{
		do(t, s, "GET", "/api/boards", ""),
		do(t, s, "POST", "/api/boards/check", ""),
	} {
		if strings.Contains(w.Body.String(), "hunter2") {
			t.Fatalf("a secret came back over the wire: %s", w.Body.String())
		}
	}

	// But the page still needs to know one is held, to show which boards can
	// actually be reached.
	w := do(t, s, "GET", "/api/boards", "")
	if !strings.Contains(w.Body.String(), `"hasSecret":true`) {
		t.Errorf("the page cannot tell that a secret is held: %s", w.Body.String())
	}
}

func TestEditingABoardKeepsItsSecret(t *testing.T) {
	/* The page never receives the secret, so it cannot send one back. Without
	 * this, renaming a note would silently lock the studio out of the board. */
	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"192.168.1.145","secret":"hunter2"}]}`)

	w := do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"192.168.1.145","note":"moved to the shelf"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save said %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"hasSecret":true`) {
		t.Fatalf("the secret was dropped by an edit that did not mention it: %s", w.Body.String())
	}
}

func TestABoardCanBeRemoved(t *testing.T) {
	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards", `{"boards":[
		{"name":"bench","addr":"192.168.1.145","secret":"x"},
		{"name":"ceiling","addr":"192.168.1.146","secret":"y"}]}`)

	w := do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"ceiling","addr":"192.168.1.146"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save said %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "bench") {
		t.Errorf("bench is still there: %s", w.Body.String())
	}
	// And the one that stayed kept the secret it was not sent.
	if !strings.Contains(w.Body.String(), `"hasSecret":true`) {
		t.Errorf("ceiling lost its secret: %s", w.Body.String())
	}
}

func TestARefusedSaveSaysWhy(t *testing.T) {
	s, _ := withBoards(t)
	w := do(t, s, "PUT", "/api/boards", `{"boards":[
		{"name":"bench","addr":"192.168.1.145"},
		{"name":"other","addr":"192.168.1.145"}]}`)
	if w.Code == http.StatusOK {
		t.Fatal("accepted two boards at one address")
	}
	if !strings.Contains(w.Body.String(), "already another board") {
		t.Errorf("said %q", w.Body.String())
	}
}

func TestABoardThatIsNotThereReadsAsOffline(t *testing.T) {
	/* Offline is the ordinary state of a board on a shelf, and it has to be a
	 * row that says so rather than an error that loses the whole list. */
	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"absent","addr":"127.0.0.1:5999","secret":"x"}]}`)

	w := do(t, s, "POST", "/api/boards/check", "")
	if w.Code != http.StatusOK {
		t.Fatalf("check said %d", w.Code)
	}
	var got struct {
		Boards []struct {
			Name   string `json:"name"`
			Online bool   `json:"online"`
			Why    string `json:"why"`
		} `json:"boards"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Boards) != 1 || got.Boards[0].Online {
		t.Fatalf("check returned %+v", got.Boards)
	}
	if got.Boards[0].Why == "" {
		t.Error("offline with no reason; a wrong secret and an unplugged board look the same otherwise")
	}
}

func TestABoardThatAnswersReadsAsOnline(t *testing.T) {
	// Against a real node speaking the real protocol, because the interesting
	// half of "online" is that the secret was right.
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"`+secret+`"}]}`)

	w := do(t, s, "POST", "/api/boards/check", "")
	var got struct {
		Boards []struct {
			Name     string `json:"name"`
			Online   bool   `json:"online"`
			Firmware string `json:"firmware"`
			Why      string `json:"why"`
		} `json:"boards"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Boards) != 1 || !got.Boards[0].Online {
		t.Fatalf("check returned %+v", got.Boards)
	}
	if got.Boards[0].Firmware == "" {
		t.Error("online but with no firmware version; the check learned nothing")
	}
}

func TestAWrongSecretIsNotOnline(t *testing.T) {
	/* The distinction the Why field exists for. A board that is switched on and
	 * refusing us is not a board we can use, and calling it online would be a
	 * green light next to something that will not take a cue. */
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	s, _ := withBoards(t)
	do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"not the secret"}]}`)

	w := do(t, s, "POST", "/api/boards/check", "")
	if strings.Contains(w.Body.String(), `"online":true`) {
		t.Errorf("a board refusing our secret was called online: %s", w.Body.String())
	}
}

func TestAStudioWithNoBoardsFileSaysSo(t *testing.T) {
	// Rather than offering an Add button that loses what it is given.
	dir := t.TempDir()
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: score})
	if err != nil {
		t.Fatal(err)
	}
	w := do(t, s, "GET", "/api/boards", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list said %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"editable":false`) {
		t.Errorf("claims to be editable with nowhere to save: %s", w.Body.String())
	}

	w = do(t, s, "PUT", "/api/boards", `{"boards":[{"name":"bench","addr":"1.2.3.4"}]}`)
	if w.Code == http.StatusOK {
		t.Error("accepted a save with nowhere to put it")
	}
}
