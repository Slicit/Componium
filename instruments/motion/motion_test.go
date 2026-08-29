package motion

import (
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Slicit/Componium/internal/instrument"
)

func listen(t *testing.T) (*net.UDPConn, string) {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, c.LocalAddr().String()
}

func recv(t *testing.T, c *net.UDPConn) string {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	n, _, err := c.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no packet: %v", err)
	}
	return strings.TrimSpace(string(buf[:n]))
}

func pose(params map[string]float64) instrument.Dispatch {
	return instrument.Dispatch{Cue: instrument.Cue{Action: "set", Params: params}}
}

func TestSendsPoseAsCSV(t *testing.T) {
	conn, addr := listen(t)
	p, err := New(Config{ID: "motion.seat", Addr: addr,
		Limits: Limits{Surge: 1, Sway: 1, Heave: 1, Roll: 30, Pitch: 30, Yaw: 30}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if err := p.Dispatch(pose(map[string]float64{
		"surge": 0.1, "sway": -0.2, "heave": 0.05,
		"roll": 3, "pitch": -2.5, "yaw": 1,
	})); err != nil {
		t.Fatal(err)
	}
	got := recv(t, conn)
	want := "0.10000,-0.20000,0.05000,3.000,-2.500,1.000"
	if got != want {
		t.Errorf("sent %q, want %q", got, want)
	}
}

func TestLabelledFormatNamesTheAxes(t *testing.T) {
	conn, addr := listen(t)
	p, _ := New(Config{ID: "m", Addr: addr, Format: FormatLabelled,
		Limits: Limits{Surge: 1, Sway: 1, Heave: 1, Roll: 30, Pitch: 30, Yaw: 30}})
	defer p.Close()

	p.Dispatch(pose(map[string]float64{"heave": 0.25}))
	got := recv(t, conn)
	if !strings.Contains(got, "heave=0.25000") {
		t.Errorf("labelled output %q does not name heave", got)
	}
}

// A platform commanded beyond its travel does not politely refuse. It drives
// into its end stops.
func TestPoseIsClampedToDeclaredTravel(t *testing.T) {
	conn, addr := listen(t)
	p, _ := New(Config{ID: "m", Addr: addr,
		Limits: Limits{Surge: 0.1, Sway: 0.1, Heave: 0.1, Roll: 5, Pitch: 5, Yaw: 5}})
	defer p.Close()

	p.Dispatch(pose(map[string]float64{"surge": 99, "roll": -99}))
	got := recv(t, conn)
	if !strings.HasPrefix(got, "0.10000,") {
		t.Errorf("surge was not clamped: %q", got)
	}
	if !strings.Contains(got, "-5.000") {
		t.Errorf("roll was not clamped: %q", got)
	}
	if p.Clamped() != 1 {
		t.Errorf("clamped count %d, want 1", p.Clamped())
	}
}

// A score that clamps constantly was written for a different rig, and the
// operator should be told rather than left wondering.
func TestClampingIsCounted(t *testing.T) {
	conn, addr := listen(t)
	p, _ := New(Config{ID: "m", Addr: addr, Limits: Limits{Surge: 1, Sway: 1, Heave: 1, Roll: 1, Pitch: 1, Yaw: 1}})
	defer p.Close()

	for i := 0; i < 3; i++ {
		p.Dispatch(pose(map[string]float64{"surge": 50}))
		recv(t, conn)
	}
	p.Dispatch(pose(map[string]float64{"surge": 0.5}))
	recv(t, conn)

	if p.Clamped() != 3 {
		t.Errorf("clamped %d of 4 poses, want 3", p.Clamped())
	}
	if p.Sent() != 4 {
		t.Errorf("sent %d, want 4", p.Sent())
	}
}

func TestNaNBecomesZeroRatherThanGarbage(t *testing.T) {
	conn, addr := listen(t)
	p, _ := New(Config{ID: "m", Addr: addr, Limits: Limits{Surge: 1, Sway: 1, Heave: 1, Roll: 1, Pitch: 1, Yaw: 1}})
	defer p.Close()

	p.Dispatch(pose(map[string]float64{"surge": math.NaN()}))
	got := recv(t, conn)
	if !strings.HasPrefix(got, "0.00000,") {
		t.Errorf("NaN produced %q", got)
	}
}

func TestSafeActionCommandsNeutral(t *testing.T) {
	conn, addr := listen(t)
	p, _ := New(Config{ID: "m", Addr: addr, Limits: Limits{Surge: 1, Sway: 1, Heave: 1, Roll: 30, Pitch: 30, Yaw: 30}})
	defer p.Close()

	p.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "safe", Params: map[string]float64{"surge": 0.9, "roll": 20},
	}})
	got := recv(t, conn)
	if got != "0.00000,0.00000,0.00000,0.000,0.000,0.000" {
		t.Errorf("safe sent %q, want neutral", got)
	}
}

// A rig that has not declared its travel should move timidly, not confidently.
func TestUndeclaredLimitsAreSmall(t *testing.T) {
	conn, addr := listen(t)
	p, _ := New(Config{ID: "m", Addr: addr})
	defer p.Close()

	p.Dispatch(pose(map[string]float64{"heave": 10}))
	got := recv(t, conn)
	if !strings.Contains(got, "0.05000") {
		t.Errorf("default limits let %q through", got)
	}
}

func TestRefusesConfigurationItCannotUse(t *testing.T) {
	if _, err := New(Config{Addr: "127.0.0.1:1"}); err == nil {
		t.Error("accepted a platform with no ID")
	}
	if _, err := New(Config{ID: "m"}); err == nil {
		t.Error("accepted a platform with no address")
	}
}

func TestSafeStateIsNeutralOnEveryAxis(t *testing.T) {
	p, _ := New(Config{ID: "m", Addr: "127.0.0.1:1"})
	defer p.Close()
	for axis, v := range p.Manifest().SafeState {
		if v != 0 {
			t.Errorf("safe state %s is %v, want 0", axis, v)
		}
	}
}
