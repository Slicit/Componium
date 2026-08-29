// Command player-events scripts pause and seek against mpv and records what
// the reported position stream does across those transitions.
//
// An anchoring clock assumes position advances predictably between anchors.
// Pause and seek both violate that: pause freezes media time while wall time
// keeps running, and seek moves media time discontinuously. The clock has to
// notice both, fast, or it will keep firing cues from a stale anchor.
//
// This is a spike. It is not part of the build and it is not tested.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

type client struct {
	conn  net.Conn
	r     *bufio.Reader
	reqID int
}

type reply struct {
	Data      json.RawMessage `json:"data"`
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
	Event     string          `json:"event"`
}

func (c *client) call(cmd string) (json.RawMessage, error) {
	c.reqID++
	line := fmt.Sprintf("{\"command\":%s,\"request_id\":%d}\n", cmd, c.reqID)
	if _, err := c.conn.Write([]byte(line)); err != nil {
		return nil, err
	}
	for {
		b, err := c.r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var rep reply
		if json.Unmarshal(b, &rep) != nil {
			continue
		}
		if rep.Event != "" || rep.RequestID != c.reqID {
			continue
		}
		if rep.Error != "success" {
			return nil, fmt.Errorf("%s", rep.Error)
		}
		return rep.Data, nil
	}
}

func (c *client) timePos() (float64, bool) {
	raw, err := c.call(`["get_property","time-pos"]`)
	if err != nil {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		return 0, false
	}
	return f, true
}

type event struct {
	at   time.Duration
	name string
	cmd  string
}

type sample struct {
	at  time.Duration
	pos float64
	ok  bool
}

func main() {
	socket := flag.String("socket", "/tmp/mpv.sock", "mpv IPC socket")
	rate := flag.Float64("rate", 100, "samples per second")
	flag.Parse()

	conn, err := net.Dial("unix", *socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	c := &client{conn: conn, r: bufio.NewReader(conn)}

	script := []event{
		{3 * time.Second, "PAUSE", `["set_property","pause",true]`},
		{6 * time.Second, "RESUME", `["set_property","pause",false]`},
		{9 * time.Second, "SEEK +30", `["seek",30,"relative"]`},
		{12 * time.Second, "SEEK abs 5", `["seek",5,"absolute"]`},
	}

	interval := time.Duration(float64(time.Second) / *rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start := time.Now()
	deadline := start.Add(15 * time.Second)

	var samples []sample
	fired := make([]time.Duration, len(script))
	next := 0

	fmt.Printf("sampling at %.0f Hz, scripted events at 3s, 6s, 9s, 12s\n\n", *rate)

	for now := range ticker.C {
		if now.After(deadline) {
			break
		}
		off := now.Sub(start)
		if next < len(script) && off >= script[next].at {
			t0 := time.Now()
			if _, err := c.call(script[next].cmd); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", script[next].name, err)
			}
			fired[next] = t0.Sub(start)
			next++
		}
		pos, ok := c.timePos()
		samples = append(samples, sample{at: off, pos: pos, ok: ok})
	}

	for i, ev := range script {
		fmt.Printf("=== %s issued at %.3f s ===\n", ev.name, fired[i].Seconds())
		show(samples, fired[i], 250*time.Millisecond, 400*time.Millisecond)
		fmt.Println()
	}
	summarisePause(samples, fired[0], fired[1])
}

// show prints the samples in a window around t, marking where the reported
// position stops tracking a simple forward prediction from the sample before
// the event.
func show(s []sample, t, before, after time.Duration) {
	var prev *sample
	shown := 0
	for i := range s {
		if s[i].at < t-before || s[i].at > t+after {
			continue
		}
		mark := ""
		if prev != nil {
			dWall := (s[i].at - prev.at).Seconds()
			dMedia := s[i].pos - prev.pos
			// Anything more than 25ms away from realtime advance is a
			// discontinuity worth flagging.
			if !s[i].ok {
				mark = "  <- unavailable"
			} else if prev.ok && dMedia-dWall > 0.025 {
				mark = fmt.Sprintf("  <- JUMP FORWARD %.3f s", dMedia)
			} else if prev.ok && dWall-dMedia > 0.025 {
				mark = fmt.Sprintf("  <- STALLED (media advanced %.3f s in %.3f s)", dMedia, dWall)
			}
		}
		if s[i].ok {
			fmt.Printf("  t=%7.3f  pos=%8.3f%s\n", s[i].at.Seconds(), s[i].pos, mark)
		} else {
			fmt.Printf("  t=%7.3f  pos=  (none)%s\n", s[i].at.Seconds(), mark)
		}
		prev = &s[i]
		shown++
		if shown > 24 {
			fmt.Println("  ...")
			break
		}
	}
}

// summarisePause reports how far media time fell behind wall time across the
// paused window, which is the error a clock would accumulate if it failed to
// notice the pause at all.
func summarisePause(s []sample, from, to time.Duration) {
	var a, b *sample
	for i := range s {
		if s[i].ok && s[i].at >= from && a == nil {
			a = &s[i]
		}
		if s[i].ok && s[i].at <= to {
			b = &s[i]
		}
	}
	if a == nil || b == nil {
		return
	}
	wall := (b.at - a.at).Seconds()
	media := b.pos - a.pos
	fmt.Printf("=== pause window ===\n")
	fmt.Printf("  wall elapsed   %.3f s\n", wall)
	fmt.Printf("  media advanced %.3f s\n", media)
	fmt.Printf("  a clock that missed the pause would be %.3f s wrong by the end\n", wall-media)
}
