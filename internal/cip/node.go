package cip

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// NodeConfig describes a simulated instrument node.
type NodeConfig struct {
	Manifest Manifest
	// Timeout is how long without a heartbeat before the node drives itself
	// safe. Zero means 300ms, matching the safety supervisor.
	Timeout time.Duration
	// Secret enables authentication. When set, unauthenticated datagrams are
	// ignored entirely: a node that requires a secret should be invisible to
	// anyone who does not have it.
	Secret string
	// Addr is the UDP address to listen on. Zero port picks a free one.
	Addr string
}

// Node is a software instrument node: everything the ESP32 firmware does,
// minus the pins.
//
// It exists for two reasons. It makes the protocol testable end to end without
// hardware, and it lets someone with no ESP32 at all run a complete rig, which
// is the same reason virtual instruments exist.
type Node struct {
	cfg  NodeConfig
	conn *net.UDPConn

	mu        sync.Mutex
	state     map[string]float64
	peer      *net.UDPAddr
	lastBeat  time.Time
	safe      bool
	cues      int
	curves    int
	tripped   int
	rejected  int
	startedAt time.Time
	auth      *Auth
	// holdUntil is when a span the node was given must end, whether or not a
	// stop ever arrives.
	holdUntil time.Time
	replay    replayGuard
}

// NewNode binds a socket and prepares the node.
func NewNode(cfg NodeConfig) (*Node, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 300 * time.Millisecond
	}
	addr := cfg.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return nil, err
	}
	n := &Node{cfg: cfg, conn: conn, state: map[string]float64{},
		startedAt: time.Now(), auth: NewAuth(cfg.Secret)}
	n.applySafe()
	return n, nil
}

// Addr is where the node is listening.
func (n *Node) Addr() string { return n.conn.LocalAddr().String() }

func (n *Node) Close() error { return n.conn.Close() }

// Run serves until ctx is done. It also runs the node's own watchdog, which is
// deliberately independent of the conductor: the node must be safe when the
// conductor is absent, and a node that only checks when told to would never
// notice being abandoned.
func (n *Node) Run(ctx context.Context) error {
	go n.watchdog(ctx)

	buf := make([]byte, 2048)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		read, from, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		n.handle(buf[:read], from)
	}
}

func (n *Node) watchdog(ctx context.Context) {
	t := time.NewTicker(n.cfg.Timeout / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			n.mu.Lock()
			overdue := !n.lastBeat.IsZero() && now.Sub(n.lastBeat) > n.cfg.Timeout && !n.safe
			n.mu.Unlock()
			// A span that has run its declared duration ends here, whether or
			// not the conductor's stop ever arrived. This is the layer that
			// survives a lost datagram.
			if n.holdExpired(now) {
				n.applySafe()
			}
			if overdue {
				n.mu.Lock()
				n.tripped++
				n.mu.Unlock()
				n.applySafe()
			}
		}
	}
}

func (n *Node) handle(b []byte, from *net.UDPAddr) {
	// Verify before looking at anything. An unauthenticated datagram to a
	// node that requires a secret is dropped in silence: replying at all
	// would confirm the node exists and is worth attacking.
	body, err := n.auth.Unwrap(b)
	if err != nil {
		n.mu.Lock()
		n.rejected++
		n.mu.Unlock()
		return
	}
	b = body
	// A curve frame is binary and has no envelope, so it is recognised first.
	if outs, err := UnmarshalBundle(b); err == nil {
		n.mu.Lock()
		n.curves++
		for _, o := range outs {
			// One device, so index 0 and nothing else. An index this node does
			// not have is skipped and the rest of the frame is applied: a frame
			// is fifty times a second and superseded 20ms later, so refusing
			// all of it because one output has gone would be the wrong trade.
			if o.Index != 0 {
				continue
			}
			n.safe = false
			for i, ch := range n.cfg.Manifest.Channels {
				if i < len(o.Values) {
					n.state[ch.Name] = float64(o.Values[i])
				}
			}
		}
		n.mu.Unlock()
		return
	}

	m, err := Decode(b)
	if err != nil {
		return
	}
	if !n.acceptCounter(m.N) {
		n.mu.Lock()
		n.rejected++
		n.mu.Unlock()
		return
	}
	n.mu.Lock()
	n.peer = from
	n.mu.Unlock()

	switch m.Type {
	case TypeHello:
		reply := &Message{
			Type: TypeHello,
			Node: NodeInfo{Name: n.cfg.Manifest.ID, Firmware: Version, Chip: "software"},
			Instruments: []Instrument{
				n.cfg.Manifest.toAnnouncement(0),
			},
		}
		n.send(reply, from)
	case TypeHeartbeat:
		n.mu.Lock()
		n.lastBeat = time.Now()
		n.mu.Unlock()
	case TypeCue:
		if instrumentStop(m.Action) {
			n.applySafe()
			n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
			return
		}
		n.mu.Lock()
		n.cues++
		n.safe = false
		for k, v := range m.Params {
			n.state[k] = v
		}
		if m.HoldMS > 0 {
			n.holdUntil = time.Now().Add(m.HoldMS.Duration())
		} else {
			n.holdUntil = time.Time{}

		}
		n.mu.Unlock()
		n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
	case TypeSafe:
		n.applySafe()
		n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
	}
}

func (n *Node) send(m *Message, to *net.UDPAddr) {
	b, err := Encode(m)
	if err != nil {
		return
	}
	n.conn.WriteToUDP(n.auth.Wrap(b), to)
}

func (n *Node) applySafe() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.safe = true
	for k := range n.state {
		n.state[k] = 0
	}
	for k, v := range n.cfg.Manifest.SafeState {
		n.state[k] = v
	}
}

// State returns what the node's outputs are currently set to.
func (n *Node) State() map[string]float64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]float64, len(n.state))
	for k, v := range n.state {
		out[k] = v
	}
	return out
}

// Stats reports what the node has seen, for tests and for diagnosis.
func (n *Node) Stats() (cues, curves, safeTrips int, isSafe bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cues, n.curves, n.tripped, n.safe
}

func (n *Node) String() string {
	c, cv, t, s := n.Stats()
	return fmt.Sprintf("node %s: %d cues, %d curve frames, %d watchdog trips, safe=%v",
		n.cfg.Manifest.ID, c, cv, t, s)
}

// acceptCounter rejects replayed control messages.
//
// Inert when authentication is off, because a counter is meaningless against
// an attacker who can simply forge the message outright.
func (n *Node) acceptCounter(counter uint64) bool {
	if n.auth == nil {
		return true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.replay.accept(counter)
}

// Rejected reports how many datagrams failed authentication or were replays.
// A number climbing steadily means somebody is trying, or a rig has the wrong
// secret configured.
func (n *Node) Rejected() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.rejected
}

// instrumentStop mirrors instrument.IsStop without importing that package,
// which would make the wire protocol depend on the conductor's types.
func instrumentStop(action string) bool {
	switch action {
	case "stop", "off", "safe", "neutral":
		return true
	}
	return false
}

// holdExpired reports whether a span the node was given has run its course.
func (n *Node) holdExpired(now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return !n.holdUntil.IsZero() && now.After(n.holdUntil) && !n.safe
}
