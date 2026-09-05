package emulated

import (
	"strings"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

/* The firmware, driven over a real socket.
 *
 * Every test here is aimed at something that has actually been wrong, and every
 * one of those was invisible to a Go node talking to a Go client.
 */

func TestABoardAnnouncesItself(t *testing.T) {
	c := dial(t)
	if got := c.Info().Firmware; got != cip.Version {
		t.Errorf("the board speaks %q and this client speaks %q", got, cip.Version)
	}
	if got := c.Info().Chip; got != "ESP32" {
		t.Errorf("chip came back as %q", got)
	}
}

func TestEveryConfiguredFieldSurvivesTheBoard(t *testing.T) {
	/* The fault: a configuration could be written and not read back, so the
	 * page filled in what the board had not said and a strip on one pin showed
	 * as a fan on another. Here against the parser and the flash that actually
	 * store it. */
	c := configured(t)

	fan, ok := c.Device("wind.main")
	if !ok {
		t.Fatalf("no fan; the board has %v", c.Names())
	}
	w := fan.Wiring()
	if w.Type != string(cip.DevicePWM) || w.GPIO != 19 || w.FreqHz != 18000 {
		t.Errorf("the fan came back as %+v", w)
	}
	if w.Safe != 0.25 {
		t.Errorf("the safe value came back as %v", w.Safe)
	}
	if fan.Manifest().Latency != 1234*time.Millisecond {
		t.Errorf("latency came back as %v", fan.Manifest().Latency)
	}

	fog, _ := c.Device("fog.left")
	if fw := fog.Wiring(); fw.Type != string(cip.DeviceRelay) || fw.GPIO != 23 || fw.Active != "low" {
		t.Errorf("the fogger came back as %+v", fw)
	}
}

func TestAConfigurationSurvivesAFreshConnection(t *testing.T) {
	// Written to flash, not held in memory. This is the read somebody does
	// tomorrow, and the one the page does every time it opens a board.
	configured(t)

	again, err := cip.Dial(board.cip, dialTimeout, secret())
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	fan, ok := again.Device("wind.main")
	if !ok {
		t.Fatalf("the board forgot its devices; it has %v", again.Names())
	}
	if w := fan.Wiring(); w.GPIO != 19 || w.FreqHz != 18000 {
		t.Errorf("a fresh connection reports %+v", w)
	}
}

func TestSeveralDevicesOnOneBoardAreAddressedByName(t *testing.T) {
	/* What ADR 0007 is for. Two devices, two types, two pins, told apart by the
	 * name a score uses. */
	c := configured(t)
	beat(t, c)
	if got := c.Names(); len(got) != 2 {
		t.Fatalf("the board announced %v", got)
	}

	cue(t, c, "wind.main", "gust", map[string]float64{"intensity": 0.8}, 0)
	if !holding(t, "wind.main", "0.80") {
		t.Errorf("the fan is not holding what it was sent:\n%s", devices(t))
	}
	// And the other device was not touched by a cue addressed to the first.
	if !holding(t, "fog.left", "safe") {
		t.Errorf("a cue for the fan moved the fogger:\n%s", devices(t))
	}
}

func TestAStopEndsTheSpan(t *testing.T) {
	/* The one reported as "the on and the colour are well timed and the off
	 * happens whenever it wants". A stop carries no values, the cue path read
	 * only the parameters, so the output kept everything it had and the span
	 * could not end. It was not a timing fault: nothing was ending the flash. */
	c := configured(t)
	beat(t, c)

	cue(t, c, "wind.main", "gust", map[string]float64{"intensity": 0.9}, 0)
	if !holding(t, "wind.main", "0.90") {
		t.Fatalf("the fan never started:\n%s", devices(t))
	}

	cue(t, c, "wind.main", "stop", nil, 0)
	if !holding(t, "wind.main", "safe") {
		t.Errorf("the fan is still running after a stop:\n%s", devices(t))
	}
}

func TestASpanEndsOnItsOwnWhenTheStopNeverArrives(t *testing.T) {
	/* The other half of ending a span, and the one that matters when the
	 * conductor dies mid show: the board was told how long, and holds itself to
	 * it without being reminded. */
	c := configured(t)
	beat(t, c)

	cue(t, c, "wind.main", "gust", map[string]float64{"intensity": 0.7}, 700*time.Millisecond)
	if !holding(t, "wind.main", "0.70") {
		t.Fatalf("the fan never started:\n%s", devices(t))
	}
	if !eventually(t, func() bool { return has(t, "wind.main", "safe") }, settleWindow) {
		t.Errorf("the span outlived the hold it was given:\n%s", devices(t))
	}
}

func TestTheBoardGoesSafeWhenTheConductorStopsTalking(t *testing.T) {
	/* The reason this protocol exists. A fan that keeps running because the
	 * thing driving it crashed is the failure the whole design is arranged
	 * around, and it is checked here against the watchdog that actually runs. */
	c := configured(t)
	cue(t, c, "wind.main", "gust", map[string]float64{"intensity": 1.0}, 0)
	if !holding(t, "wind.main", "1.00") {
		t.Fatalf("the fan never started:\n%s", devices(t))
	}

	/* A conductor first, then a dead one. The board does not arm this
	 * watchdog until it has heard at least one heartbeat, deliberately: a
	 * board nobody has ever spoken to is not a board whose conductor has
	 * died, and tripping on silence that was always silent would have every
	 * idle board logging itself safe for ever. So this has to be a
	 * conductor before it can stop being one. */
	quiet := beat(t, c)
	time.Sleep(300 * time.Millisecond)

	// And now the conductor stops, without saying goodbye, which is what
	// a crash is.
	quiet()

	c.Close()

	if !eventually(t, func() bool { return has(t, "wind.main", "safe") }, settleWindow) {
		t.Errorf("the board kept driving an output with nobody talking to it:\n%s", devices(t))
	}
	if !board.waitFor("no heartbeat", settleWindow) {
		t.Error("the board went safe without saying why")
	}
}

func TestABoardIgnoresUnsignedTraffic(t *testing.T) {
	// It has a secret, so anything without one is not for it.
	if _, err := cip.Dial(board.cip, 2*time.Second, ""); err == nil {
		t.Error("a board with a secret answered a client without one")
	}
}

func TestABoardIgnoresTheWrongSecret(t *testing.T) {
	if _, err := cip.Dial(board.cip, 2*time.Second, secret()+" not"); err == nil {
		t.Error("a board answered the wrong secret")
	}
}

func TestTheBoardSaysWhatItRefused(t *testing.T) {
	/* Refusing in silence is right for one datagram and wrong for a hundred:
	 * two separate faults presented as a board that simply said nothing, and
	 * each cost a packet capture to tell apart from a board that was not
	 * listening. */
	_, _ = cip.Dial(board.cip, 2*time.Second, "definitely not the secret")
	if !board.waitFor("refused", settleWindow) {
		t.Error("the board refused traffic without ever saying so")
	}
}

/* ------------------------------------------------------------ the page */

func TestThePageNeedsTheSecret(t *testing.T) {
	if code, _ := page(t, "", ""); code != 401 {
		t.Errorf("the page answered %d without a secret", code)
	}
	if code, _ := page(t, "someone", "not the secret"); code != 401 {
		t.Errorf("the page answered %d to the wrong secret", code)
	}
}

func TestThePageShowsWhatTheBoardIsDoing(t *testing.T) {
	/* It was blank once, for a stylesheet built through a formatter whose
	 * buffer was a third of its length: the tag never closed and the browser
	 * read the document as CSS. Everything else was working perfectly. */
	configured(t)

	code, body := page(t, "any", secret())
	if code != 200 {
		t.Fatalf("the page answered %d", code)
	}
	if !strings.Contains(body, "</html>") {
		t.Errorf("the page is truncated; it is %d bytes and does not end", len(body))
	}
	/* The tag that was actually missing. The page was never empty: the
	 * stylesheet went through a formatter whose buffer was a third of its
	 * length, so <style> never closed and the browser read the rest of the
	 * document as CSS. Every word below was present the whole time, which
	 * is why looking for words does not find this. */
	if !strings.Contains(body, "</style>") {
		t.Error("the stylesheet is not closed; a browser reads everything after " +
			"it as CSS and shows a blank page")
	}
	if head := strings.Index(body, "</head>"); head < 0 || !strings.Contains(body[head:], "wind.main") {
		t.Error("the devices are not in the body; the head swallowed them")
	}

	for _, want := range []string{"wind.main", "fog.left", "Componium node", "stack spare"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page never mentions %q", want)
		}
	}
}

func TestThePageReportsAnOutputThatIsRunning(t *testing.T) {
	// The diagnostic somebody actually wants, and a second opinion on every
	// assertion above: what the board says it is holding, from the board.
	c := configured(t)
	beat(t, c)
	cue(t, c, "wind.main", "gust", map[string]float64{"intensity": 0.5}, 0)

	if !holding(t, "wind.main", "running") {
		t.Errorf("the page does not show the fan running:\n%s", devices(t))
	}
	cue(t, c, "wind.main", "stop", nil, 0)
	if !holding(t, "wind.main", "safe") {
		t.Errorf("the page still shows the fan running after a stop:\n%s", devices(t))
	}
}

/* ------------------------------------------------------------- reading */

// row returns the page's line for one device.
func row(t *testing.T, id string) string {
	t.Helper()
	_, body := page(t, "any", secret())
	for _, line := range strings.Split(strings.ReplaceAll(body, "<tr>", "\n<tr>"), "\n") {
		if strings.Contains(line, ">"+id+"<") {
			return line
		}
	}
	return ""
}

func devices(t *testing.T) string {
	t.Helper()
	return row(t, "wind.main") + "\n" + row(t, "fog.left")
}

func has(t *testing.T, id, want string) bool {
	t.Helper()
	return strings.Contains(row(t, id), want)
}

// holding waits for a device to show something, because a cue crosses an
// emulated network and is applied by an interpreted core.
func holding(t *testing.T, id, want string) bool {
	t.Helper()
	return eventually(t, func() bool { return has(t, id, want) }, settleWindow)
}

func eventually(t *testing.T, ok func() bool, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}
