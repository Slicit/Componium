package cip

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Slicit/componium/internal/instrument"
)

// DefaultAckTimeout is how long the client waits for a node to acknowledge a
// cue before retrying.
const DefaultAckTimeout = 40 * time.Millisecond

// DefaultRetries is how many times a cue is retried before it is given up on.
//
// Three attempts at 40ms is 120ms in the worst case, well inside the latency of
// any instrument slow enough to be reached over a network.
const DefaultRetries = 3

// Client is the conductor's side of a node: a board, not an instrument.
//
// A board carries several devices, so what satisfies instrument.Instrument is a
// Remote, one per device, all sharing this connection. One socket, one
// heartbeat, one watchdog, several instruments the conductor cannot tell from
// any others. That is the point of the protocol, and before ADR 0007 it was
// also why two rig entries at one address came back as the same instrument.
type Client struct {
	conn net.Conn
	node NodeInfo
	// devices in the order the node announced them, which is the order their
	// indices refer to.
	devices []*Remote

	mu      sync.Mutex
	seq     uint32
	counter uint64
	acks    map[uint32]chan struct{}
	retries int
	timeout time.Duration
	auth    *Auth
}

// Dial connects to a node and waits for it to say what is attached to it.
//
// The manifests come from the node rather than from local configuration,
// because the node is the only thing that actually knows its own latency, and
// a rig file that disagrees with the hardware is worse than no rig file. Since
// ADR 0007 that list is also what the node was configured with, which is what
// turns latency from a number compiled into firmware into one a person who has
// measured their fan can set.
//
// secret may be empty, in which case traffic is unauthenticated. A node
// configured with a secret will ignore an unauthenticated client entirely,
// which presents as no hello arriving. Any node that accepts configuration
// requires one; see docs/cip.md.
func Dial(addr string, wait time.Duration, secret string) (*Client, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("cip: dial %s: %w", addr, err)
	}
	c := &Client{
		conn: conn, acks: map[uint32]chan struct{}{},
		retries: DefaultRetries, timeout: DefaultAckTimeout,
		auth: NewAuth(secret),
	}

	// Ask rather than wait: a node that booted before the conductor should not
	// have to keep shouting.
	hello, _ := Encode(&Message{Type: TypeHello, N: c.next()})
	if err := c.send(hello); err != nil {
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
		body, err := c.auth.Unwrap(buf[:n])
		if err != nil {
			continue
		}
		m, err := Decode(body)
		if err != nil || m.Type != TypeHello {
			continue
		}
		if !c.adopt(m) {
			continue
		}
		conn.SetReadDeadline(time.Time{})
		welcome, _ := Encode(&Message{Type: TypeWelcome, N: c.next()})
		c.send(welcome)
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

// adopt takes the instrument list out of a hello, in either version.
//
// A 0.2 node sends one manifest and no list. It becomes a single device at
// index 0, which is what it was addressing all along, because a firmware
// upgrade should not be the price of a conductor upgrade.
//
// A node with nothing configured sends an empty list and that is accepted: it
// is the state every freshly flashed board is in, and refusing to connect to
// one would mean it could never be configured.
func (c *Client) adopt(m *Message) bool {
	c.node = m.Node
	c.devices = nil
	switch m.Version {
	case Version:
		// The list is the truth, including when it is empty: a board with
		// nothing attached announces nothing, and has to remain reachable or
		// it can never be told what it has.
		for _, in := range m.Instruments {
			c.devices = append(c.devices, &Remote{
				client: c, index: in.Index, manifest: in.toInstrument(),
			})
		}
		return true
	case Version02:
		if m.Manifest == nil {
			return false
		}
		c.devices = []*Remote{{
			client: c, index: 0, manifest: m.Manifest.toInstrument(),
		}}
		return true
	}
	return false
}

// Info is what the node says about itself.
func (c *Client) Info() NodeInfo { return c.node }

// Devices are the instruments on this node, in the order it announced them.
func (c *Client) Devices() []*Remote {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*Remote(nil), c.devices...)
}

// Device finds one by id, which is how a rig entry names it.
func (c *Client) Device(id string) (*Remote, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.devices {
		if d.manifest.ID == id {
			return d, true
		}
	}
	return nil, false
}

// Names lists what this node has, for an error that has to say what was
// available instead of what was asked for.
func (c *Client) Names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.devices))
	for _, d := range c.devices {
		out = append(out, d.manifest.ID)
	}
	return out
}

func (c *Client) Close() error { return c.conn.Close() }

// Authenticated reports whether traffic carries an authentication tag.
func (c *Client) Authenticated() bool { return c.auth.Enabled() }

// next returns the next replay counter. Only meaningful when authenticated.
func (c *Client) next() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth == nil {
		return 0
	}
	c.counter++
	return c.counter
}

func (c *Client) send(body []byte) error {
	_, err := c.conn.Write(c.auth.Wrap(body))
	return err
}

// readLoop delivers acknowledgements to whoever is waiting for them.
func (c *Client) readLoop() {
	buf := make([]byte, 2048)
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			return
		}
		body, err := c.auth.Unwrap(buf[:n])
		if err != nil {
			continue
		}
		m, err := Decode(body)
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

// SendBundle sends one curve frame carrying every output due this tick.
//
// Curve frames are authenticated but carry no replay counter. A replayed frame
// is superseded by the next genuine one 20ms later, and giving every frame a
// counter would mean the node dropping frames whenever one arrived out of
// order, which UDP does routinely.
func (c *Client) SendBundle(outs []Outputs) error {
	b, err := MarshalBundle(outs)
	if err != nil {
		return err
	}
	return c.send(b)
}

// Heartbeat tells the node the conductor is alive. A node that stops hearing
// this drives itself safe without being asked, which is the whole point.
func (c *Client) Heartbeat() error {
	b, _ := Encode(&Message{Type: TypeHeartbeat, N: c.next()})
	return c.send(b)
}

// Safe orders an immediate return to the safe state.
func (c *Client) Safe() error {
	b, _ := Encode(&Message{Type: TypeSafe, N: c.next()})
	return c.send(b)
}
