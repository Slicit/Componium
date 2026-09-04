package cip

import (
	"fmt"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// One device on a node, as the conductor holds it.
//
// A Client is a board and a Remote is one thing plugged into it. That split is
// the same one the sACN package arrived at when a Light stopped owning a whole
// universe: the transport is shared, the instruments are not. A board with a
// fan and a strip on it is one socket, one heartbeat and one watchdog, with two
// instruments the conductor cannot tell from any others.
//
// Before ADR 0007 the Client was the instrument, which is why two rig entries
// at one address came back as the same instrument and the rig refused to start.
type Remote struct {
	// announced is what the node said about this device, kept whole.
	//
	// The manifest below says how to drive it; this says how it is wired, which
	// is a different question and one only the board can answer. A studio
	// without it has to invent a pin, and an invented pin looks exactly like a
	// board that forgot its configuration.
	announced Instrument

	client   *Client
	manifest instrument.Manifest
	// index is what curve frames address, and is only good for the session
	// that announced it. A node that reboots says hello again with fresh
	// indices; anything holding an old one is holding a way to drive the wrong
	// output with nothing in the room to show for it.
	index int
}

// Manifest is what the node said about this device.
func (r *Remote) Manifest() instrument.Manifest { return r.manifest }

// Index is this device's position on its node, for a caller assembling a
// bundled curve frame.
func (r *Remote) Index() int { return r.index }

// Node is the board this device is on, for a caller that needs to heartbeat it
// or take the whole thing safe.
func (r *Remote) Node() *Client { return r.client }

// Dispatch sends a cue and waits for the node to acknowledge it.
//
// Retrying rather than firing and forgetting matters because a lost cue is
// invisible: the effect simply never happens, and nothing in the room explains
// why. A cue that genuinely cannot be delivered becomes an error the conductor
// records as a skip.
func (r *Remote) Dispatch(d instrument.Dispatch) error {
	c := r.client
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

	// Named rather than indexed. A cue is rare, is read by a person in a log,
	// and survives a node reconfiguring itself between the conductor deciding
	// to send one and the node receiving it.
	msg := &Message{
		Type: TypeCue, Seq: seq, N: c.next(),
		Instrument: r.manifest.ID,
		Action:     d.Cue.Action,
		HoldMS:     Ms(d.Cue.Hold),
		Params:     d.Cue.Params,
	}
	b, err := Encode(msg)
	if err != nil {
		return err
	}

	for attempt := 0; attempt < c.retries; attempt++ {
		if err := c.send(b); err != nil {
			return err
		}
		select {
		case why := <-ch:
			if why != "" {
				// The node refused it, most often enforcing a limit of its own.
				// Retrying would not change its mind.
				return fmt.Errorf("cip: %s refused %s: %s",
					r.manifest.ID, d.Cue.Action, why)
			}
			return nil
		case <-time.After(c.timeout):
		}
	}
	return fmt.Errorf("cip: %s did not acknowledge %s after %d attempts",
		r.manifest.ID, d.Cue.Action, c.retries)
}

// SendCurve sends this one device's values.
//
// A bundle of one. Correct, and wasteful if several devices on the same board
// are being driven at once: see Client.SendBundle, which is what a curve driver
// with more than one output on a node should be reaching for.
func (r *Remote) SendCurve(values []float32) error {
	return r.client.SendBundle([]Outputs{{Index: r.index, Values: values}})
}

var _ instrument.Instrument = (*Remote)(nil)

// Wiring is what the node said about how this device is attached.
//
// Zero valued on a node that announced nothing about it, which is every node
// built before ADR 0007. Type empty is the way to tell that apart from a device
// genuinely on GPIO 0.
func (r *Remote) Wiring() Instrument { return r.announced }
