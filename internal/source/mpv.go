package source

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// MPV reads position from mpv over its JSON IPC socket, started with
// --input-ipc-server=PATH.
//
// mpv is the reference source. Measured on Debian 13: the round trip is 41 to
// 86 us, position updates once per frame, and playback pacing is accurate to
// within a few hundred parts per million. See
// LOGBOOK/features/feat-timing-core.md.
type MPV struct {
	conn    net.Conn
	r       *bufio.Reader
	reqID   int
	timeout time.Duration
}

// DialMPV connects to an mpv IPC socket.
func DialMPV(socket string) (*MPV, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("mpv: dial %s: %w (is mpv running with --input-ipc-server=%s?)", socket, err, socket)
	}
	return &MPV{conn: conn, r: bufio.NewReader(conn), timeout: 2 * time.Second}, nil
}

// NewMPVConn wraps an existing connection, for tests against a stub server.
func NewMPVConn(conn net.Conn) *MPV {
	return &MPV{conn: conn, r: bufio.NewReader(conn), timeout: 2 * time.Second}
}

func (m *MPV) Name() string { return "mpv" }

func (m *MPV) Close() error { return m.conn.Close() }

type mpvReply struct {
	Data      json.RawMessage `json:"data"`
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
	Event     string          `json:"event"`
}

// getProperty asks mpv for one property. found is false when mpv knows the
// property but has no value for it right now, which is an ordinary state
// rather than a failure.
func (m *MPV) getProperty(name string) (raw json.RawMessage, found bool, err error) {
	m.reqID++
	id := m.reqID
	if err := m.conn.SetDeadline(time.Now().Add(m.timeout)); err != nil {
		return nil, false, err
	}
	req := fmt.Sprintf("{\"command\":[\"get_property\",%q],\"request_id\":%d}\n", name, id)
	if _, err := m.conn.Write([]byte(req)); err != nil {
		return nil, false, fmt.Errorf("mpv: write: %w", err)
	}

	// mpv interleaves asynchronous event messages on the same socket, so read
	// until the reply carrying our request id.
	for {
		line, err := m.r.ReadBytes('\n')
		if err != nil {
			return nil, false, fmt.Errorf("mpv: read: %w", err)
		}
		var rep mpvReply
		if json.Unmarshal(line, &rep) != nil {
			continue // not JSON we understand; ignore rather than fail
		}
		if rep.Event != "" || rep.RequestID != id {
			continue
		}
		if rep.Error != "success" {
			// "property unavailable" is what mpv says while idle or between
			// files. It is a state, not a problem.
			if rep.Error == "property unavailable" {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("mpv: %s: %s", name, rep.Error)
		}
		return rep.Data, true, nil
	}
}

func (m *MPV) getFloat(name string) (float64, bool, error) {
	raw, found, err := m.getProperty(name)
	if err != nil || !found {
		return 0, false, err
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false, nil
	}
	return f, true, nil
}

// Position returns mpv's time-pos, the presentation timestamp of the frame
// currently on screen.
func (m *MPV) Position() (time.Duration, bool, error) {
	secs, ok, err := m.getFloat("time-pos")
	if err != nil || !ok {
		return 0, false, err
	}
	return time.Duration(secs * float64(time.Second)), true, nil
}

// FrameInterval derives the frame period from the container frame rate,
// falling back to mpv's own estimate for content that does not declare one.
func (m *MPV) FrameInterval() (time.Duration, bool) {
	for _, prop := range []string{"container-fps", "estimated-vf-fps"} {
		fps, ok, err := m.getFloat(prop)
		if err == nil && ok && fps > 0 {
			return time.Duration(float64(time.Second) / fps), true
		}
	}
	return 0, false
}

// Duration returns the length of the file mpv currently has open.
func (m *MPV) Duration() (time.Duration, bool) {
	secs, ok, err := m.getFloat("duration")
	if err != nil || !ok || secs <= 0 {
		return 0, false
	}
	return time.Duration(secs * float64(time.Second)), true
}
