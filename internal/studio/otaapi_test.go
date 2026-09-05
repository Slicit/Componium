package studio

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Slicit/componium/internal/cip"
)

/* Telling a board to replace its firmware.
 *
 * The studio is the only thing holding both the image and the secret, which is
 * why it is the thing that can do this. Two of the ways it could go wrong end
 * with somebody on a ladder: sending the wrong part of the package, and sending
 * an address the board cannot reach. Both are tested here.
 */

// packaged writes a firmware directory shaped like a real one.
//
// Parts in the order make-web-install.sh emits them, which is the order that
// matters: the application is last and the bootloader is first, so anything
// picking the first part picks the one that bricks a board.
func packaged(t *testing.T, app []byte) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, body []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("bootloader.bin", []byte("not the application"))
	write("partition-table.bin", []byte("nor this"))
	write("otadata.bin", []byte("nor this either"))
	write("componium-node-esp32.bin", app)
	write(firmwareManifest, []byte(`{"name":"Componium node","version":"v1.2.3","builds":[{"parts":[
		{"path":"bootloader.bin","offset":4096},
		{"path":"partition-table.bin","offset":32768},
		{"path":"otadata.bin","offset":61440},
		{"path":"componium-node-esp32.bin","offset":131072}]}]}`))
	return dir
}

// withPackage is withBoards, plus a package to send and an address to serve it
// from.
func withPackage(t *testing.T, firmware string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "boards.toml")
	score := filepath.Join(dir, "s.componium")
	if err := os.WriteFile(score, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Score: score, Boards: path,
		Firmware: firmware, Addr: "0.0.0.0:8722"})
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestTheImageSentIsTheApplication(t *testing.T) {
	/* An update writes the app slot and nothing else. The bootloader and the
	 * partition table are earlier in flash and cannot be replaced this way,
	 * which is why a layout change still needs a cable, and why sending one of
	 * them here would be an update that ends in a ladder. */
	app := []byte("this is the application")
	s := &Server{firmware: packaged(t, app)}

	path, name, err := s.appImage()
	if err != nil {
		t.Fatal(err)
	}
	if name != "componium-node-esp32.bin" {
		t.Errorf("would have sent %s", name)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(app) {
		t.Errorf("the bytes sent are %q, not the application's", body)
	}
}

func TestAStudioWithNoFirmwareSaysSo(t *testing.T) {
	// Rather than offering a button that fails somewhere further in.
	s := &Server{}
	if _, _, err := s.appImage(); err == nil {
		t.Fatal("offered to send firmware it does not have")
	}
	if _, _, err := (&Server{firmware: t.TempDir()}).appImage(); err == nil {
		t.Fatal("offered to send firmware from an empty directory")
	}
}

func TestTheSignatureIsOfThatImageAndThatBoardsSecret(t *testing.T) {
	/* Per board, not per studio. The signature is the only thing standing
	 * between the board and running whatever answers a URL, so a signature made
	 * over the wrong bytes or the wrong secret has to be a different string. */
	app := []byte("this is the application")
	dir := packaged(t, app)
	image := filepath.Join(dir, "componium-node-esp32.bin")

	got, err := cip.ImageMACOf("one board's secret", image)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("one board's secret"))
	mac.Write(app)
	if want := hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("signed as %s, want %s", got, want)
	}

	other, err := cip.ImageMACOf("another board's secret", image)
	if err != nil {
		t.Fatal(err)
	}
	if other == got {
		t.Error("two different secrets produced one signature")
	}
	bootloader, err := cip.ImageMACOf("one board's secret", filepath.Join(dir, "bootloader.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bootloader == got {
		t.Error("two different images produced one signature")
	}
}

func TestAnUnknownBoardIsNotUpdated(t *testing.T) {
	s, _ := withPackage(t, packaged(t, []byte("app")))
	w := do(t, s, "POST", "/api/boards/update", `{"board":"nothing like it"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("said %d: %s", w.Code, w.Body.String())
	}
}

func TestABoardWithNoStoredSecretCannotBeUpdated(t *testing.T) {
	/* An update is the largest thing this protocol can ask for, and the secret
	 * is the whole of the permission to ask for it. */
	s, _ := withPackage(t, packaged(t, []byte("app")))
	do(t, s, "PUT", "/api/boards", `{"boards":[{"name":"bare","addr":"192.168.1.50"}]}`)

	w := do(t, s, "POST", "/api/boards/update", `{"board":"bare"}`)
	if w.Code == http.StatusOK {
		t.Fatal("updated a board with no secret")
	}
	if !strings.Contains(w.Body.String(), "secret") {
		t.Errorf("refused for the wrong reason: %s", w.Body.String())
	}
}

func TestTheBoardIsSentAnAddressItCanActuallyReach(t *testing.T) {
	/* Asked of the routing table, not of the browser. A studio is very often
	 * reached through an ssh tunnel at localhost, and telling a board on a shelf
	 * to fetch from localhost is telling it to fetch from itself. */
	host, err := reachableFrom("192.0.2.10:5570")
	if err != nil {
		t.Skipf("this machine has no route off itself: %v", err)
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() {
		t.Errorf("would have told a board to fetch from %q", host)
	}
}

func TestABoardOnlyReachableOverLoopbackIsRefused(t *testing.T) {
	// Better a refusal than an address that means something only here.
	if _, err := reachableFrom("127.0.0.1:5570"); err == nil {
		t.Error("offered a loopback address as somewhere a board could fetch from")
	}
}

func TestTheUrlCarriesTheStudiosOwnPort(t *testing.T) {
	/* Not the port the request arrived on. A request that came down an ssh
	 * tunnel arrived on whatever port the far end used, and that number means
	 * nothing on this side. */
	got, err := firmwareURL("192.168.1.20", "0.0.0.0:8722", "componium-node-esp32.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://192.168.1.20:8722/firmware/componium-node-esp32.bin"; got != want {
		t.Errorf("would have sent the board to %s, want %s", got, want)
	}
	if _, err := firmwareURL("192.168.1.20", "", "app.bin"); err == nil {
		t.Error("built a url out of a studio that does not know its own port")
	}
}

func TestEverythingBeforeTheAddressWorksAgainstARealBoard(t *testing.T) {
	/* A board with a secret, a package to send, and a running node: the handler
	 * gets all the way to working out an address, and the address is the thing
	 * that stops it, because a node in a test is on loopback. Which is the
	 * honest limit of what can be tested from here. The rest of this path is
	 * covered against real firmware in internal/emulated. */
	const secret = "correct horse battery staple"
	n := startNode(t, secret)

	s, _ := withPackage(t, packaged(t, []byte("this is the application")))
	if w := do(t, s, "PUT", "/api/boards",
		`{"boards":[{"name":"bench","addr":"`+n.Addr()+`","secret":"`+secret+`"}]}`); w.Code != http.StatusOK {
		t.Fatalf("saving the board said %d: %s", w.Code, w.Body.String())
	}

	w := do(t, s, "POST", "/api/boards/update", `{"board":"bench"}`)
	if w.Code == http.StatusOK {
		t.Fatalf("sent a board a loopback url: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "loopback") {
		t.Errorf("stopped earlier than expected, for: %s", w.Body.String())
	}
}

func TestTheStudioSaysWhichBuildItWouldSend(t *testing.T) {
	// So somebody can tell whether the update is worth the board going away.
	s := &Server{firmware: packaged(t, []byte("app"))}
	if got := s.firmwareVersion(); got != "v1.2.3" {
		t.Errorf("version came back as %q", got)
	}
	if got := (&Server{}).firmwareVersion(); got != "" {
		t.Errorf("a studio with no firmware claims version %q", got)
	}
}

func TestUpdatingIsNotSomethingAPageCanDoByAccident(t *testing.T) {
	// A GET is what a browser does on its own. This one restarts a board.
	s, _ := withPackage(t, packaged(t, []byte("app")))
	if w := do(t, s, "GET", "/api/boards/update", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("a GET said %d", w.Code)
	}
}

func TestThePageIsToldWhatCouldBeSentOverTheNetwork(t *testing.T) {
	/* The button is only worth offering when there is an application to send,
	 * and only worth pressing when somebody can see which build it is. A
	 * package with a manifest but no application is still flashable over USB,
	 * so it says available and offers nothing to update with. */
	s := &Server{firmware: packaged(t, []byte("this is the application"))}
	var got struct {
		Available bool   `json:"available"`
		Version   string `json:"version"`
		Bytes     int64  `json:"bytes"`
		AppBytes  int64  `json:"appBytes"`
	}
	if err := json.Unmarshal(ask(s, "/api/firmware").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Version != "v1.2.3" {
		t.Errorf("said %+v", got)
	}
	if got.AppBytes != int64(len("this is the application")) {
		t.Errorf("the application is %d bytes here, said %d", len("this is the application"), got.AppBytes)
	}
	if got.AppBytes >= got.Bytes {
		t.Errorf("the application (%d) is not smaller than the package (%d), so one "+
			"of the two numbers is counting the wrong thing", got.AppBytes, got.Bytes)
	}
}

func TestAStudioThatWasToldItsAddressUsesIt(t *testing.T) {
	/* The container case, and the reason this exists at all. Inside one, the
	 * routing table offers a bridge address that only the other containers can
	 * reach, and it is not loopback, so nothing about it looks wrong from in
	 * there. Being told beats looking, always: whoever set the flag could see
	 * both ends. */
	got, err := fetchFrom("192.168.1.67", "127.0.0.1:5570", "0.0.0.0:8722", "app.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://192.168.1.67:8722/firmware/app.bin"; got != want {
		t.Errorf("would have sent the board to %s, want %s", got, want)
	}
}

func TestBeingToldBeatsTheLoopbackRefusal(t *testing.T) {
	/* The board here is on loopback, which is refused when the studio has to
	 * work the address out. It must not be refused when the studio was told,
	 * because a board reached over a forwarded port is a real arrangement and
	 * the address that works is not the one the socket reveals. */
	if _, err := fetchFrom("", "127.0.0.1:5570", "0.0.0.0:8722", "app.bin"); err == nil {
		t.Fatal("worked out a loopback address and offered it to a board")
	}
	if _, err := fetchFrom("192.168.1.67", "127.0.0.1:5570", "0.0.0.0:8722", "app.bin"); err != nil {
		t.Errorf("refused an address it was given: %v", err)
	}
}

func TestAPublishedPortCanDifferFromTheListeningOne(t *testing.T) {
	// docker -p 9000:8722, which is somebody else's decision to make.
	got, err := fetchFrom("192.168.1.67:9000", "127.0.0.1:5570", "0.0.0.0:8722", "app.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://192.168.1.67:9000/firmware/app.bin"; got != want {
		t.Errorf("came out as %s, want %s", got, want)
	}
}

func TestAnAdvertisedAddressThatIsNotAnAddressIsRefused(t *testing.T) {
	/* Refused here rather than discovered as a board that took the instruction
	 * and then fetched nothing, which is the failure this whole flag exists to
	 * prevent and is invisible from the studio. */
	for _, bad := range []string{"http://192.168.1.67:8722", "192.168.1.67/firmware", " "} {
		if url, err := fetchFrom(bad, "127.0.0.1:5570", "0.0.0.0:8722", "app.bin"); err == nil {
			t.Errorf("accepted %q and built %s", bad, url)
		}
	}
}

func TestAnIPv6StudioCanBeAdvertised(t *testing.T) {
	// Bracketed, which is how it has to be written anyway, and how it has to
	// come back out or the port is ambiguous.
	got, err := fetchFrom("[fd00::1]", "127.0.0.1:5570", "0.0.0.0:8722", "app.bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://[fd00::1]:8722/firmware/app.bin"; got != want {
		t.Errorf("came out as %s, want %s", got, want)
	}
}
