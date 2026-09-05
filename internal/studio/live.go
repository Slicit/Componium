package studio

import (
	"context"
	"fmt"
	"sync"
	"time"

	showclock "github.com/Slicit/componium/internal/clock"
	"github.com/Slicit/componium/internal/conductor"
	"github.com/Slicit/componium/internal/rig"
	"github.com/Slicit/componium/internal/safety"
	"github.com/Slicit/componium/internal/score"
	"github.com/Slicit/componium/internal/show"
	"github.com/Slicit/componium/internal/source"
)

// Driving the actual room from the studio's playhead.
//
// Until now the studio has been an editor and a simulator: it read the rig to
// draw a preview and never opened a socket to anything. The thing that drives
// hardware is `componium play`, and it follows mpv rather than the timeline you
// are editing. Which is right for a show and useless for the half hour where
// you are asking whether a cue lands on the frame you put it on.
//
// None of the timing here is new, and that is the interesting part. `show.Run`
// already turns any TimeSource into a disciplined clock with latency
// compensation, a curve driver and a safety supervisor, and `source.Studio` is
// a TimeSource fed by the page. What was needed was an adapter and a switch,
// not a second timing stack.
//
// THE SWITCH IS THE POINT
//
// Off by default, asked for explicitly, and it puts itself away. A rig left
// armed by somebody who wandered off is a fan running all night, which is the
// hazard the node's own watchdog exists for, one level up. So nothing is
// dispatched while the page is silent, and a page that stays silent takes the
// whole rig safe and disarms it.

// Quiet is how long the page can say nothing before the rig is put away.
//
// Long enough to survive a stall, a collection or a laptop lid; short enough
// that a closed tab does not leave anything running. The source stops answering
// after a second, so nothing is being dispatched well before this; this is what
// happens when the silence continues.
var Quiet = 5 * time.Second

// LiveState is what the page is told about the rig it is driving.
type LiveState struct {
	Armed bool `json:"armed"`
	// Problem is why arming failed, when it did.
	Problem string `json:"problem,omitempty"`
	Rig     string `json:"rig,omitempty"`
	// Real counts the instruments that will actually move something. Zero is
	// worth saying out loud: an armed rig of nothing but virtual devices looks
	// identical to a broken one from across a room.
	Real        int      `json:"real"`
	Instruments []string `json:"instruments,omitempty"`
	// Silent is true when the page has not reported recently. Shown, because
	// "armed and nothing happening" has two very different causes.
	Silent    bool     `json:"silent"`
	Media     float64  `json:"media"`
	Precision float64  `json:"precision"`
	Cues      int      `json:"cues"`
	Curves    int      `json:"curves"`
	Events    []string `json:"events,omitempty"`
}

// live holds everything that exists only while the rig is armed.
type live struct {
	src   *source.Studio
	sup   *safety.Supervisor
	cond  *conductor.Conductor
	curve *conductor.CurveDriver
	built *rig.Built
	stop  context.CancelFunc
	done  chan struct{}

	rigName string
	names   []string
	real    int

	mu        sync.Mutex
	on        bool
	media     time.Duration
	precision time.Duration
}

// armLive builds the rig and starts driving it from the page's playhead.
func (s *Server) armLive(cfg *rig.Config, sc *score.Score, name string) error {
	if cfg == nil {
		return fmt.Errorf("no rig is loaded, so there is nothing to drive")
	}
	if sc == nil {
		return fmt.Errorf("no score is open, so there is nothing to play")
	}
	s.disarmLive() // idempotent, and arming twice would open every socket twice

	// Where the secrets come from. A rig names a board and the boards file
	// knows how to authenticate to it, so the rig itself stays committable.
	cfg.UseSecrets(s.boards.SecretFor)
	built, err := cfg.Build()
	if err != nil {
		return err
	}
	if err := built.Collisions(); err != nil {
		built.Close()
		return err
	}

	// A score naming an instrument the rig does not have is worth saying now
	// rather than discovering halfway through a film.
	var missing []string
	for _, id := range sc.Instruments() {
		if _, ok := built.Instruments[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		built.Close()
		return fmt.Errorf("the score needs instruments this rig does not have: %v", missing)
	}

	l := &live{
		// The source stops answering a fifth of the way into the silence, so
		// nothing is dispatched long before the rig is put away.
		src:     &source.Studio{Stale: Quiet / 5},
		sup:     safety.New(safety.DefaultTimeout),
		cond:    conductor.New(),
		curve:   conductor.NewCurveDriver(20 * time.Millisecond),
		built:   built,
		done:    make(chan struct{}),
		rigName: name,
		on:      true,
	}
	for id, inst := range built.Instruments {
		/* Trimmed outside the guard, so the supervisor sends its own idea of
		 * safe exactly as it means it. A slider left at plus eighty must not
		 * be able to brighten a blackout. */
		guarded := trimmed{inner: l.sup.Guard(inst), of: s.trim.get}
		if err := l.cond.Register(guarded); err != nil {
			built.Close()
			return err
		}
		l.curve.Register(guarded)
		l.names = append(l.names, id)
	}
	for _, in := range cfg.Instruments {
		if in.Driver != "" && in.Driver != "virtual" {
			l.real++
		}
	}
	if err := l.cond.Load(sc.Cues()); err != nil {
		built.Close()
		return err
	}
	for _, tr := range sc.Curves() {
		t := tr
		l.curve.Add(conductor.CurveTrack{
			Instrument: t.Instrument,
			ValueAt:    func(at time.Duration) map[string]float64 { return t.ValueAt(at) },
		})
	}

	frame := time.Second / 24
	if fps := sc.Meta.Media.FPS; fps > 0 {
		frame = time.Duration(float64(time.Second) / fps)
	}

	ctx, stop := context.WithCancel(context.Background())
	l.stop = stop
	// The watchdog runs on its own goroutine deliberately. Checked from inside
	// the show loop it could never notice the show loop stopping, which is the
	// one failure it exists to catch.
	go safety.Watch(ctx, l.sup, 0)
	// See play: a cue driven light needs its universe retransmitted or it goes
	// dark on the receiver's timer rather than on the score's.
	go built.Keepalive(ctx)
	go l.run(ctx, stop, frame)

	s.liveMu.Lock()
	s.live = l
	s.liveProblem = ""
	s.liveMu.Unlock()
	return nil
}

// run drives the show until the context is cancelled or the page goes quiet.
func (l *live) run(ctx context.Context, stop context.CancelFunc, frame time.Duration) {
	defer close(l.done)

	clk := showclock.New(showclock.Config{FrameInterval: frame, PollInterval: pollInterval})
	var quietSince time.Time

	_ = show.Run(ctx, show.Config{
		Source: l.src, Clock: clk, Conductor: l.cond, PollInterval: pollInterval,
		OnReading: func(r showclock.Reading) {
			now := time.Now()
			l.sup.Heartbeat(now)
			l.built.Heartbeat()
			l.curve.Tick(now, r)

			l.mu.Lock()
			l.media, l.precision = r.Media, r.Precision
			l.mu.Unlock()

			// A page that has stopped reporting takes the rig with it. Nothing
			// is being dispatched by now, because the source stopped answering
			// a second ago; this is what happens when that continues.
			if !l.src.Silent() {
				quietSince = time.Time{}
				return
			}
			if quietSince.IsZero() {
				quietSince = now
				return
			}
			if now.Sub(quietSince) >= Quiet {
				stop()
			}
		},
	})

	// Leave the rig safe whatever happened. A page that was closed should not
	// leave a fan running any more than a crash should.
	l.sup.AllStop(time.Now(), safety.StopManual, "live output ended")
	l.built.Safe()
	l.built.Close()

	l.mu.Lock()
	l.on = false
	l.mu.Unlock()
}

// pollInterval is how often the show loop asks where playback is.
//
// Faster than the page reports, on purpose: the source interpolates between
// reports, so this is what decides how finely a cue can land rather than how
// often the browser speaks.
const pollInterval = 5 * time.Millisecond

// reportLive records where the page says its playhead is.
func (s *Server) reportLive(at time.Duration, playing bool) bool {
	s.liveMu.Lock()
	l := s.live
	s.liveMu.Unlock()
	if l == nil || !l.running() {
		return false
	}
	l.src.Report(at, playing, 0)
	return true
}

func (l *live) running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.on
}

// disarmLive puts the rig away. Safe to call when nothing is armed.
func (s *Server) disarmLive() {
	s.liveMu.Lock()
	l := s.live
	s.live = nil
	s.liveMu.Unlock()
	if l == nil {
		return
	}
	l.stop()
	<-l.done
}

// liveState describes what is armed, or why nothing is.
func (s *Server) liveState() LiveState {
	s.liveMu.Lock()
	l, problem := s.live, s.liveProblem
	s.liveMu.Unlock()
	if l == nil {
		return LiveState{Problem: problem}
	}
	l.mu.Lock()
	media, precision, on := l.media, l.precision, l.on
	l.mu.Unlock()

	out := LiveState{
		Armed: on, Rig: l.rigName, Real: l.real, Instruments: l.names,
		Silent:    l.src.Silent(),
		Media:     media.Seconds(),
		Precision: precision.Seconds(),
		Cues:      l.cond.Dispatched(),
		Curves:    l.curve.Sent(),
	}
	for _, e := range l.sup.Events() {
		out.Events = append(out.Events, string(e.Reason)+" "+e.Instrument+" "+e.Detail)
	}
	return out
}
