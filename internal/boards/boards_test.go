package boards_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Slicit/componium/internal/boards"
)

/* Remembering which boards exist.
 *
 * Most of what matters here is what happens to the file, because the file holds
 * secrets and is the only record that a board was ever attached.
 */

func shelf(t *testing.T) *boards.Shelf {
	t.Helper()
	s, err := boards.Open(filepath.Join(t.TempDir(), "boards.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAnInstallationWithNoBoardsIsOrdinary(t *testing.T) {
	// Not an error. It is every installation until somebody attaches one.
	s := shelf(t)
	if got := s.All(); len(got) != 0 {
		t.Fatalf("a fresh shelf holds %v", got)
	}
	if !s.Editable() {
		t.Error("a shelf with a path should be editable")
	}
}

func TestBoardsSurviveTheRoundTrip(t *testing.T) {
	/* The check the rig file failed once: written by the code that writes it,
	 * and read back by the code that reads it, with every field intact. */
	s := shelf(t)
	err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570", Secret: "a secret", Note: "the cracked case"},
		{Name: "ceiling", Addr: "192.168.1.146:5570", Secret: "another"},
	})
	if err != nil {
		t.Fatal(err)
	}

	again, err := boards.Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	got := again.All()
	if len(got) != 2 {
		t.Fatalf("read back %d boards", len(got))
	}
	if got[0].Name != "bench" || got[0].Addr != "192.168.1.145:5570" {
		t.Errorf("bench came back as %+v", got[0])
	}
	if got[0].Secret != "a secret" {
		t.Errorf("the secret did not survive: %q", got[0].Secret)
	}
	if got[0].Note != "the cracked case" {
		t.Errorf("the note did not survive: %q", got[0].Note)
	}
	if got[1].Secret != "another" {
		t.Errorf("the second secret did not survive: %q", got[1].Secret)
	}
}

func TestTheFileIsNotReadableByEveryone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix permissions here")
	}
	/* It holds the string that authorises moving a relay onto a pin, on every
	 * board it names, and there is no changing a board's secret except over
	 * USB. */
	s := shelf(t)
	if err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570", Secret: "a secret"},
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("boards file is mode %04o; anybody on this machine can read the secrets", mode)
	}
}

func TestTheFileSaysWhatItHolds(t *testing.T) {
	// Somebody will open this in an editor before they think about where it is
	// stored, and that is the moment to tell them.
	s := shelf(t)
	if err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570", Secret: "a secret"},
	}); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "CREDENTIALS") {
		t.Error("the file does not say that it holds credentials")
	}
}

func TestAnAddressWithoutAPortGetsTheUsualOne(t *testing.T) {
	// Typing the port is a thing nobody should have to do; it is never
	// interesting and it is always the same.
	got, err := boards.Validate([]boards.Board{{Name: "bench", Addr: "192.168.1.145"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Addr != "192.168.1.145:"+boards.DefaultPort {
		t.Errorf("address came out as %q", got[0].Addr)
	}
}

func TestAnAddressPastedFromABrowserIsUnderstood(t *testing.T) {
	/* People copy the web page's address, because that is the address they have
	 * been looking at. The rig learned this the hard way. */
	got, err := boards.Validate([]boards.Board{
		{Name: "bench", Addr: "http://192.168.1.145/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Addr != "192.168.1.145:"+boards.DefaultPort {
		t.Errorf("address came out as %q", got[0].Addr)
	}
}

func TestWhatIsRefused(t *testing.T) {
	for _, bad := range []struct {
		why    string
		boards []boards.Board
		says   string
	}{
		{"no name", []boards.Board{{Addr: "192.168.1.145:5570"}}, "no name"},
		{"no address", []boards.Board{{Name: "bench"}}, "not an address"},
		{"two names the same", []boards.Board{
			{Name: "bench", Addr: "192.168.1.145:5570"},
			{Name: "bench", Addr: "192.168.1.146:5570"},
		}, "two boards"},
		{"two boards on one address", []boards.Board{
			{Name: "bench", Addr: "192.168.1.145:5570"},
			{Name: "other", Addr: "192.168.1.145:5570"},
		}, "already another board"},
	} {
		t.Run(bad.why, func(t *testing.T) {
			_, err := boards.Validate(bad.boards)
			if err == nil {
				t.Fatal("accepted it")
			}
			if !strings.Contains(err.Error(), bad.says) {
				t.Errorf("said %q, wanted something about %q", err, bad.says)
			}
		})
	}
}

func TestARefusedSaveChangesNothing(t *testing.T) {
	/* Refused whole. Half a shelf is one somebody has to reconcile by hand
	 * against what is actually plugged in, which is the job this does for them. */
	s := shelf(t)
	if err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570", Secret: "a secret"},
	}); err != nil {
		t.Fatal(err)
	}
	err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570"},
		{Name: "broken", Addr: "not an address at all"},
	})
	if err == nil {
		t.Fatal("accepted a shelf with a bad address in it")
	}
	if got := s.All(); len(got) != 1 || got[0].Secret != "a secret" {
		t.Errorf("the shelf changed anyway: %+v", got)
	}
}

func TestRemovingABoardRemovesIt(t *testing.T) {
	// Deleting is saving a list without it, which is the same path as every
	// other edit and so cannot rot separately.
	s := shelf(t)
	if err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570"},
		{Name: "ceiling", Addr: "192.168.1.146:5570"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save([]boards.Board{{Name: "ceiling", Addr: "192.168.1.146:5570"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Find("bench"); ok {
		t.Error("bench is still there")
	}
	again, _ := boards.Open(s.Path())
	if len(again.All()) != 1 {
		t.Errorf("the file still holds %d", len(again.All()))
	}
}

func TestABoardCanBeLookedUpByName(t *testing.T) {
	// How the studio reaches a board without the secret going through a browser.
	s := shelf(t)
	if err := s.Save([]boards.Board{
		{Name: "bench", Addr: "192.168.1.145:5570", Secret: "a secret"},
	}); err != nil {
		t.Fatal(err)
	}
	b, ok := s.Find("bench")
	if !ok {
		t.Fatal("no bench")
	}
	if b.Secret != "a secret" || b.Addr != "192.168.1.145:5570" {
		t.Errorf("found %+v", b)
	}
	if _, ok := s.Find("nothing like it"); ok {
		t.Error("found a board that was never saved")
	}
}

func TestAShelfWithNowhereToSaveSaysSo(t *testing.T) {
	// The studio can be started without one, and then the page has to say the
	// list is read only rather than pretending an edit landed.
	s, err := boards.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Editable() {
		t.Fatal("a shelf with no path claims to be editable")
	}
	if err := s.Save([]boards.Board{{Name: "bench", Addr: "1.2.3.4:5570"}}); err == nil {
		t.Error("saved to nowhere")
	}
}
