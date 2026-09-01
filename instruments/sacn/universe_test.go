package sacn

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// Reported as: ambient works, event does not.
//
// A DMX universe is 512 channels and carries several fixtures. E1.31 sends all
// of them in every packet, so a fixture that owned its own buffer transmitted
// every other fixture's channels as zero, and the one sending more often won.
// A wash on a curve track at 50Hz against an event light on a cue is not a
// fight: it is a wash, and an event light that does nothing at all.

// listener returns a UDP socket and its address.
func listener(t *testing.T) (*net.UDPConn, string) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, conn.LocalAddr().String()
}

// next reads one packet and returns its DMX slots.
func next(t *testing.T, conn *net.UDPConn) [Slots]byte {
	t.Helper()
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	return p.Data
}

func flash(l *Light, r, g, b float64) instrument.Dispatch {
	return instrument.Dispatch{Cue: instrument.Cue{
		Instrument: l.cfg.ID, Action: "flash",
		Params: map[string]float64{"r": r, "g": g, "b": b},
	}}
}

func TestTwoFixturesOnOneUniverseDoNotEraseEachOther(t *testing.T) {
	conn, addr := listener(t)
	u, err := Dial(1, addr, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()

	wash, err := On(u, Config{ID: "light.ambient", Universe: 1, Start: 1, Mode: ModeRGB})
	if err != nil {
		t.Fatal(err)
	}
	event, err := On(u, Config{ID: "light.event", Universe: 1, Start: 4, Mode: ModeRGB})
	if err != nil {
		t.Fatal(err)
	}

	if err := wash.Dispatch(flash(wash, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	next(t, conn)

	if err := event.Dispatch(flash(event, 0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	got := next(t, conn)

	// Both are in the frame. Before the universe was shared, the second
	// fixture's packet carried the first one's channels as zero.
	if got[0] != 255 || got[1] != 0 || got[2] != 0 {
		t.Errorf("the wash was erased: %v", got[0:3])
	}
	if got[3] != 0 || got[4] != 0 || got[5] != 255 {
		t.Errorf("the event light did not land: %v", got[3:6])
	}
}

func TestAWashSendingConstantlyDoesNotBlankTheEventLight(t *testing.T) {
	// The symptom exactly: a curve track writes fifty times a second, and the
	// event light's channels have to survive every one of those frames.
	conn, addr := listener(t)
	u, err := Dial(1, addr, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()

	wash, _ := On(u, Config{ID: "a", Universe: 1, Start: 1, Mode: ModeRGB})
	event, _ := On(u, Config{ID: "b", Universe: 1, Start: 4, Mode: ModeRGB})

	event.Dispatch(flash(event, 0, 1, 0))
	next(t, conn)
	for i := 0; i < 5; i++ {
		wash.Dispatch(flash(wash, 0.5, 0.5, 0.5))
		got := next(t, conn)
		if got[4] != 255 {
			t.Fatalf("frame %d blanked the event light: %v", i, got[3:6])
		}
	}
}

func TestOffOnlyTurnsOffItsOwnChannels(t *testing.T) {
	conn, addr := listener(t)
	u, _ := Dial(1, addr, "test")
	defer u.Close()

	wash, _ := On(u, Config{ID: "a", Universe: 1, Start: 1, Mode: ModeRGB})
	event, _ := On(u, Config{ID: "b", Universe: 1, Start: 4, Mode: ModeRGB})
	wash.Dispatch(flash(wash, 1, 1, 1))
	next(t, conn)
	event.Dispatch(flash(event, 1, 1, 1))
	next(t, conn)

	event.Dispatch(instrument.Dispatch{Cue: instrument.Cue{Action: "off"}})
	got := next(t, conn)
	if got[0] != 255 {
		t.Errorf("turning one fixture off took the other with it: %v", got[0:6])
	}
	if got[3] != 0 || got[4] != 0 || got[5] != 0 {
		t.Errorf("it did not go off: %v", got[3:6])
	}
}

func TestKeepaliveKeepsTalking(t *testing.T) {
	/* E1.31 receivers drop back to idle after about two and a half seconds of
	 * silence. A cue driven light sets its colour once, so without this it
	 * goes dark on the receiver's timer rather than on the score's. Nothing
	 * called this until it was found on a bench. */
	conn, addr := listener(t)
	u, _ := Dial(1, addr, "test")
	defer u.Close()

	l, _ := On(u, Config{ID: "a", Universe: 1, Start: 1, Mode: ModeRGB})
	l.Dispatch(flash(l, 1, 0, 0))
	next(t, conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.Keepalive(ctx, 20*time.Millisecond)

	// Two more frames with nobody dispatching anything.
	for i := 0; i < 2; i++ {
		if got := next(t, conn); got[0] != 255 {
			t.Errorf("keepalive sent the wrong state: %v", got[0:3])
		}
	}
}

func TestAFixtureOutsideTheUniverseIsRefused(t *testing.T) {
	u, _ := Dial(1, "127.0.0.1:1", "test")
	defer u.Close()
	if _, err := On(u, Config{ID: "a", Universe: 1, Start: 511, Mode: ModeRGB}); err == nil {
		t.Error("accepted a fixture that runs off the end of the universe")
	}
	if _, err := On(u, Config{ID: "", Universe: 1, Start: 1, Mode: ModeRGB}); err == nil {
		t.Error("accepted a fixture with no id")
	}
}

func TestOneFixtureStillDialsItsOwn(t *testing.T) {
	// Everything that had a single light keeps working.
	conn, addr := listener(t)
	l, err := New(Config{ID: "a", Universe: 1, Addr: addr, Start: 1, Mode: ModeRGB})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Dispatch(flash(l, 0, 1, 0))
	if got := next(t, conn); got[1] != 255 {
		t.Errorf("%v", got[0:3])
	}
}

func TestUniversesAreKeyedByWhereTheyGo(t *testing.T) {
	if Key(1, "10.0.0.1:5568") == Key(2, "10.0.0.1:5568") {
		t.Error("two universes at one address share a key")
	}
	if Key(1, "10.0.0.1:5568") == Key(1, "10.0.0.2:5568") {
		t.Error("one universe at two addresses shares a key")
	}
	// An empty address means the conventional multicast group, so it must key
	// the same as naming that group explicitly.
	if Key(1, "") != Key(1, MulticastAddr(1)) {
		t.Error("the default address keys differently from itself")
	}
}
