// Command clock-jitter measures how well mpv's reported playback position can
// be turned into a usable media clock.
//
// The question it answers: if we poll mpv for time-pos at some rate, how
// accurately can we predict media time *between* polls? That number decides
// how much filtering the conductor needs, and whether frame-accurate cueing
// from a polled source is possible at all.
//
// Usage:
//
//	mpv --input-ipc-server=/tmp/mpv.sock --idle=yes video.mkv
//	go run ./spikes/clock-jitter -socket /tmp/mpv.sock -rate 10 -duration 60
//
// This is a spike. It is not part of the build and it is not tested.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"sort"
	"time"
)

type reply struct {
	Data      *float64 `json:"data"`
	RequestID int      `json:"request_id"`
	Error     string   `json:"error"`
	Event     string   `json:"event"`
}

type sample struct {
	idx      int
	sendOff  time.Duration // from start of run
	rtt      time.Duration
	mediaPos float64
}

func main() {
	socket := flag.String("socket", "/tmp/mpv.sock", "mpv IPC socket path")
	rate := flag.Float64("rate", 10, "polls per second")
	duration := flag.Duration("duration", 60*time.Second, "how long to sample")
	out := flag.String("out", "", "optional CSV output path")
	flag.Parse()

	conn, err := net.Dial("unix", *socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial %s: %v\nIs mpv running with --input-ipc-server=%s ?\n", *socket, err, *socket)
		os.Exit(1)
	}
	defer conn.Close()
	r := bufio.NewReader(conn)

	interval := time.Duration(float64(time.Second) / *rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	deadline := start.Add(*duration)
	var samples []sample
	reqID := 0
	skipped := 0

	fmt.Fprintf(os.Stderr, "sampling %s at %.1f Hz (every %s)...\n", *duration, *rate, interval)

	for now := range ticker.C {
		if now.After(deadline) {
			break
		}
		reqID++
		sendAt := time.Now()
		if _, err := fmt.Fprintf(conn, `{"command":["get_property","time-pos"],"request_id":%d}`+"\n", reqID); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			break
		}

		pos, err := awaitReply(r, reqID)
		recvAt := time.Now()
		if err != nil {
			// Property is unavailable while paused, seeking, or idle. Those
			// gaps are themselves interesting, so count rather than fail.
			skipped++
			continue
		}
		samples = append(samples, sample{
			idx:      reqID,
			sendOff:  sendAt.Sub(start),
			rtt:      recvAt.Sub(sendAt),
			mediaPos: pos,
		})
	}

	if len(samples) < 10 {
		fmt.Fprintf(os.Stderr, "only %d usable samples (%d skipped). Is a file actually playing?\n", len(samples), skipped)
		os.Exit(1)
	}
	report(samples, skipped)
	if *out != "" {
		if err := writeCSV(*out, samples); err != nil {
			fmt.Fprintf(os.Stderr, "csv: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nwrote %s\n", *out)
	}
}

// awaitReply reads until it sees the response matching id, discarding the
// asynchronous event messages mpv interleaves on the same socket.
func awaitReply(r *bufio.Reader, id int) (float64, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return 0, err
		}
		var rep reply
		if err := json.Unmarshal(line, &rep); err != nil {
			continue
		}
		if rep.Event != "" || rep.RequestID != id {
			continue
		}
		if rep.Error != "success" || rep.Data == nil {
			return 0, fmt.Errorf("mpv: %s", rep.Error)
		}
		return *rep.Data, nil
	}
}

// report fits media position against wall time and reports the residuals.
// The residual is the number that matters: it is how far off we would be if
// we predicted media time by linear extrapolation between polls.
func report(s []sample, skipped int) {
	n := float64(len(s))
	var sx, sy, sxx, sxy float64
	for _, v := range s {
		x := v.sendOff.Seconds()
		y := v.mediaPos
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	denom := n*sxx - sx*sx
	slope := (n*sxy - sx*sy) / denom
	intercept := (sy*sxx - sx*sxy) / denom

	var resid []float64
	var sumSq, maxAbs float64
	for _, v := range s {
		e := v.mediaPos - (slope*v.sendOff.Seconds() + intercept)
		resid = append(resid, e)
		sumSq += e * e
		if math.Abs(e) > maxAbs {
			maxAbs = math.Abs(e)
		}
	}
	stddev := math.Sqrt(sumSq / n)

	rtts := make([]time.Duration, len(s))
	for i, v := range s {
		rtts[i] = v.rtt
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })

	fmt.Printf("samples          %d (%d skipped)\n", len(s), skipped)
	fmt.Printf("span             %.1f s of wall time\n", s[len(s)-1].sendOff.Seconds()-s[0].sendOff.Seconds())
	fmt.Printf("\nIPC round trip\n")
	fmt.Printf("  p50            %s\n", rtts[len(rtts)/2])
	fmt.Printf("  p95            %s\n", rtts[len(rtts)*95/100])
	fmt.Printf("  max            %s\n", rtts[len(rtts)-1])
	fmt.Printf("\nMedia clock vs wall clock\n")
	fmt.Printf("  rate           %.6f (1.000000 = perfect realtime playback)\n", slope)
	fmt.Printf("  residual sd    %.2f ms\n", stddev*1000)
	fmt.Printf("  residual max   %.2f ms\n", maxAbs*1000)
	fmt.Printf("\nInterpretation: residual max is roughly the worst case error of a\n")
	fmt.Printf("naive linear clock. One frame at 24fps is 41.7 ms.\n")
}

func writeCSV(path string, s []sample) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	fmt.Fprintln(w, "idx,wall_s,rtt_us,media_pos_s")
	for _, v := range s {
		fmt.Fprintf(w, "%d,%.6f,%d,%.6f\n", v.idx, v.sendOff.Seconds(), v.rtt.Microseconds(), v.mediaPos)
	}
	return nil
}
