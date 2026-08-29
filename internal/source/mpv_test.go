package source

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// stub answers get_property requests the way mpv does. Any preamble lines are
// sent in response to the first request, before its reply, which is both how
// mpv interleaves events and the only ordering net.Pipe permits: the pipe is
// unbuffered, so a server that writes before reading deadlocks against a
// client that writes before reading.
func stub(t *testing.T, conn net.Conn, reply func(prop string, id int) string, preamble ...string) {
	t.Helper()
	go func() {
		defer conn.Close()
		r := bufio.NewReader(conn)
		sent := false
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				Command   []json.RawMessage `json:"command"`
				RequestID int               `json:"request_id"`
			}
			if json.Unmarshal(line, &req) != nil || len(req.Command) < 2 {
				continue
			}
			var prop string
			json.Unmarshal(req.Command[1], &prop)
			if !sent {
				sent = true
				for _, p := range preamble {
					if _, err := conn.Write([]byte(p + "\n")); err != nil {
						return
					}
				}
			}
			if _, err := conn.Write([]byte(reply(prop, req.RequestID) + "\n")); err != nil {
				return
			}
		}
	}()
}

func ok(prop string, id int) string {
	switch prop {
	case "time-pos":
		return jsonf(`{"data":12.5,"request_id":%d,"error":"success"}`, id)
	case "container-fps":
		return jsonf(`{"data":24.0,"request_id":%d,"error":"success"}`, id)
	case "duration":
		return jsonf(`{"data":7200.0,"request_id":%d,"error":"success"}`, id)
	}
	return jsonf(`{"request_id":%d,"error":"property unavailable"}`, id)
}

func jsonf(format string, id int) string {
	return strings.Replace(format, "%d", itoa(id), 1)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestMPVReadsPosition(t *testing.T) {
	c1, c2 := net.Pipe()
	stub(t, c2, ok)
	m := NewMPVConn(c1)
	defer m.Close()

	pos, found, err := m.Position()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("position not found")
	}
	if want := 12500 * time.Millisecond; pos != want {
		t.Errorf("pos %v, want %v", pos, want)
	}
}

func TestMPVSkipsInterleavedEvents(t *testing.T) {
	// mpv pushes events on the same socket. A reply must still be found.
	c1, c2 := net.Pipe()
	stub(t, c2, ok,
		`{"event":"playback-restart"}`,
		`{"event":"file-loaded"}`,
	)
	m := NewMPVConn(c1)
	defer m.Close()

	pos, found, err := m.Position()
	if err != nil || !found {
		t.Fatalf("pos=%v found=%v err=%v", pos, found, err)
	}
	if want := 12500 * time.Millisecond; pos != want {
		t.Errorf("pos %v, want %v", pos, want)
	}
}

func TestMPVUnavailablePositionIsNotAnError(t *testing.T) {
	// While idle or between files mpv reports the property as unavailable.
	// That is a state the clock handles, not a failure the show should die on.
	c1, c2 := net.Pipe()
	stub(t, c2, func(prop string, id int) string {
		return jsonf(`{"request_id":%d,"error":"property unavailable"}`, id)
	})
	m := NewMPVConn(c1)
	defer m.Close()

	_, found, err := m.Position()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("reported a position when the property was unavailable")
	}
}

func TestMPVRealErrorIsReported(t *testing.T) {
	c1, c2 := net.Pipe()
	stub(t, c2, func(prop string, id int) string {
		return jsonf(`{"request_id":%d,"error":"invalid parameter"}`, id)
	})
	m := NewMPVConn(c1)
	defer m.Close()

	if _, _, err := m.Position(); err == nil {
		t.Error("no error for a genuine mpv failure")
	}
}

func TestMPVFrameIntervalAndDuration(t *testing.T) {
	c1, c2 := net.Pipe()
	stub(t, c2, ok)
	m := NewMPVConn(c1)
	defer m.Close()

	fi, found := m.FrameInterval()
	if !found {
		t.Fatal("no frame interval")
	}
	// 24fps is 41.666ms. Allow a microsecond of rounding.
	if want := time.Second / 24; fi < want-time.Microsecond || fi > want+time.Microsecond {
		t.Errorf("frame interval %v, want %v", fi, want)
	}
	d, found := m.Duration()
	if !found || d != 2*time.Hour {
		t.Errorf("duration %v found=%v, want 2h", d, found)
	}
}
