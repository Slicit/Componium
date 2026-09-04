package cip_test

import (
	"net"
	"testing"
	"time"

	"github.com/Slicit/componium/internal/cip"
)

/* A board that answers with the configuration it had a moment ago.
 *
 * Found on hardware: writing a fan onto GPIO 18 came back reporting GPIO 19,
 * and reading the board a second later showed 18. It had done exactly as it was
 * told and the reply was one announcement out of date, which from a page is
 * indistinguishable from a board that ignored the write.
 *
 * Configure waited for the node to re-announce and decided it had by comparing
 * names, and a reconfiguration usually keeps the names. So the stale
 * announcement satisfied the check.
 *
 * This board sends the old announcement first and the new one after, which is
 * the ordering that actually happens when a hello is already on the wire.
 */
type staleBoard struct {
	conn *net.UDPConn
	auth *cip.Auth

	devices []cip.Device
	delay   time.Duration
}

func newStaleBoard(t *testing.T, secret string, delay time.Duration, start []cip.Device) *staleBoard {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	b := &staleBoard{conn: conn, auth: cip.NewAuth(secret), delay: delay, devices: start}
	t.Cleanup(func() { conn.Close() })
	go b.serve()
	return b
}

func (b *staleBoard) addr() string { return b.conn.LocalAddr().String() }

func (b *staleBoard) hello(of []cip.Device) *cip.Message {
	m := &cip.Message{
		Type:        cip.TypeHello,
		Node:        cip.NodeInfo{Name: "stale", Firmware: cip.Version, Chip: "test"},
		Instruments: []cip.Instrument{},
	}
	for i, d := range of {
		m.Instruments = append(m.Instruments, cip.Instrument{
			Index: i, ID: d.ID, Kind: d.Kind,
			Type: d.Type, GPIO: d.GPIO, Pixels: d.Pixels, FreqHz: d.FreqHz,
			Active: d.Active,
		})
	}
	return m
}

func (b *staleBoard) send(m *cip.Message, to *net.UDPAddr) {
	body, err := cip.Encode(m)
	if err != nil {
		return
	}
	b.conn.WriteToUDP(b.auth.Wrap(body), to)
}

func (b *staleBoard) serve() {
	buf := make([]byte, 4096)
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
			b.send(b.hello(b.devices), from)
		case cip.TypeConfigure:
			was := b.devices
			b.devices = m.Devices
			b.send(&cip.Message{Type: cip.TypeAck, Seq: m.Seq}, from)
			// The announcement that was already on its way, describing the
			// board as it was a moment ago. Same names, old pins.
			b.send(b.hello(was), from)
			go func(to *net.UDPAddr, now []cip.Device) {
				time.Sleep(b.delay)
				b.send(b.hello(now), to)
			}(from, m.Devices)
		}
	}
}

func TestConfigureIgnoresTheAnnouncementFromBefore(t *testing.T) {
	before := []cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 19, Kind: "wind", FreqHz: 18000},
		{ID: "light.ambient", Type: cip.DeviceWS28xx, GPIO: 5, Kind: "light", Pixels: 30},
	}
	b := newStaleBoard(t, secret, 300*time.Millisecond, before)

	c, err := cip.Dial(b.addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// The same devices by name, on a different pin. Which is the ordinary edit:
	// somebody fixes the pin and leaves everything else alone.
	after := []cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 18, Kind: "wind", FreqHz: 25000},
		{ID: "light.ambient", Type: cip.DeviceWS28xx, GPIO: 5, Kind: "light", Pixels: 30},
	}
	if err := c.Configure(after); err != nil {
		t.Fatal(err)
	}

	fan, ok := c.Device("wind.main")
	if !ok {
		t.Fatal("no fan")
	}
	if w := fan.Wiring(); w.GPIO != 18 || w.FreqHz != 25000 {
		t.Errorf("Configure returned with the board on gpio %d at %dHz, which is "+
			"what it was before the write", w.GPIO, w.FreqHz)
	}
}

func TestConfigureStillWorksWithABoardThatAnnouncesNoWiring(t *testing.T) {
	/* A node from before ADR 0007 cannot answer a question about pins, so names
	 * are the whole of what it can be held to. Waiting for wiring it will never
	 * send would hang every configuration against one. */
	n := startNode(t, cip.NodeConfig{
		Manifest: fanManifest(), Secret: secret, Timeout: 5 * time.Second,
	})
	c, err := cip.Dial(n.Addr(), time.Second, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Configure([]cip.Device{
		{ID: "wind.main", Type: cip.DevicePWM, GPIO: 18, Kind: "wind"},
	}); err != nil {
		t.Fatal(err)
	}
}
