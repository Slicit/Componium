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
	// Manifest is one device, for the common case and for everything written
	// before a node could have several.
	Manifest Manifest
	// Devices is what a node with more than one carries. When empty, Manifest
	// is the only device.
	Devices []Manifest
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

	mu      sync.Mutex
	devices []*nodeDevice
	byID    map[string]*nodeDevice

	peer      *net.UDPAddr
	lastBeat  time.Time
	cues      int
	curves    int
	tripped   int
	rejected  int
	startedAt time.Time
	auth      *Auth
	replay    replayGuard
}

// nodeDevice is one output, with the state that belongs to it alone.
//
// Per device rather than per node, and that is the safety-critical part of
// carrying several. A hold expiring on the fogger must take the fogger safe and
// not the fan halfway through a scene; the watchdog, when it fires, must take
// every one of them and not just whichever was addressed last.
type nodeDevice struct {
	manifest Manifest
	index    int
	state    map[string]float64
	// holdUntil is when a span this device was given must end, whether or not
	// a stop ever arrives.
	holdUntil time.Time
	safe      bool
}

func (d *nodeDevice) applySafe() {
	d.safe = true
	d.holdUntil = time.Time{}
	for k := range d.state {
		d.state[k] = 0
	}
	for k, v := range d.manifest.SafeState {
		d.state[k] = v
	}
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
	n := &Node{cfg: cfg, conn: conn, byID: map[string]*nodeDevice{},
		startedAt: time.Now(), auth: NewAuth(cfg.Secret)}
	n.adopt(devicesOf(cfg))
	n.applySafe()
	return n, nil
}

// devicesOf is what a config actually declares, one shape or the other.
func devicesOf(cfg NodeConfig) []Manifest {
	if len(cfg.Devices) > 0 {
		return cfg.Devices
	}
	if cfg.Manifest.ID != "" {
		return []Manifest{cfg.Manifest}
	}
	// A node with nothing configured. An ordinary state: it is what every
	// freshly flashed board is in, and it can still be talked to and told what
	// is attached to it.
	return nil
}

// adopt replaces the node's devices. The caller must not hold the lock.
func (n *Node) adopt(manifests []Manifest) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.devices = nil
	n.byID = map[string]*nodeDevice{}
	for i, m := range manifests {
		d := &nodeDevice{manifest: m, index: i, state: map[string]float64{}}
		d.applySafe()
		n.devices = append(n.devices, d)
		n.byID[m.ID] = d
	}
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
			overdue := !n.lastBeat.IsZero() &&
				now.Sub(n.lastBeat) > n.cfg.Timeout && !n.allSafe()
			// A span that has run its declared duration ends here, whether or
			// not the conductor's stop ever arrived. This is the layer that
			// survives a lost datagram, and it takes the one device whose hold
			// expired: a four second fog burst ending must not stop a fan in
			// the middle of a scene.
			for _, d := range n.devices {
				if !d.holdUntil.IsZero() && now.After(d.holdUntil) && !d.safe {
					d.applySafe()
				}
			}
			if overdue {
				n.tripped++
				// Every device, not the one most recently addressed. The
				// conductor is gone; nothing here knows which output is the
				// dangerous one, so all of them go.
				for _, d := range n.devices {
					d.applySafe()
				}
			}
			n.mu.Unlock()
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
			// An index this node does not have is skipped and the rest of the
			// frame applied. A frame is fifty times a second and superseded
			// 20ms later, so refusing all of it because one output has gone is
			// the wrong trade: the outputs that are still there should keep
			// moving.
			if o.Index < 0 || o.Index >= len(n.devices) {
				continue
			}
			d := n.devices[o.Index]
			d.safe = false
			for i, ch := range d.manifest.Channels {
				if i < len(o.Values) {
					d.state[ch.Name] = float64(o.Values[i])
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
		n.send(n.helloMessage(), from)
	case TypeHeartbeat:
		n.mu.Lock()
		n.lastBeat = time.Now()
		n.mu.Unlock()
	case TypeCue:
		d := n.route(m.Instrument)
		if d == nil {
			// Not acknowledged, deliberately. Acknowledging a cue that was not
			// applied is a lie, and the conductor's retry and then its recorded
			// skip is exactly the machinery for a cue that did not land. A
			// silent success would be the only outcome nobody can see.
			return
		}
		if instrumentStop(m.Action) {
			n.mu.Lock()
			d.applySafe()
			n.mu.Unlock()
			n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
			return
		}
		n.mu.Lock()
		n.cues++
		d.safe = false
		for k, v := range m.Params {
			d.state[k] = v
		}
		if m.HoldMS > 0 {
			d.holdUntil = time.Now().Add(m.HoldMS.Duration())
		} else {
			d.holdUntil = time.Time{}
		}
		n.mu.Unlock()
		n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
	case TypeSafe:
		// Every device. A conductor asking for safe is not asking about one
		// output.
		n.applySafe()
		n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
	case TypeConfigure:
		// Only when authenticated, and that is the rule rather than a caution.
		// A stranger who can write this can move a relay onto a pin nobody
		// intended, or declare a latency of zero and corrupt the timing of
		// every cue after it in a way that reads as the score being wrong. A
		// node with no secret does not have the capability, which is also what
		// keeps a demonstration with no hardware working with no key
		// management.
		if !n.auth.Enabled() {
			n.send(&Message{Type: TypeAck, Seq: m.Seq,
				Error: "this node takes no configuration without a secret"}, from)
			return
		}
		if err := n.configure(m.Devices); err != nil {
			n.send(&Message{Type: TypeAck, Seq: m.Seq, Error: err.Error()}, from)
			return
		}
		n.send(&Message{Type: TypeAck, Seq: m.Seq}, from)
		// The instruments and their indices have just changed, so anything
		// holding the old ones is now wrong and has to be told.
		n.send(n.helloMessage(), from)
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
	for _, d := range n.devices {
		d.applySafe()
	}
}

// allSafe reports whether every device is at its safe value. The caller holds
// the lock.
func (n *Node) allSafe() bool {
	for _, d := range n.devices {
		if !d.safe {
			return false
		}
	}
	return true
}

// route finds the device a cue names.
//
// An empty name is the single device, which is what a conductor built before
// ADR 0007 sends and what a board with one thing on it should keep accepting. A
// name on a node with several is required: guessing which output somebody meant
// is the one thing worse than not applying the cue.
func (n *Node) route(id string) *nodeDevice {
	n.mu.Lock()
	defer n.mu.Unlock()
	if id == "" {
		if len(n.devices) == 1 {
			return n.devices[0]
		}
		return nil
	}
	return n.byID[id]
}

// helloMessage is what this node says it has.
func (n *Node) helloMessage() *Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	name := n.cfg.Manifest.ID
	if len(n.devices) > 0 && name == "" {
		name = n.devices[0].manifest.ID
	}
	out := &Message{
		Type: TypeHello,
		Node: NodeInfo{Name: name, Firmware: Version, Chip: "software"},
		// Never nil, so that a node with nothing configured announces an empty
		// list rather than looking like a 0.2 node with no manifest.
		Instruments: []Instrument{},
	}
	for _, d := range n.devices {
		out.Instruments = append(out.Instruments, d.manifest.toAnnouncement(d.index))
	}
	return out
}

// configure replaces what the node believes is attached to it.
//
// Refused whole or accepted whole. Half a configuration is worse than none: it
// is a node that looks configured and is not, and nothing downstream can tell.
func (n *Node) configure(devices []Device) error {
	seen := map[string]bool{}
	pins := map[int]string{}
	manifests := make([]Manifest, 0, len(devices))
	for i, dev := range devices {
		if dev.ID == "" {
			return fmt.Errorf("device %d has no id", i+1)
		}
		if seen[dev.ID] {
			return fmt.Errorf("two devices called %q", dev.ID)
		}
		seen[dev.ID] = true
		switch dev.Type {
		case DevicePWM, DeviceWS28xx, DeviceRelay:
		default:
			return fmt.Errorf("%s: unknown device type %q", dev.ID, dev.Type)
		}
		if other, taken := pins[dev.GPIO]; taken {
			return fmt.Errorf("%s and %s both claim gpio %d", other, dev.ID, dev.GPIO)
		}
		pins[dev.GPIO] = dev.ID
		manifests = append(manifests, dev.toManifest())
	}
	n.adopt(manifests)
	return nil
}

// State returns what the node's first device is set to.
//
// Kept for the one device case, which is most of them and all of the tests
// written before a node could have several. Reaching for it on a node with
// three is asking about the first one, which is rarely the question; StateOf
// names which.
func (n *Node) State() map[string]float64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.devices) == 0 {
		return map[string]float64{}
	}
	return copyState(n.devices[0].state)
}

// StateOf returns what one device is set to, by the id it announced.
func (n *Node) StateOf(id string) map[string]float64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	d, ok := n.byID[id]
	if !ok {
		return nil
	}
	return copyState(d.state)
}

// Announced lists the devices this node currently has, in index order.
func (n *Node) Announced() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.devices))
	for _, d := range n.devices {
		out = append(out, d.manifest.ID)
	}
	return out
}

func copyState(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Stats reports what the node has seen, for tests and for diagnosis.
func (n *Node) Stats() (cues, curves, safeTrips int, isSafe bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	// Every device, because a node is safe when all of it is.
	return n.cues, n.curves, n.tripped, n.allSafe()
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

// holdExpired reports whether any device's span has run its course.
func (n *Node) holdExpired(now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, d := range n.devices {
		if !d.holdUntil.IsZero() && now.After(d.holdUntil) && !d.safe {
			return true
		}
	}
	return false
}
