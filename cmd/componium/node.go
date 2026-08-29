package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Slicit/Componium/internal/cip"
)

// nodeCmd runs a software instrument node: everything the ESP32 firmware does,
// minus the pins.
//
// It exists so that the protocol can be exercised end to end without hardware,
// and so that somebody with no microcontroller at all can run a complete rig
// over the network. Same reason virtual instruments exist.
func nodeCmd(args []string) error {
	fs := flag.NewFlagSet("node", flag.ExitOnError)
	id := fs.String("id", "wind.main", "instrument id this node answers to")
	kind := fs.String("kind", "wind", "instrument kind")
	addr := fs.String("addr", fmt.Sprintf("0.0.0.0:%d", cip.Port), "address to listen on")
	latency := fs.Duration("latency", 1200*time.Millisecond, "latency this node declares")
	rampUp := fs.Duration("ramp-up", 1800*time.Millisecond, "ramp up time this node declares")
	timeout := fs.Duration("timeout", 300*time.Millisecond, "go safe after this long without a heartbeat")
	channel := fs.String("channel", "intensity", "the channel name this node accepts")
	fs.Parse(args)

	n, err := cip.NewNode(cip.NodeConfig{
		Addr:    *addr,
		Timeout: *timeout,
		Manifest: cip.Manifest{
			ID: *id, Kind: *kind,
			LatencyMS: cip.Ms(*latency),
			RampUpMS:  cip.Ms(*rampUp),
			SafeState: map[string]float64{*channel: 0},
			Channels: []cip.Channel{
				{Name: *channel, Unit: "normalised", Range: [2]float64{0, 1}},
			},
		},
	})
	if err != nil {
		return err
	}
	defer n.Close()

	fmt.Printf("node       %s (%s) on %s\n", *id, *kind, n.Addr())
	fmt.Printf("declares   %v latency, %v ramp up\n", *latency, *rampUp)
	fmt.Printf("watchdog   safe after %v without a heartbeat\n", *timeout)
	fmt.Println("ctrl-c to stop")
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		var lastCues, lastCurves int
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cues, curves, trips, safe := n.Stats()
				state := "running"
				if safe && trips == 0 && cues == 0 && curves == 0 {
					state = "idle"
				} else if safe {
					state = "SAFE"
				}
				fmt.Printf("%-8s cues %-5d (+%d)  curves %-7d (+%d)  watchdog trips %d  %v\n",
					state, cues, cues-lastCues, curves, curves-lastCurves, trips, n.State())
				lastCues, lastCurves = cues, curves
			}
		}
	}()

	err = n.Run(ctx)
	if err == context.Canceled {
		return nil
	}
	return err
}
