package cip_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

// Telling a board what is attached to it.
//
// The message that moves latency out of firmware, and the one with the largest
// blast radius: it decides which pin a relay is on. Most of what is worth
// testing is what it refuses.

func configurable(t *testing.T) (*cip.Node, *cip.Client) {
	t.Helper()
	n := startNode(t, cip.NodeConfig{Secret: secret, Timeout: 5 * time.Second})
	c, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return n, c
}

func TestConfiguringABoardFromNothing(t *testing.T) {
	// What a freshly flashed board is: no devices, reachable, waiting.
	n, c := configurable(t)
	if got := c.Names(); len(got) != 0 {
		t.Fatalf("a new board announced %v", got)
	}

	err := c.Configure([]cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 18, Kind: "wind",
			FreqHz: 25000, LatencyMS: 1200, RampUpMS: 1800},
		{ID: "light.strip", Type: cip.DeviceWS28xx, GPIO: 5, Kind: "light",
			Pixels: 30, LatencyMS: 20},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The node announced again, and the client is holding the new ones.
	if got := c.Names(); len(got) != 2 || got[0] != "wind.main" {
		t.Fatalf("client holds %v", got)
	}
	if got := n.Announced(); len(got) != 2 {
		t.Fatalf("node holds %v", got)
	}
	// And the physical facts made the trip, which is the entire point.
	fan, ok := c.Device("wind.main")
	if !ok {
		t.Fatal("no fan")
	}
	if got := fan.Manifest().Latency; got != 1200*time.Millisecond {
		t.Errorf("latency came back as %v", got)
	}
	if got := fan.Manifest().Ramp.Up; got != 1800*time.Millisecond {
		t.Errorf("ramp came back as %v", got)
	}
}

func TestReconfiguringMovesTheIndices(t *testing.T) {
	/* The reason an index is only good for the session that announced it. The
	 * device at index 0 is a different device afterwards, and a conductor
	 * holding the old one would drive the wrong output with nothing in the room
	 * to show for it. */
	_, c := configurable(t)
	if err := c.Configure([]cip.Device{
		{ID: "a.one", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
		{ID: "b.two", Type: cip.DevicePWM, GPIO: 19, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}
	first, _ := c.Device("a.one")
	if first.Index() != 0 {
		t.Fatalf("a.one is at %d", first.Index())
	}

	if err := c.Configure([]cip.Device{
		{ID: "b.two", Type: cip.DevicePWM, GPIO: 19, Kind: "wind"},
		{ID: "a.one", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}
	moved, _ := c.Device("a.one")
	if moved.Index() != 1 {
		t.Errorf("a.one is at %d after being reordered, want 1", moved.Index())
	}
}

func TestAConfigurationTheBoardCannotUse(t *testing.T) {
	// Refused whole, with the reason, and the board keeps what it had.
	n, c := configurable(t)
	if err := c.Configure([]cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []struct {
		why     string
		devices []cip.Device
		says    string
	}{
		{"no id", []cip.Device{{Type: cip.DevicePWM, GPIO: 18}}, "no id"},
		{"a type this build has not got",
			[]cip.Device{{ID: "x", Type: "steam", GPIO: 18}}, "device type"},
		{"two devices on one pin", []cip.Device{
			{ID: "a", Type: cip.DevicePWM, GPIO: 18},
			{ID: "b", Type: cip.DevicePWM, GPIO: 18},
		}, "gpio"},
		{"two devices with one name", []cip.Device{
			{ID: "a", Type: cip.DevicePWM, GPIO: 18},
			{ID: "a", Type: cip.DevicePWM, GPIO: 19},
		}, "two devices"},
	} {
		t.Run(bad.why, func(t *testing.T) {
			err := c.Configure(bad.devices)
			if err == nil {
				t.Fatal("accepted it")
			}
			if !strings.Contains(err.Error(), bad.says) {
				t.Errorf("said %q, wanted something about %q", err, bad.says)
			}
			// And nothing changed: half a configuration is a board that looks
			// set up and is not.
			if got := n.Announced(); len(got) != 1 || got[0] != "wind.main" {
				t.Errorf("the board now holds %v", got)
			}
		})
	}
}

func TestConfiguringWithoutTheSecret(t *testing.T) {
	/* A stranger who can write a configuration can move a relay onto a pin
	 * nobody intended, or declare a latency of zero and corrupt the timing of
	 * every cue after it. So the requirement follows from the capability. */
	n := startNode(t, cip.NodeConfig{Secret: secret, Timeout: 5 * time.Second})

	// Without the secret a node ignores everything, including hello, so this
	// does not even get as far as a connection.
	if _, err := cip.Dial(n.Addr(), 300*time.Millisecond, ""); err == nil {
		t.Fatal("reached a node that requires a secret without one")
	}

	// And a client that could reach an unauthenticated node still refuses to
	// configure it, rather than sending into a silence.
	open := startNode(t, cip.NodeConfig{Timeout: 5 * time.Second})
	c, err := cip.Dial(open.Addr(), time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Configure([]cip.Device{{ID: "a", Type: cip.DevicePWM, GPIO: 18}})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Errorf("configured an unauthenticated node: %v", err)
	}
}

func TestConfiguringNothingEmptiesTheBoard(t *testing.T) {
	// An ordinary thing to want: take everything off and start again.
	n, c := configurable(t)
	if err := c.Configure([]cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.Configure(nil); err != nil {
		t.Fatal(err)
	}
	if got := n.Announced(); len(got) != 0 {
		t.Errorf("the board still holds %v", got)
	}
}
