package cip_test

import (
	"context"
	"testing"
	"time"

	"github.com/Slicit/Componium/internal/cip"
	"github.com/Slicit/Componium/internal/instrument"
)

func fanManifest() cip.Manifest {
	return cip.Manifest{
		ID: "wind.main", Kind: "wind",
		LatencyMS: cip.Ms(1200 * time.Millisecond),
		RampUpMS:  cip.Ms(1800 * time.Millisecond),
		SafeState: map[string]float64{"intensity": 0},
		Channels:  []cip.Channel{{Name: "intensity", Unit: "normalised", Range: [2]float64{0, 1}}},
	}
}

// startNode runs a node and returns it plus a cancel.
func startNode(t *testing.T, cfg cip.NodeConfig) *cip.Node {
	t.Helper()
	n, err := cip.NewNode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go n.Run(ctx)
	t.Cleanup(func() { cancel(); n.Close() })
	return n
}

func TestCurveFrameRoundTrips(t *testing.T) {
	in := []float32{0, 0.5, 1, -1}
	out, err := cip.UnmarshalCurve(cip.MarshalCurve(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d values, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("value %d is %v, want %v", i, out[i], in[i])
		}
	}
}

func TestCurveFrameRejectsRubbish(t *testing.T) {
	if _, err := cip.UnmarshalCurve([]byte{1, 2, 3, 4}); err == nil {
		t.Error("accepted a frame with the wrong magic")
	}
	b := cip.MarshalCurve([]float32{1, 2})
	if _, err := cip.UnmarshalCurve(b[:len(b)-1]); err == nil {
		t.Error("accepted a truncated frame")
	}
}

// A future protocol version must be refused rather than half understood.
func TestDecodeRejectsAnotherProtocolVersion(t *testing.T) {
	b := []byte(`{"v":"99.0","t":"cue"}`)
	if _, err := cip.Decode(b); err == nil {
		t.Error("accepted a message from a version this build does not speak")
	}
}

// The manifest comes from the node, because the node is the only thing that
// actually knows its own latency.
func TestClientLearnsTheManifestFromTheNode(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Manifest: fanManifest()})
	c, err := cip.Dial(n.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	m := c.Manifest()
	if m.ID != "wind.main" {
		t.Errorf("id %q", m.ID)
	}
	if m.Latency != 1200*time.Millisecond {
		t.Errorf("latency %v, want 1.2s", m.Latency)
	}
	if m.SafeState["intensity"] != 0 {
		t.Errorf("safe state %v", m.SafeState)
	}
}

func TestCueIsDeliveredAndAcknowledged(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Manifest: fanManifest()})
	c, err := cip.Dial(n.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Instrument: "wind.main", Action: "gust",
		Params: map[string]float64{"intensity": 0.8},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := n.State()["intensity"]; got != 0.8 {
		t.Errorf("node intensity %v, want 0.8", got)
	}
	cues, _, _, _ := n.Stats()
	if cues != 1 {
		t.Errorf("node saw %d cues, want 1", cues)
	}
}

// A lost cue is invisible: the effect simply never happens and nothing in the
// room explains why. Undeliverable cues must become errors.
func TestUndeliverableCueBecomesAnError(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Manifest: fanManifest()})
	c, err := cip.Dial(n.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	n.Close() // the node goes away

	err = c.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Instrument: "wind.main", Action: "gust",
		Params: map[string]float64{"intensity": 1},
	}})
	if err == nil {
		t.Error("a cue to a dead node reported success")
	}
}

func TestCurveFramesReachTheNode(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Manifest: fanManifest()})
	c, err := cip.Dial(n.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for i := 0; i < 5; i++ {
		if err := c.SendCurve([]float32{0.42}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, curves, _, _ := n.Stats(); curves >= 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, curves, _, _ := n.Stats(); curves < 5 {
		t.Fatalf("node received %d curve frames, want 5", curves)
	}
	// Curve frames carry float32, so 0.42 comes back as 0.41999998.
	// Seven significant digits is far more than any physical output
	// resolves, and halving the frame size at 50Hz per instrument is
	// worth more than digits nothing can act on.
	if got := n.State()["intensity"]; got < 0.4199 || got > 0.4201 {
		t.Errorf("node intensity %v after curve frames, want about 0.42", got)
	}
}

// The behaviour the whole protocol exists to guarantee.
func TestNodeGoesSafeOnItsOwnWhenHeartbeatsStop(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Manifest: fanManifest(), Timeout: 120 * time.Millisecond})
	c, err := cip.Dial(n.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Heartbeat()
	c.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Instrument: "wind.main", Action: "gust",
		Params: map[string]float64{"intensity": 1},
	}})
	if got := n.State()["intensity"]; got != 1 {
		t.Fatalf("node did not accept the cue, intensity %v", got)
	}

	// Stop talking to it. Nothing else changes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, trips, safe := n.Stats(); trips > 0 && safe {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _, trips, safe := n.Stats()
	if trips == 0 || !safe {
		t.Fatalf("node did not go safe after heartbeats stopped: trips=%d safe=%v", trips, safe)
	}
	if got := n.State()["intensity"]; got != 0 {
		t.Errorf("intensity %v after going safe, want 0", got)
	}
}

func TestExplicitSafeCommand(t *testing.T) {
	n := startNode(t, cip.NodeConfig{Manifest: fanManifest()})
	c, err := cip.Dial(n.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "gust", Params: map[string]float64{"intensity": 1}}})
	c.Safe()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if n.State()["intensity"] == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := n.State()["intensity"]; got != 0 {
		t.Errorf("intensity %v after an explicit safe, want 0", got)
	}
}

func TestMillisRoundsRatherThanTruncates(t *testing.T) {
	if got := cip.Ms(1499 * time.Microsecond); got != 1 {
		t.Errorf("1.499ms became %d, want 1", got)
	}
	if got := cip.Ms(1501 * time.Microsecond); got != 2 {
		t.Errorf("1.501ms became %d, want 2", got)
	}
}
