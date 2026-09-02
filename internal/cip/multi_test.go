package cip_test

import (
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
	"github.com/Slicit/componium/internal/instrument"
)

// A node carrying several devices, which is what ADR 0007 is for.
//
// All of it against the software node, with no hardware anywhere, which is what
// ADR 0001 asks of every instrument kind and what makes the firmware something
// to check against a design rather than a place to discover one.

func twoDevices() []cip.Manifest {
	return []cip.Manifest{
		{
			ID: "wind.main", Kind: "wind", LatencyMS: 1200,
			SafeState: map[string]float64{"intensity": 0},
			Channels: []cip.Channel{
				{Name: "intensity", Unit: "normalised", Range: [2]float64{0, 1}},
			},
		},
		{
			ID: "light.strip", Kind: "light", LatencyMS: 20,
			SafeState: map[string]float64{"r": 0, "g": 0, "b": 0},
			Channels: []cip.Channel{
				{Name: "r", Unit: "normalised", Range: [2]float64{0, 1}},
				{Name: "g", Unit: "normalised", Range: [2]float64{0, 1}},
				{Name: "b", Unit: "normalised", Range: [2]float64{0, 1}},
			},
		},
	}
}

func board(t *testing.T) (*cip.Node, *cip.Client) {
	t.Helper()
	n := startNode(t, cip.NodeConfig{Devices: twoDevices(), Timeout: 5 * time.Second})
	c, err := cip.Dial(n.Addr(), time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return n, c
}

func device(t *testing.T, c *cip.Client, id string) *cip.Remote {
	t.Helper()
	d, ok := c.Device(id)
	if !ok {
		t.Fatalf("no device %q; the node announced %v", id, c.Names())
	}
	return d
}

func TestANodeAnnouncesEverythingItHas(t *testing.T) {
	_, c := board(t)
	if got := c.Names(); len(got) != 2 || got[0] != "wind.main" || got[1] != "light.strip" {
		t.Fatalf("announced %v", got)
	}
	// Each with its own latency, which is the point: a fan and a strip on one
	// board have wildly different dead times, and a node that could only
	// declare one had to lie about the other.
	if got := device(t, c, "wind.main").Manifest().Latency; got != 1200*time.Millisecond {
		t.Errorf("the fan's latency came back as %v", got)
	}
	if got := device(t, c, "light.strip").Manifest().Latency; got != 20*time.Millisecond {
		t.Errorf("the strip's latency came back as %v", got)
	}
}

func TestACueGoesToTheDeviceItNames(t *testing.T) {
	n, c := board(t)
	err := device(t, c, "light.strip").Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "flash", Params: map[string]float64{"r": 1, "g": 0.5, "b": 0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := n.StateOf("light.strip")["r"]; got != 1 {
		t.Errorf("the strip did not take the cue: %v", got)
	}
	// And nothing else moved.
	if got := n.StateOf("wind.main")["intensity"]; got != 0 {
		t.Errorf("the fan moved when the strip was addressed: %v", got)
	}
}

func TestACueForSomethingTheNodeDoesNotHaveIsNotAcknowledged(t *testing.T) {
	/* Not acknowledged rather than acknowledged and dropped. Acking a cue that
	 * was not applied is a lie, and the conductor's retry and recorded skip is
	 * exactly the machinery for a cue that did not land. */
	n, c := board(t)
	fan := device(t, c, "wind.main")
	err := fan.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "gust", Params: map[string]float64{"intensity": 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cues, _, _, _ := n.Stats()
	if cues != 1 {
		t.Errorf("%d cues landed", cues)
	}
}

func TestOneFrameCarriesEveryOutput(t *testing.T) {
	// The reason bundling exists: two outputs that must move together arrive
	// in one datagram and are applied before it returns.
	n, c := board(t)
	fan := device(t, c, "wind.main")
	strip := device(t, c, "light.strip")

	err := c.SendBundle([]cip.Outputs{
		// A value that survives float32 exactly, so this test is about
		// routing rather than about arithmetic.
		{Index: fan.Index(), Values: []float32{0.75}},
		{Index: strip.Index(), Values: []float32{0, 0, 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return n.StateOf("wind.main")["intensity"] == 0.75 })

	if got := n.StateOf("light.strip")["b"]; got != 1 {
		t.Errorf("the strip did not take the frame: %v", got)
	}
	_, frames, _, _ := n.Stats()
	if frames != 1 {
		t.Errorf("%d frames for two outputs, want one", frames)
	}
}

func TestAnOutputTheNodeDoesNotHaveDoesNotSpoilTheFrame(t *testing.T) {
	/* A frame is fifty times a second and superseded 20ms later, so refusing
	 * all of it because one output has gone would stop the outputs that are
	 * still there for no reason. */
	n, c := board(t)
	err := c.SendBundle([]cip.Outputs{
		{Index: 9, Values: []float32{1}},
		{Index: 0, Values: []float32{0.5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return n.StateOf("wind.main")["intensity"] == 0.5 })
}

func TestTheWatchdogTakesEveryOutput(t *testing.T) {
	/* The conductor is gone. Nothing on the node knows which output is the
	 * dangerous one, so all of them go. */
	n := startNode(t, cip.NodeConfig{Devices: twoDevices(), Timeout: 120 * time.Millisecond})
	c, err := cip.Dial(n.Addr(), time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Heartbeat()
	if err := c.SendBundle([]cip.Outputs{
		{Index: 0, Values: []float32{1}},
		{Index: 1, Values: []float32{1, 1, 1}},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return n.StateOf("wind.main")["intensity"] == 1 })

	// And then silence.
	waitFor(t, func() bool {
		return n.StateOf("wind.main")["intensity"] == 0 &&
			n.StateOf("light.strip")["r"] == 0
	})
	if _, _, trips, safe := n.Stats(); trips == 0 || !safe {
		t.Errorf("trips %d, safe %v", trips, safe)
	}
}

func TestAHoldExpiringTakesOneOutputAndNotTheNode(t *testing.T) {
	/* A four second fog burst ending must not stop a fan in the middle of a
	 * scene. This is the safety rule most easily got wrong by a node that
	 * used to have one output and one hold. */
	n := startNode(t, cip.NodeConfig{Devices: twoDevices(), Timeout: 5 * time.Second})
	c, err := cip.Dial(n.Addr(), time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.Heartbeat()
			}
		}
	}()

	// The fan runs with no ending of its own.
	if err := device(t, c, "wind.main").Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "gust", Params: map[string]float64{"intensity": 1},
	}}); err != nil {
		t.Fatal(err)
	}
	// The strip flashes for a moment.
	if err := device(t, c, "light.strip").Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "flash", Params: map[string]float64{"r": 1},
		Hold: 150 * time.Millisecond,
	}}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return n.StateOf("light.strip")["r"] == 0 })
	if got := n.StateOf("wind.main")["intensity"]; got != 1 {
		t.Fatalf("the fan stopped when the strip's hold expired: %v", got)
	}
	if _, _, trips, _ := n.Stats(); trips != 0 {
		t.Errorf("the watchdog tripped %d times, and heartbeats never stopped", trips)
	}
}

func TestANodeWithNothingConfiguredIsStillANode(t *testing.T) {
	// What every freshly flashed board is. It announces nothing and can still
	// be talked to, because otherwise it could never be told what it has.
	n := startNode(t, cip.NodeConfig{})
	c, err := cip.Dial(n.Addr(), time.Second, "")
	if err != nil {
		t.Fatalf("could not reach a node with no devices: %v", err)
	}
	defer c.Close()
	if got := c.Names(); len(got) != 0 {
		t.Errorf("announced %v", got)
	}
	if _, ok := c.Device("wind.main"); ok {
		t.Error("found a device on a node that has none")
	}
	_ = n
}
