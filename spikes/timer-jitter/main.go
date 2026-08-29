// Command timer-jitter measures how late this machine actually fires a timer.
//
// This is the floor on dispatch accuracy. However good the media clock is, a
// cue can only be dispatched when the scheduler actually wakes up, so if a 5 ms
// ticker routinely fires 20 ms late then 5 ms of clock precision buys nothing.
//
// Reported as a mean, which is a bias the conductor could subtract, and a
// spread, which it cannot. See LOGBOOK/features/feat-tuning.md.
//
// This is a spike. It is not part of the build and it is not tested.
package main

import (
	"flag"
	"fmt"
	"math"
	"sort"
	"time"
)

func main() {
	rate := flag.Float64("rate", 200, "ticks per second")
	duration := flag.Duration("duration", 20*time.Second, "how long to measure")
	flag.Parse()

	interval := time.Duration(float64(time.Second) / *rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	deadline := start.Add(*duration)
	var late []time.Duration
	n := 0

	fmt.Printf("ticking every %s for %s...\n\n", interval, *duration)

	for now := range ticker.C {
		if now.After(deadline) {
			break
		}
		n++
		// Where the tick should have landed, measured from the start rather
		// than from the previous tick, so that error does not accumulate.
		want := start.Add(time.Duration(n) * interval)
		late = append(late, time.Since(want))
	}

	if len(late) < 10 {
		fmt.Println("too few ticks")
		return
	}
	sort.Slice(late, func(i, j int) bool { return late[i] < late[j] })

	var sum float64
	for _, d := range late {
		sum += d.Seconds()
	}
	mean := sum / float64(len(late))
	var sq float64
	for _, d := range late {
		e := d.Seconds() - mean
		sq += e * e
	}
	sd := math.Sqrt(sq / float64(len(late)))

	p := func(q int) time.Duration { return late[len(late)*q/100] }

	fmt.Printf("ticks            %d at %.0f Hz\n", len(late), *rate)
	fmt.Printf("\nLateness (actual wake minus intended wake)\n")
	fmt.Printf("  mean           %.3f ms   (bias, compensatable)\n", mean*1000)
	fmt.Printf("  sd             %.3f ms   (spread, not compensatable)\n", sd*1000)
	fmt.Printf("  p50            %.3f ms\n", p(50).Seconds()*1000)
	fmt.Printf("  p95            %.3f ms\n", p(95).Seconds()*1000)
	fmt.Printf("  p99            %.3f ms\n", p(99).Seconds()*1000)
	fmt.Printf("  max            %.3f ms\n", late[len(late)-1].Seconds()*1000)
	fmt.Printf("  min            %.3f ms\n", late[0].Seconds()*1000)
}
