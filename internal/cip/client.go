package cip

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Slicit/Componium/internal/instrument"
)

// DefaultAckTimeout is how long the client waits for a node to acknowledge a
// cue before retrying.
const DefaultAckTimeout = 40 * time.Millisecond

// DefaultRetries is how many times a cue is retried before it is given up on.
//
// Three attempts at 40ms is 120ms in the worst case, which is well inside the
// latency of any instrument slow enough to be reached over the network.
const DefaultRetries = 3

// Client is the conductor's side of a remote instrument.
//
// It satisfies instrument.Instrument, so the conductor cannot tell the
// difference between a fan on the other side of the room and one in this
// process. That is the point of the protocol.
type Client struct {
	conn     net.Conn
	manifest instrument.Manifest

	mu      sync.Mutex
	seq     uint32
	acks    map[uint32]chan struct{}
	retries int
	timeout time.Duration
}

// Dial connects to a node and waits for it to introduce itself.
//
// The manifest comes from the node rather than from local configuration,
// because the node is the only thing that actually knows its own latency, and
// a rig file that disagrees with the hardware is worse than no rig file.
func Dial(addr string, wait time.Duration) (*Client, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("cip: dial %s: %w", addr, err)
	}
	c := &Client{
		conn: conn, acks: map[uint32]chan struct{}{},
		retries: DefaultRetries, timeout: DefaultAckTimeout,
	}

	// Ask, rather than wait: a node that booted before the conductor should
	// not have to keep shouting.
	hello, _ := Encode(&Message{Type: TypeHello})
	if _, err := conn.Write(hello); err != nil {
		conn.Close()
		return nil, err
	}

	conn.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 2048)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("cip: no hello from %s within %v: %w", addr, wait, err)
		}
		m, err := Decode(buf[:n])
		if err != nil || m.Type != TypeHello || m.Manifest == nil {
			continue
		}
		c.manifest = m.Manifest.toInstrument()
		conn.SetReadDeadline(time.Time{})
		welcome, _ := Encode(&Message{Type: TypeWelcome})
		conn.Write(welcome)
		go c.readLoop()
		return c, nil
	}
}

func (m *Manifest) toInstrument() instrument.Manifest {
	return instrument.Manifest{
		ID:            m.ID,
		Kind:          m.Kind,
		Latency:       m.LatencyMS.Duration(),
		Ramp:          instrument.Ramp{Up: m.RampUpMS.Duration(), Down: m.RampDownMS.Duration()},
		MaxContinuous: m.MaxContinuous.Duration(),
		DutyCycle:     m.DutyCycle,
		SafeState:     m.SafeState,
	}
}

func (c *Client) Manifest() instrument.Manifest { return c.manifest }

func (c *Client) Close() error { return c.conn.Close() }

// readLoop delivers acknowledgements to whoever is waiting for them.
func (c *Client) readLoop() {
	buf := make([]byte, 2048)
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			return
		}
		m, err := Decode(buf[:n])
		if err != nil || m.Type != TypeAck {
			continue
		}
		c.mu.Lock()
		if ch, ok := c.acks[m.Seq]; ok {
			close(ch)
			delete(c.acks, m.Seq)
		}
		c.mu.Unlock()
	}
}

// Dispatch sends a cue and waits for the node to acknowledge it.
//
// Retrying rather than firing and forgetting matters because a lost cue is
// invisible: the effect simply never happens, and nothing in the room explains
// why. A cue that genuinely cannot be delivered becomes an error the conductor
// records as a skip.
func (c *Client) Dispatch(d instrument.Dispatch) error {
	c.mu.Lock()
	c.seq++
	seq := c.seq
	ch := make(chan struct{})
	c.acks[seq] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.acks, seq)
		c.mu.Unlock()
	}()

	msg := &Message{
		Type: TypeCue, Seq: seq,
		Instrument: d.Cue.Instrument,
		Action:     d.Cue.Action,
		Params:     d.Cue.Params,
	}
	b, err := Encode(msg)
	if err != nil {
		return err
	}

	for attempt := 0; attempt < c.retries; attempt++ {
		if _, err := c.conn.Write(b); err != nil {
			return err
		}
		select {
		case <-ch:
			return nil
		case <-time.After(c.timeout):
		}
	}
	return fmt.Errorf("cip: %s did not acknowledge %s after %d attempts",
		c.manifest.ID, d.Cue.Action, c.retries)
}

// SendCurve sends one curve frame, unacknowledged.
func (c *Client) SendCurve(values []float32) error {
	_, err := c.conn.Write(MarshalCurve(values))
	return err
}

// Heartbeat tells the node the conductor is alive. A node that stops hearing
// this drives itself safe without being asked, which is the whole point.
func (c *Client) Heartbeat() error {
	b, _ := Encode(&Message{Type: TypeHeartbeat})
	_, err := c.conn.Write(b)
	return err
}

// Safe orders an immediate return to the safe state.
func (c *Client) Safe() error {
	b, _ := Encode(&Message{Type: TypeSafe})
	_, err := c.conn.Write(b)
	return err
}
