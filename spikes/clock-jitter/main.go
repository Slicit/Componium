// Command clock-jitter measures how well a media player's reported playback
// position can be turned into a usable media clock.
//
// The question it answers: if we poll the player for its position, how
// accurately can we predict media time between polls? That number decides how
// much filtering the conductor needs, and whether frame-accurate cueing from a
// polled source is possible at all.
//
// Usage:
//
//	mpv --input-ipc-server=/tmp/mpv.sock video.mkv
//	go run ./spikes/clock-jitter -source mpv -socket /tmp/mpv.sock
//
//	vlc --extraintf http --http-password pw video.mkv
//	go run ./spikes/clock-jitter -source vlc -password pw
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
	"net/http"
	"os"
	"sort"
	"time"
)

// A poller returns the player's current media position in seconds.
type poller interface {
	pos() (float64, error)
	close() error
}

type sample struct {
	sendOff  time.Duration
	rtt      time.Duration
	mediaPos float64
}

func main() {
	source := flag.String("source", "mpv", "player to sample: mpv or vlc")
	socket := flag.String("socket", "/tmp/mpv.sock", "mpv: IPC socket path")
	url := flag.String("url", "http://127.0.0.1:8080", "vlc: HTTP interface base URL")
	password := flag.String("password", "componium", "vlc: HTTP interface password")
	rate := flag.Float64("rate", 10, "polls per second")
	duration := flag.Duration("duration", 60*time.Second, "how long to sample")
	out := flag.String("out", "", "optional CSV output path")
	flag.Parse()

	var p poller
	var err error
	switch *source {
	case "mpv":
		p, err = newMPV(*socket)
	case "vlc":
		p, err = newVLC(*url, *password)
	default:
		fmt.Fprintf(os.Stderr, "unknown -source %q, want mpv or vlc\n", *source)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer p.close()

	interval := time.Duration(float64(time.Second) / *rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	deadline := start.Add(*duration)
	var samples []sample
	skipped := 0

	fmt.Fprintf(os.Stderr, "sampling %s at %.1f Hz from %s...\n", *duration, *rate, *source)

	for now := range ticker.C {
		if now.After(deadline) {
			break
		}
		sendAt := time.Now()
		pos, err := p.pos()
		recvAt := time.Now()
		if err != nil {
			skipped++
			continue
		}
		samples = append(samples, sample{
			sendOff:  sendAt.Sub(start),
			rtt:      recvAt.Sub(sendAt),
			mediaPos: pos,
		})
	}

	if len(samples) < 10 {
		fmt.Fprintf(os.Stderr, "only %d usable samples (%d skipped). Is a file actually playing?\n", len(samples), skipped)
		os.Exit(1)
	}
	report(*source, samples, skipped)
	if *out != "" {
		if err := writeCSV(*out, samples); err != nil {
			fmt.Fprintf(os.Stderr, "csv: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nwrote %s\n", *out)
	}
}

// --- mpv, over its JSON IPC socket ---

type mpvPoller struct {
	conn  net.Conn
	r     *bufio.Reader
	reqID int
}

func newMPV(socket string) (poller, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("%s: %w (is mpv running with --input-ipc-server?)", socket, err)
	}
	return &mpvPoller{conn: conn, r: bufio.NewReader(conn)}, nil
}

type mpvReply struct {
	Data      *float64 `json:"data"`
	RequestID int      `json:"request_id"`
	Error     string   `json:"error"`
	Event     string   `json:"event"`
}

func (m *mpvPoller) pos() (float64, error) {
	m.reqID++
	cmd := fmt.Sprintf("{\"command\":[\"get_property\",\"time-pos\"],\"request_id\":%d}\n", m.reqID)
	if _, err := m.conn.Write([]byte(cmd)); err != nil {
		return 0, err
	}
	// Read until the matching response, discarding the asynchronous event
	// messages mpv interleaves on the same socket.
	for {
		line, err := m.r.ReadBytes('\n')
		if err != nil {
			return 0, err
		}
		var rep mpvReply
		if err := json.Unmarshal(line, &rep); err != nil {
			continue
		}
		if rep.Event != "" || rep.RequestID != m.reqID {
			continue
		}
		if rep.Error != "success" || rep.Data == nil {
			return 0, fmt.Errorf("mpv: %s", rep.Error)
		}
		return *rep.Data, nil
	}
}

func (m *mpvPoller) close() error { return m.conn.Close() }

// --- VLC, over its HTTP interface ---
//
// VLC reports "time" as an integer number of seconds, far too coarse to be
// useful. "position" is a float fraction of the total duration, so the usable
// media time is position*length. Any error in length is a scale error, which
// the linear fit in report() absorbs into the rate term.

type vlcPoller struct {
	client   *http.Client
	url      string
	password string
	length   float64
}

func newVLC(base, password string) (poller, error) {
	v := &vlcPoller{
		client:   &http.Client{Timeout: 5 * time.Second},
		url:      base + "/requests/status.json",
		password: password,
	}
	if _, err := v.pos(); err != nil {
		return nil, fmt.Errorf("%s: %w (is VLC running with --extraintf http?)", base, err)
	}
	return v, nil
}

type vlcStatus struct {
	State    string  `json:"state"`
	Position float64 `json:"position"`
	Length   float64 `json:"length"`
	Rate     float64 `json:"rate"`
}

func (v *vlcPoller) pos() (float64, error) {
	req, err := http.NewRequest("GET", v.url, nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth("", v.password)
	resp, err := v.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("vlc: HTTP %d", resp.StatusCode)
	}
	var st vlcStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return 0, err
	}
	if st.Length > 0 {
		v.length = st.Length
	}
	if v.length <= 0 || st.State != "playing" {
		return 0, fmt.Errorf("vlc: state=%s length=%v", st.State, st.Length)
	}
	return st.Position * v.length, nil
}

func (v *vlcPoller) close() error {
	v.client.CloseIdleConnections()
	return nil
}

// --- analysis ---

// report fits media position against wall time and reports the residuals.
// The residual is the number that matters: it is how far off we would be if we
// predicted media time by linear extrapolation between polls.
func report(source string, s []sample, skipped int) {
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

	var sumSq, maxAbs float64
	for _, v := range s {
		e := v.mediaPos - (slope*v.sendOff.Seconds() + intercept)
		sumSq += e * e
		if math.Abs(e) > maxAbs {
			maxAbs = math.Abs(e)
		}
	}
	stddev := math.Sqrt(sumSq / n)

	// The smallest non-zero step between consecutive readings reveals the
	// granularity the player actually exposes, which is usually the real story.
	minStep := math.Inf(1)
	for i := 1; i < len(s); i++ {
		d := math.Abs(s[i].mediaPos - s[i-1].mediaPos)
		if d > 1e-9 && d < minStep {
			minStep = d
		}
	}

	rtts := make([]time.Duration, len(s))
	for i, v := range s {
		rtts[i] = v.rtt
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })

	fmt.Printf("source           %s\n", source)
	fmt.Printf("samples          %d (%d skipped)\n", len(s), skipped)
	fmt.Printf("span             %.1f s of wall time\n", s[len(s)-1].sendOff.Seconds()-s[0].sendOff.Seconds())
	fmt.Printf("\nQuery round trip\n")
	fmt.Printf("  p50            %s\n", rtts[len(rtts)/2])
	fmt.Printf("  p95            %s\n", rtts[len(rtts)*95/100])
	fmt.Printf("  max            %s\n", rtts[len(rtts)-1])
	fmt.Printf("\nMedia clock vs wall clock\n")
	fmt.Printf("  rate           %.6f (1.000000 = perfect realtime playback)\n", slope)
	fmt.Printf("  min step       %.2f ms (smallest observed change in reported position)\n", minStep*1000)
	fmt.Printf("  residual sd    %.2f ms\n", stddev*1000)
	fmt.Printf("  residual max   %.2f ms\n", maxAbs*1000)
	fmt.Printf("\nOne frame at 24fps is 41.7 ms.\n")
}

func writeCSV(path string, s []sample) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	fmt.Fprintln(w, "wall_s,rtt_us,media_pos_s")
	for _, v := range s {
		fmt.Fprintf(w, "%.6f,%d,%.6f\n", v.sendOff.Seconds(), v.rtt.Microseconds(), v.mediaPos)
	}
	return nil
}
