// Command componium drives physical effects in time with a film.
//
// Only rehearse exists so far: it runs the whole chain against a real player
// with virtual instruments, printing what a rig would have been told. Nothing
// here touches hardware.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Slicit/Componium/internal/clock"
	"github.com/Slicit/Componium/internal/conductor"
	"github.com/Slicit/Componium/internal/instrument"
	"github.com/Slicit/Componium/internal/show"
	"github.com/Slicit/Componium/internal/source"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validateCmd(os.Args[2:])
	case "play":
		err = playCmd(os.Args[2:])
	case "rehearse":
		err = rehearse(os.Args[2:])
	case "tune":
		err = tuneCmd(os.Args[2:])
	case "doctor":
		err = doctorCmd(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "componium: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `componium drives physical effects in time with a film.

usage:
  componium play [flags]        play a score against a rig
  componium validate [flags]    check a score, optionally against a rig
  componium rehearse [flags]    dry run against a player, with virtual instruments
  componium tune [flags]        measure this machine and player, and cache a profile
  componium doctor [flags]      print the cached profile and what it means

rehearse flags:
  -socket   mpv IPC socket path        (default /tmp/mpv.sock)
  -every    interval between demo cues (default 5s)
  -latency  virtual instrument latency (default 1.2s)
  -poll     player polling interval    (default 5ms)

Start mpv with:
  mpv --input-ipc-server=/tmp/mpv.sock film.mkv
`)
}

// logInstrument prints what it is told instead of doing anything physical.
type logInstrument struct{ m instrument.Manifest }

func (l *logInstrument) Manifest() instrument.Manifest { return l.m }

func (l *logInstrument) Dispatch(d instrument.Dispatch) error {
	early := d.Cue.At - d.Media
	fmt.Printf("CUE      %-11s %-6s cue at %-8s sent at %-9s %s early, precision %s\n",
		d.Cue.Instrument, d.Cue.Action,
		fmtDur(d.Cue.At), fmtDur(d.Media), fmtDur(early),
		d.Precision.Round(100*time.Microsecond))
	return nil
}

func fmtDur(d time.Duration) string { return d.Round(time.Millisecond).String() }

// waitForFrameRate retries briefly, because a player that has only just been
// connected to may not have loaded a file yet and will report no frame rate at
// all for the first fraction of a second.
func waitForFrameRate(src *source.MPV) (time.Duration, bool) {
	for i := 0; i < 40; i++ {
		if f, ok := src.FrameInterval(); ok {
			return f, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, false
}

func rehearse(args []string) error {
	fs := flag.NewFlagSet("rehearse", flag.ExitOnError)
	socket := fs.String("socket", "/tmp/mpv.sock", "mpv IPC socket path")
	every := fs.Duration("every", 5*time.Second, "interval between demo cues")
	latency := fs.Duration("latency", 1200*time.Millisecond, "virtual instrument latency")
	poll := fs.Duration("poll", show.DefaultPollInterval, "player polling interval")
	fs.Parse(args)

	src, err := source.DialMPV(*socket)
	if err != nil {
		return err
	}
	defer src.Close()

	frame, ok := waitForFrameRate(src)
	if !ok {
		frame = time.Second / 24
		fmt.Println("player never reported a frame rate, assuming 24fps")
	}
	dur, haveDur := src.Duration()

	fmt.Printf("source     %s on %s\n", src.Name(), *socket)
	fmt.Printf("frame      %v (%.3f fps)\n", frame.Round(time.Microsecond), float64(time.Second)/float64(frame))
	if haveDur {
		fmt.Printf("duration   %s\n", fmtDur(dur))
	}
	fmt.Printf("polling    every %v\n", *poll)

	clk := clock.New(clock.Config{FrameInterval: frame, PollInterval: *poll})

	inst := &logInstrument{m: instrument.Manifest{
		ID: "wind.main", Kind: "wind", Latency: *latency,
		Ramp: instrument.Ramp{Up: 1800 * time.Millisecond, Down: 3 * time.Second},
	}}
	cond := conductor.New()
	if err := cond.Register(inst); err != nil {
		return err
	}

	horizon := 30 * time.Minute
	if haveDur {
		horizon = dur
	}
	var cues []instrument.Cue
	for at := *every; at < horizon; at += *every {
		cues = append(cues, instrument.Cue{
			At: at, Instrument: "wind.main", Action: "gust",
			Params: map[string]float64{"intensity": 0.8},
		})
	}
	if err := cond.Load(cues); err != nil {
		return err
	}
	fmt.Printf("cues       %d, every %v, for %q with %v of latency\n",
		len(cues), *every, inst.m.ID, *latency)
	if n := len(cond.Unreachable()); n > 0 {
		fmt.Printf("unreachable %d cue(s) too early for this instrument to reach in time\n", n)
	}
	fmt.Println("ctrl-c to stop")
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var lastPrint time.Time
	err = show.Run(ctx, show.Config{
		Source: src, Clock: clk, Conductor: cond, PollInterval: *poll,
		OnReading: func(r clock.Reading) {
			now := time.Now()
			if now.Sub(lastPrint) < time.Second {
				return
			}
			lastPrint = now
			rate, settled := clk.Rate()
			mark := " "
			if !settled {
				mark = "~"
			}
			fmt.Printf("%-8s media %-10s precision %-8s rate %s%.6f  anchors %-4d dispatched %d\n",
				r.State, fmtDur(r.Media), r.Precision.Round(100*time.Microsecond),
				mark, rate, clk.Anchors(), cond.Dispatched())
		},
	})
	summarise(cond, clk)
	if err == context.Canceled {
		return nil
	}
	return err
}

func summarise(c *conductor.Conductor, clk *clock.Clock) {
	fmt.Printf("\ndispatched %d, pending %d, discontinuities %d\n",
		c.Dispatched(), c.Pending(), clk.Discontinuities)
	skips := c.Skips()
	if len(skips) == 0 {
		return
	}
	counts := map[string]int{}
	for _, s := range skips {
		counts[s.Reason.String()]++
	}
	fmt.Printf("skipped %d:\n", len(skips))
	for reason, n := range counts {
		fmt.Printf("  %-22s %d\n", reason, n)
	}
}
