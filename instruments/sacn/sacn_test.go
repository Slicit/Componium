package sacn

import (
	"net"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

func TestPacketRoundTrips(t *testing.T) {
	p := &Packet{SourceName: "componium", Universe: 7, Priority: 120, Sequence: 42}
	p.Data[0] = 255
	p.Data[511] = 9
	copy(p.CID[:], []byte("0123456789abcdef"))

	got, err := Parse(p.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if got.Universe != 7 || got.Priority != 120 || got.Sequence != 42 {
		t.Errorf("header mismatch: %+v", got)
	}
	if got.SourceName != "componium" {
		t.Errorf("source name %q", got.SourceName)
	}
	if got.Data[0] != 255 || got.Data[511] != 9 {
		t.Errorf("slot data mismatch: [0]=%d [511]=%d", got.Data[0], got.Data[511])
	}
	if got.CID != p.CID {
		t.Error("CID mismatch")
	}
}

func TestPacketIsTheStandardLength(t *testing.T) {
	// E1.31-2018: 38 root + 77 framing + 523 DMP.
	if n := len((&Packet{}).Marshal()); n != 638 {
		t.Errorf("packet is %d bytes, want 638", n)
	}
}

func TestParseRejectsRubbish(t *testing.T) {
	if _, err := Parse([]byte("nope")); err == nil {
		t.Error("short packet accepted")
	}
	b := (&Packet{}).Marshal()
	b[5] = 'X' // corrupt the ACN identifier
	if _, err := Parse(b); err == nil {
		t.Error("packet with a bad ACN identifier accepted")
	}
}

func TestMulticastAddrFollowsConvention(t *testing.T) {
	if got := MulticastAddr(1); got != "239.255.0.1:5568" {
		t.Errorf("universe 1 maps to %q", got)
	}
	if got := MulticastAddr(300); got != "239.255.1.44:5568" {
		t.Errorf("universe 300 maps to %q", got)
	}
}

// listen gives us a real UDP socket to send at, so the instrument is exercised
// over the wire rather than through a seam invented for the test.
func listen(t *testing.T) (*net.UDPConn, string) {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, c.LocalAddr().String()
}

func recvPacket(t *testing.T, c *net.UDPConn) *Packet {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := c.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no packet received: %v", err)
	}
	p, err := Parse(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLightMapsColourOntoChannels(t *testing.T) {
	conn, addr := listen(t)
	l, err := New(Config{ID: "light.ambient", Universe: 1, Addr: addr, Start: 10, Mode: ModeRGB})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	err = l.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "set",
		Params: map[string]float64{"r": 1, "g": 180.0 / 255.0, "b": 0},
	}})
	if err != nil {
		t.Fatal(err)
	}

	p := recvPacket(t, conn)
	// Start address 10 is 1-based, so it is index 9.
	if p.Data[9] != 255 {
		t.Errorf("red channel %d, want 255", p.Data[9])
	}
	if p.Data[10] != 180 {
		t.Errorf("green channel %d, want 180", p.Data[10])
	}
	if p.Data[11] != 0 {
		t.Errorf("blue channel %d, want 0", p.Data[11])
	}
	if p.Data[8] != 0 || p.Data[12] != 0 {
		t.Error("wrote outside the fixture's own channels")
	}
}

func TestLightClampsRatherThanWraps(t *testing.T) {
	conn, addr := listen(t)
	l, _ := New(Config{ID: "l", Universe: 1, Addr: addr, Start: 1, Mode: ModeRGB})
	defer l.Close()

	// 1.5 is a mistake. Wrapping would make it dark and hide the mistake.
	l.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Params: map[string]float64{"r": 1.5, "g": -0.5, "b": 0.5},
	}})
	p := recvPacket(t, conn)
	if p.Data[0] != 255 {
		t.Errorf("over-range clamped to %d, want 255", p.Data[0])
	}
	if p.Data[1] != 0 {
		t.Errorf("negative clamped to %d, want 0", p.Data[1])
	}
	if p.Data[2] != 128 {
		t.Errorf("0.5 mapped to %d, want 128", p.Data[2])
	}
}

func TestSequenceNumberAdvances(t *testing.T) {
	conn, addr := listen(t)
	l, _ := New(Config{ID: "l", Universe: 1, Addr: addr, Start: 1, Mode: ModeDimmer})
	defer l.Close()

	d := instrument.Dispatch{Cue: instrument.Cue{Params: map[string]float64{"intensity": 1}}}
	l.Dispatch(d)
	l.Dispatch(d)
	first := recvPacket(t, conn)
	second := recvPacket(t, conn)
	if second.Sequence != first.Sequence+1 {
		t.Errorf("sequence went %d then %d", first.Sequence, second.Sequence)
	}
}

func TestOffClearsTheFixture(t *testing.T) {
	conn, addr := listen(t)
	l, _ := New(Config{ID: "l", Universe: 1, Addr: addr, Start: 5, Mode: ModeRGBW})
	defer l.Close()

	l.Dispatch(instrument.Dispatch{Cue: instrument.Cue{
		Action: "set", Params: map[string]float64{"r": 1, "g": 1, "b": 1, "w": 1}}})
	recvPacket(t, conn)
	l.Dispatch(instrument.Dispatch{Cue: instrument.Cue{Action: "off"}})
	p := recvPacket(t, conn)
	for i := 4; i < 8; i++ {
		if p.Data[i] != 0 {
			t.Errorf("channel %d is %d after off, want 0", i+1, p.Data[i])
		}
	}
}

func TestRejectsFixtureThatDoesNotFit(t *testing.T) {
	if _, err := New(Config{ID: "l", Start: 511, Mode: ModeRGBW}); err == nil {
		t.Error("accepted a 4 channel fixture starting at 511")
	}
	if _, err := New(Config{ID: "l", Start: 0, Mode: ModeRGB}); err == nil {
		t.Error("accepted start address 0, but DMX is 1 based")
	}
}
