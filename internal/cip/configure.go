package cip

import (
	"fmt"
	"time"
)

// Telling a board what is attached to it.
//
// The message that moves latency out of firmware. Until ADR 0007 a node's
// declared dead time was compiled in, so measuring a fan meant editing C and
// reflashing, and the shipped guess stayed a guess. This is how the real number
// gets in.
//
// It requires a secret, and that requirement follows from the capability rather
// than from the hardware: a stranger who can write a configuration can move a
// relay onto a pin nobody intended, or declare a latency of zero and corrupt
// the timing of every cue after it in a way that reads as the score being wrong
// rather than as an attack.

// ConfigureTimeout is how long a node has to answer.
//
// Longer than a cue's, because the node writes to flash, tears down every
// output and brings the new ones up before it replies. Still bounded: a board
// that has not answered in this long has not understood.
const ConfigureTimeout = 5 * time.Second

// Configure replaces what the node believes is attached to it.
//
// On success the node sends a fresh hello, which readLoop adopts, so the
// client's devices and their indices are the new ones by the time this returns.
// Anything holding an index from before is holding a way to drive the wrong
// output.
func (c *Client) Configure(devices []Device) error {
	if !c.auth.Enabled() {
		// Said here rather than discovered as a silence. A node that requires
		// a secret ignores unauthenticated traffic entirely, so a client
		// without one would simply wait out its timeout and report nothing
		// useful about why.
		return fmt.Errorf("cip: configuring a node needs its secret")
	}

	c.mu.Lock()
	c.seq++
	seq := c.seq
	ch := make(chan string, 1)
	c.acks[seq] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.acks, seq)
		c.mu.Unlock()
	}()

	b, err := Encode(&Message{
		Type: TypeConfigure, Seq: seq, N: c.next(), Devices: devices,
	})
	if err != nil {
		return err
	}
	if err := c.send(b); err != nil {
		return err
	}

	select {
	case why := <-ch:
		if why != "" {
			// The node's own words. It refuses a configuration whole and says
			// which part of it was the problem, which is the sentence somebody
			// needs rather than "rejected".
			return fmt.Errorf("cip: %s", why)
		}
	case <-time.After(ConfigureTimeout):
		// Not retried. A configuration is not a cue: sending it again might
		// apply it twice, and the second application tears down outputs that
		// the first one had brought up.
		return fmt.Errorf("cip: no answer to the configuration within %v", ConfigureTimeout)
	}

	// The node re-announces after applying, and readLoop adopts it. Waiting for
	// that to land means a caller reading Devices immediately afterwards sees
	// the new ones rather than the old.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.matches(devices) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Applied, and the announcement has not arrived. Worth saying, because the
	// client's idea of the indices is now the stale one.
	return fmt.Errorf("cip: configuration accepted, but the node has not "+
		"re-announced within 2s; it now has %v", c.Names())
}

// matches reports whether the node's devices are the ones just sent.
//
// The wiring, not just the names. A reconfiguration usually keeps the names:
// moving a fan from one pin to another leaves the list reading wind.main,
// light.ambient, so a comparison of names is satisfied by the announcement that
// was already in flight before the change. Configure then returned holding the
// old pins, which reads as a board that ignored what it was told.
//
// A node announcing no wiring at all is a node from before ADR 0007, and there
// is no question about pins to ask it. Names are the whole of what it can
// answer, so names are what it is held to.
func (c *Client) matches(devices []Device) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.devices) != len(devices) {
		return false
	}
	for i, d := range devices {
		got := c.devices[i]
		if got.manifest.ID != d.ID {
			return false
		}
		w := got.announced
		if w.Type == "" {
			continue // says nothing about wiring, so nothing to check
		}
		if w.Type != d.Type || w.GPIO != d.GPIO {
			return false
		}
		// The one setting per type that changes what the output physically is.
		switch d.Type {
		case DeviceWS28xx:
			if d.Pixels != 0 && w.Pixels != d.Pixels {
				return false
			}
		case DevicePWM:
			if d.FreqHz != 0 && w.FreqHz != d.FreqHz {
				return false
			}
		case DeviceRelay:
			if d.Active != "" && w.Active != d.Active {
				return false
			}
		}
	}
	return true
}
