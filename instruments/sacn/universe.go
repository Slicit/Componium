package sacn

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"
)

// One universe, shared by the fixtures in it.
//
// A DMX universe is 512 channels and is meant to carry many fixtures: a wash at
// address 1, an event light at 4, a hazer at 7. E1.31 sends the whole universe
// in every packet, because that is what a lighting desk does and what every
// receiver expects.
//
// Which means a fixture cannot own one. Giving each fixture its own buffer and
// its own socket, as this package first did, makes two fixtures on one universe
// erase each other: both transmit all 512 slots, and each one's frame has the
// other's channels at zero. The louder of them wins, and "louder" means
// whichever is sent more often. A wash on a curve track sending fifty times a
// second against an event light sending on a cue is not a fight, it is a wash
// with an event light that does nothing at all.
//
// Reported as: ambient works, event does not.
//
// So the universe owns the buffer, the sequence number, the CID and the socket,
// and a Light is a view onto a few channels of it.
type Universe struct {
	conn   net.Conn
	number uint16
	source string

	mu   sync.Mutex
	data [Slots]byte
	seq  uint8
	cid  [16]byte
}

// Dial opens a universe at an address, or at its multicast group when the
// address is empty.
func Dial(number uint16, addr, source string) (*Universe, error) {
	if addr == "" {
		addr = MulticastAddr(number)
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("sacn: dial %s: %w", addr, err)
	}
	if source == "" {
		source = "componium"
	}
	u := &Universe{conn: conn, number: number, source: source}
	if _, err := rand.Read(u.cid[:]); err != nil {
		conn.Close()
		return nil, err
	}
	return u, nil
}

// Key identifies a universe by where it is sent, so that fixtures asking for
// the same one get the same one.
func Key(number uint16, addr string) string {
	if addr == "" {
		addr = MulticastAddr(number)
	}
	return fmt.Sprintf("%s/%d", addr, number)
}

// Set writes some channels and transmits the universe.
//
// from is zero based; the caller has already turned a fixture's 1 based start
// address into an offset, because that conversion belongs with the fixture that
// knows its own mode.
func (u *Universe) Set(from int, values []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i, v := range values {
		if from+i < 0 || from+i >= Slots {
			continue
		}
		u.data[from+i] = v
	}
	return u.send()
}

// send transmits the current state. The caller must hold the lock.
func (u *Universe) send() error {
	p := &Packet{
		CID:        u.cid,
		SourceName: u.source,
		Universe:   u.number,
		Priority:   100,
		Sequence:   u.seq,
		Data:       u.data,
	}
	u.seq++
	_, err := u.conn.Write(p.Marshal())
	return err
}

// Keepalive retransmits the current state until ctx is done.
//
// E1.31 receivers commonly drop back to their idle state after about 2.5
// seconds without traffic, so a fixture set once and left alone goes dark on
// its own. Componium cues are sparse by nature: a flash is one dispatch and
// then nothing, and without this it is a flash that lasts until the receiver's
// patience runs out rather than as long as the score says.
//
// Nothing in Componium starts a goroutine the caller did not ask for, so this
// is the caller's to run.
func (u *Universe) Keepalive(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			u.mu.Lock()
			err := u.send()
			u.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
}

func (u *Universe) Close() error { return u.conn.Close() }
