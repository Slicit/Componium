package cip_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

/* A board that takes its time announcing what it now has.
 *
 * Configure promises that when it returns, the client's devices and their
 * indices are the new ones. On loopback that promise costs nothing: a real node
 * sends the acknowledgement and the fresh hello back to back, and the hello has
 * always landed before anyone looks. Deleting the wait changed no test, which
 * meant the guarantee was not being tested at all, only observed.
 *
 * A board on a switch, applying a configuration by tearing down every output
 * and bringing new ones up, is not that fast. This one is deliberately not
 * either. It speaks just enough of the protocol to be dialled and configured,
 * and it delays the announcement the way real hardware does.
 *
 * The index is the whole reason to care. A conductor holding an index from
 * before a reconfiguration is holding a way to drive the wrong output, and
 * there is nothing in the room to show for it: the fan that should not be
 * running is a fan that is running, and the one that should be is silent.
 */
type slowBoard struct {
	conn  *net.UDPConn
	auth  *cip.Auth
	delay time.Duration

	// devices is what it currently claims, in order. Replaced on configure.
	devices []cip.Device
}

func newSlowBoard(t *testing.T, secret string, delay time.Duration) *slowBoard {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	b := &slowBoard{conn: conn, auth: cip.NewAuth(secret), delay: delay}
	t.Cleanup(func() { conn.Close() })
	go b.serve()
	return b
}

func (b *slowBoard) addr() string { return b.conn.LocalAddr().String() }

func (b *slowBoard) hello() *cip.Message {
	m := &cip.Message{
		Type:        cip.TypeHello,
		Node:        cip.NodeInfo{Name: "slow", Firmware: cip.Version, Chip: "test"},
		Instruments: []cip.Instrument{},
	}
	for i, d := range b.devices {
		m.Instruments = append(m.Instruments, cip.Instrument{
			Index: i, ID: d.ID, Kind: d.Kind, LatencyMS: d.LatencyMS,
		})
	}
	return m
}

func (b *slowBoard) send(m *cip.Message, to *net.UDPAddr) {
	body, err := cip.Encode(m)
	if err != nil {
		return
	}
	b.conn.WriteToUDP(b.auth.Wrap(body), to)
}

func (b *slowBoard) serve() {
	buf := make([]byte, 2048)
	for {
		n, from, err := b.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		body, err := b.auth.Unwrap(buf[:n])
		if err != nil {
			continue
		}
		m, err := cip.Decode(body)
		if err != nil {
			continue
		}
		switch m.Type {
		case cip.TypeHello:
			b.send(b.hello(), from)
		case cip.TypeConfigure:
			// Accepted immediately, announced late. Exactly the window a real
			// board opens while it is writing flash and restarting outputs.
			b.devices = m.Devices
			b.send(&cip.Message{Type: cip.TypeAck, Seq: m.Seq}, from)
			go func(to *net.UDPAddr, hello *cip.Message) {
				time.Sleep(b.delay)
				b.send(hello, to)
			}(from, b.hello())
		}
	}
}

func TestConfigureWaitsForTheBoardToReannounce(t *testing.T) {
	const delay = 400 * time.Millisecond
	b := newSlowBoard(t, secret, delay)

	c, err := cip.Dial(b.addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Configure([]cip.Device{
		{ID: "a.one", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
		{ID: "b.two", Type: cip.DevicePWM, GPIO: 19, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}

	// Reversed, so a stale view and a fresh one disagree about every index
	// rather than about none of them.
	start := time.Now()
	if err := c.Configure([]cip.Device{
		{ID: "b.two", Type: cip.DevicePWM, GPIO: 19, Kind: "wind"},
		{ID: "a.one", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}

	// It cannot have returned before the announcement it is required to have
	// waited for. Without the wait this is a few hundred microseconds.
	if took := time.Since(start); took < delay {
		t.Errorf("returned in %v, before the board announced at %v", took, delay)
	}

	// And the indices are the new ones, checked the instant it returns rather
	// than after a sleep that would hide the whole problem.
	moved, ok := c.Device("a.one")
	if !ok {
		t.Fatal("the client no longer knows a.one")
	}
	if moved.Index() != 1 {
		t.Errorf("a.one is at index %d after being moved, want 1", moved.Index())
	}
}

func TestConfigureSaysSoWhenTheBoardNeverReannounces(t *testing.T) {
	/* Applied, and the client's idea of the indices is now the stale one. That
	 * is worth an error rather than a silent success, because the caller is
	 * about to drive by index and would be driving the wrong thing. */
	b := newSlowBoard(t, secret, time.Hour)

	c, err := cip.Dial(b.addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.Configure([]cip.Device{
		{ID: "a.one", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
	})
	if err == nil {
		t.Fatal("claimed a configuration the board never confirmed")
	}
	if got := err.Error(); !strings.Contains(got, "re-announce") {
		t.Errorf("said %q, which does not say what actually happened", got)
	}
}
