package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Slicit/Componium/internal/source"
	"github.com/Slicit/Componium/internal/tune"
)

func machineName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}

func tuneCmd(args []string) error {
	fs := flag.NewFlagSet("tune", flag.ExitOnError)
	socket := fs.String("socket", "/tmp/mpv.sock", "mpv IPC socket path")
	poll := fs.Duration("poll", 5*time.Millisecond, "polling interval to characterise")
	dur := fs.Duration("duration", 10*time.Second, "how long to measure each stage")
	out := fs.String("out", "", "where to write the profile (default: the cache path)")
	fs.Parse(args)

	machine := machineName()

	fmt.Printf("measuring scheduler lateness at %v for %v...\n", *poll, *dur/2)
	timer := tune.MeasureTimer(*poll, *dur/2)
	fmt.Printf("  %s\n\n", timer)

	src, err := source.DialMPV(*socket)
	if err != nil {
		return err
	}
	defer src.Close()

	version, _ := src.Version()
	fmt.Printf("measuring %s for %v (it must be playing)...\n", src.Name(), *dur)
	rep := tune.MeasureSource(src, *poll, *dur)
	fmt.Printf("  query        %s\n", rep.Query)
	fmt.Printf("  update every %v\n", rep.UpdatePeriod.Round(time.Microsecond))
	fmt.Printf("  pacing       %.0f ppm from realtime\n", rep.RatePPM)
	if rep.Samples == 0 {
		return fmt.Errorf("no usable samples: is the player actually playing?")
	}
	if rep.Unavailable > 0 {
		fmt.Printf("  %d polls had no position\n", rep.Unavailable)
	}

	p := &tune.Profile{
		Machine: machine, Player: src.Name(), PlayerVersion: version,
		Created: time.Now().UTC(),
		Timer:   timer, Query: rep.Query,
		UpdatePeriod: rep.UpdatePeriod, RateStabilityPPM: rep.RatePPM,
		PollInterval: *poll,
	}
	p.Estimate()

	path := *out
	if path == "" {
		path = tune.DefaultPath(machine, src.Name())
	}
	if err := p.Save(path); err != nil {
		return err
	}
	fmt.Printf("\nachievable precision %v\n", p.Achievable.Round(100*time.Microsecond))
	fmt.Printf("written to %s\n", path)
	return nil
}

func doctorCmd(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	player := fs.String("player", "mpv", "which player's profile to read")
	in := fs.String("in", "", "profile path (default: the cache path)")
	fs.Parse(args)

	machine := machineName()
	path := *in
	if path == "" {
		path = tune.DefaultPath(machine, *player)
	}
	p, err := tune.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no profile for %s with %s. Run: componium tune", machine, *player)
		}
		return err
	}

	fmt.Printf("profile    %s\n", path)
	fmt.Printf("machine    %s\n", p.Machine)
	fmt.Printf("player     %s %s\n", p.Player, p.PlayerVersion)
	fmt.Printf("measured   %s\n\n", p.Created.Local().Format(time.RFC1123))

	fmt.Printf("scheduler lateness   %s\n", p.Timer)
	fmt.Printf("  the mean is a bias the conductor could subtract; the spread it cannot\n")
	fmt.Printf("player query cost    %s\n", p.Query)
	fmt.Printf("position granularity %v\n", p.UpdatePeriod.Round(time.Microsecond))
	fmt.Printf("playback pacing      %.0f ppm from realtime\n\n", p.RateStabilityPPM)

	fmt.Printf("achievable precision %v at a %v poll\n",
		p.Achievable.Round(100*time.Microsecond), p.PollInterval)
	explain(p)
	return nil
}

// explain turns the numbers into the thing an operator actually wants to know,
// which is what this rig can and cannot be trusted to do.
func explain(p *tune.Profile) {
	frame24 := time.Second / 24
	fmt.Println()
	switch {
	case p.Achievable <= frame24/4:
		fmt.Printf("Good enough for anything, including shake and motion:\n")
		fmt.Printf("  %v is well inside a quarter of a frame at 24fps.\n", p.Achievable.Round(100*time.Microsecond))
	case p.Achievable <= frame24:
		fmt.Printf("Good enough for most effects. %v is inside one frame at 24fps,\n", p.Achievable.Round(time.Millisecond))
		fmt.Printf("  but frame critical cues may occasionally be a frame out.\n")
	default:
		fmt.Printf("Too coarse for frame critical effects. %v exceeds a frame at 24fps.\n", p.Achievable.Round(time.Millisecond))
		fmt.Printf("  Slow effects are unaffected: a fogger with seconds of its own lag\n")
		fmt.Printf("  does not care. Shake and motion cues will be refused.\n")
	}
	if p.UpdatePeriod > 100*time.Millisecond {
		fmt.Printf("\nThis player reports position only every %v, which is the limiting\n", p.UpdatePeriod.Round(time.Millisecond))
		fmt.Printf("factor rather than the machine. A player that updates per frame would\n")
		fmt.Printf("do considerably better on the same hardware.\n")
	}
}
