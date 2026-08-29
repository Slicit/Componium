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
	"github.com/Slicit/Componium/internal/rig"
	"github.com/Slicit/Componium/internal/score"
	"github.com/Slicit/Componium/internal/show"
	"github.com/Slicit/Componium/internal/source"
)

// logging wraps an instrument so that cue dispatches are visible. Curve
// updates are deliberately not logged: at 50Hz they would bury everything.
type logging struct{ inner instrument.Instrument }

func (l logging) Manifest() instrument.Manifest { return l.inner.Manifest() }

func (l logging) Dispatch(d instrument.Dispatch) error {
	if d.Cue.Action != "set" {
		fmt.Printf("CUE      %-14s %-8s cue at %-9s sent at %-9s %s early\n",
			d.Cue.Instrument, d.Cue.Action, fmtDur(d.Cue.At), fmtDur(d.Media),
			fmtDur(d.Cue.At-d.Media))
	}
	return l.inner.Dispatch(d)
}

func playCmd(args []string) error {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	scorePath := fs.String("score", "", "score file (required)")
	rigPath := fs.String("rig", "", "rig file (required)")
	socket := fs.String("socket", "/tmp/mpv.sock", "mpv IPC socket path")
	poll := fs.Duration("poll", show.DefaultPollInterval, "player polling interval")
	curveRate := fs.Duration("curve-rate", 20*time.Millisecond, "how often to send curve values")
	quiet := fs.Bool("quiet", false, "do not log individual cues")
	fs.Parse(args)

	if *scorePath == "" || *rigPath == "" {
		return fmt.Errorf("both -score and -rig are required")
	}

	rc, err := rig.Load(*rigPath)
	if err != nil {
		return err
	}
	built, err := rc.Build()
	if err != nil {
		return err
	}
	defer built.Close()

	sc, err := score.Load(*scorePath)
	if err != nil {
		return err
	}

	// A score naming an instrument the rig does not have is a mistake worth
	// stopping for, not something to discover halfway through a film.
	var missing []string
	for _, id := range sc.Instruments() {
		if _, ok := built.Instruments[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("score needs instruments this rig does not have: %v", missing)
	}

	src, err := source.DialMPV(*socket)
	if err != nil {
		return err
	}
	defer src.Close()

	frame, ok := waitForFrameRate(src)
	if !ok {
		if sc.Meta.Media.FPS > 0 {
			frame = time.Duration(float64(time.Second) / sc.Meta.Media.FPS)
		} else {
			frame = time.Second / 24
		}
	}

	cond := conductor.New()
	curves := conductor.NewCurveDriver(*curveRate)
	for id, inst := range built.Instruments {
		var use instrument.Instrument = inst
		if !*quiet {
			use = logging{inner: inst}
		}
		if err := cond.Register(use); err != nil {
			return err
		}
		curves.Register(use)
		_ = id
	}
	if err := cond.Load(sc.Cues()); err != nil {
		return err
	}
	for _, tr := range sc.Curves() {
		t := tr
		curves.Add(conductor.CurveTrack{
			Instrument: t.Instrument,
			ValueAt:    func(at time.Duration) map[string]float64 { return t.ValueAt(at) },
		})
	}

	fmt.Printf("rig        %s, %d instruments\n", rc.Rig.Name, len(built.Instruments))
	fmt.Printf("score      %s\n", sc.Meta.Title)
	fmt.Printf("           %d cues, %d curves\n", len(sc.Cues()), len(sc.Curves()))
	fmt.Printf("frame      %v\n", frame.Round(time.Microsecond))
	if n := len(cond.Unreachable()); n > 0 {
		fmt.Printf("warning    %d cue(s) cannot be dispatched early enough for their instrument\n", n)
	}
	fmt.Println()

	clk := clock.New(clock.Config{FrameInterval: frame, PollInterval: *poll})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var lastPrint time.Time
	err = show.Run(ctx, show.Config{
		Source: src, Clock: clk, Conductor: cond, PollInterval: *poll,
		OnReading: func(r clock.Reading) {
			now := time.Now()
			curves.Tick(now, r)
			if now.Sub(lastPrint) < time.Second {
				return
			}
			lastPrint = now
			fmt.Printf("%-8s media %-11s precision %-8s cues %-4d curves %d\n",
				r.State, fmtDur(r.Media), r.Precision.Round(100*time.Microsecond),
				cond.Dispatched(), curves.Sent())
		},
	})
	summarise(cond, clk)
	fmt.Printf("curve updates %d\n", curves.Sent())
	if err == context.Canceled {
		return nil
	}
	return err
}
