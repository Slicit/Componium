package cip_test

import (
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

/* What a board says about how it is wired.
 *
 * The fault: configuring a strip on GPIO 5 worked, and reading it back showed a
 * PWM output on GPIO 18. Nothing was wrong with the board. A hello carried an
 * index, an id, a kind and a latency, so the studio filled in the rest from the
 * defaults for a new row, and a default that looks like an answer is worse than
 * no answer at all.
 *
 * These assert the physical facts make the round trip, because a configuration
 * you cannot read back is a configuration you cannot edit.
 */

func TestTheWiringComesBackWithTheAnnouncement(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Secret: secret, Timeout: 5 * time.Second})
	c, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.Configure([]cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 18, Kind: "wind",
			FreqHz: 25000, LatencyMS: 1200},
		{ID: "light.strip", Type: cip.DeviceWS28xx, GPIO: 5, Kind: "light",
			Pixels: 30, LatencyMS: 20},
		{ID: "fog.left", Type: cip.DeviceRelay, GPIO: 21, Kind: "fog",
			Active: "low", LatencyMS: 2000},
	})
	if err != nil {
		t.Fatal(err)
	}

	strip, ok := c.Device("light.strip")
	if !ok {
		t.Fatal("no strip")
	}
	w := strip.Wiring()
	if w.Type != string(cip.DeviceWS28xx) {
		t.Errorf("the strip came back as type %q", w.Type)
	}
	if w.GPIO != 5 {
		t.Errorf("the strip came back on gpio %d, not the 5 it was given", w.GPIO)
	}
	if w.Pixels != 30 {
		t.Errorf("pixels came back as %d", w.Pixels)
	}

	fan, _ := c.Device("wind.main")
	if fw := fan.Wiring(); fw.Type != string(cip.DevicePWM) || fw.GPIO != 18 || fw.FreqHz != 25000 {
		t.Errorf("the fan came back as %+v", fw)
	}

	fog, _ := c.Device("fog.left")
	if rw := fog.Wiring(); rw.Type != string(cip.DeviceRelay) || rw.GPIO != 21 || rw.Active != "low" {
		t.Errorf("the fogger came back as %+v", rw)
	}
}

func TestTheWiringSurvivesAReconnect(t *testing.T) {
	/* The case that actually bit: configure, then come back later and read.
	 * A client that only knows the wiring because it just sent it would pass
	 * the test above and fail the thing somebody does. */
	n := startNode(t, cip.NodeConfig{Secret: secret, Timeout: 5 * time.Second})

	first, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Configure([]cip.Device{
		{ID: "light.strip", Type: cip.DeviceWS28xx, GPIO: 5, Kind: "light", Pixels: 60},
	}); err != nil {
		t.Fatal(err)
	}
	first.Close()

	again, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	strip, ok := again.Device("light.strip")
	if !ok {
		t.Fatal("the board forgot the strip")
	}
	w := strip.Wiring()
	if w.Type != string(cip.DeviceWS28xx) || w.GPIO != 5 || w.Pixels != 60 {
		t.Errorf("a fresh connection reports %+v, not the strip that was configured", w)
	}
}

func TestANodeThatSaysNothingAboutWiringIsDistinguishable(t *testing.T) {
	/* A node configured from a compiled-in manifest rather than over CIP has no
	 * pins to report, and must not appear to claim GPIO 0. Empty type is how
	 * the difference is told, which is why the studio keys on it. */
	n := startNode(t, cip.NodeConfig{
		Manifest: fanManifest(), Secret: secret, Timeout: 5 * time.Second,
	})
	c, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	d := only(t, c)
	if w := d.Wiring(); w.Type != "" {
		t.Errorf("a node with no configuration claimed to be wired as %+v", w)
	}
}
