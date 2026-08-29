---
status: shipped
branch: feat-timing-core
---

# Timing core

## Intent

Everything in Componium rests on one unverified assumption: that a media
player's reported position can be disciplined into a clock good enough to land
a cue on a frame. Until that is measured, every downstream design choice (how
much filtering is needed, whether instruments need their own synchronised
clock, whether 50 Hz curve frames suffice) is guesswork. This feature measures
it, then builds the scheduler that depends on the answer.

## Plan

1. **Measure first.** `spikes/clock-jitter` drives mpv over its IPC socket,
   polls `time-pos`, and reports how far a naive linear clock drifts between
   polls. mpv deliberately, because its IPC answers on demand with sub frame
   precision. Kodi and Plex are coarser and come later; mpv is the best case,
   and if the best case is bad we need to know on day one.
2. Define the `TimeSource` interface from what the numbers show, not from what
   seems reasonable now. Shape is roughly
   `Now() (mediaTime, wallClock, rate, precision)`, where precision is measured per source rather than assumed.
3. Clock: anchor on frame transitions (see the 2026-08-29 spike results), resync on
   seek and pause, expose confidence so the conductor can refuse to fire cues
   when the clock is not trustworthy.
4. Conductor skeleton: clock, scheduler, instrument registry.
5. Virtual instrument that records what it was told and when.
6. **The test that is the point of the milestone:** an instrument declaring
   1200 ms latency receives its cue 1200 ms before the cue's score time.
   Deterministic, driven by a fake clock, no mpv in the test path.

## Decisions

### 2026-08-29

- **Decision:** Measure before building. The spike is the first commit of this
  feature, not a side quest.
- **Why:** The filter design depends entirely on observed jitter. Building a
  PLL before knowing whether one is needed risks solving a problem that does
  not exist, or under building for one that does.
- **Impact:** `spikes/clock-jitter` is throwaway and excluded from the build.
  Its findings get appended here as a dated decision.

### 2026-08-29

- **Decision:** One Go module at the repository root,
  `github.com/Slicit/Componium`, with the other languages alongside rather
  than inside it.
- **Why:** The Go parts are one program. `composer/`, `studio/` and
  `firmware/` carry their own toolchains and do not belong in a Go module.
- **Impact:** See `REPO.md`. Note the module path carries a capital C to match
  the repository, which Go proxy escapes as `!componium`. Renaming the
  repository to lowercase would avoid that and is cheapest to do now.

### 2026-08-29 · Clock spike results, and what they mean for the filter

Measured against mpv 0.40 on claude-machine-02, headless (`--vo=null
--ao=null`), local file, idle box, no seeking or pausing.

**IPC is effectively free.** Round trip to query `time-pos` is 52 to 82 us at
the median and under 200 us at the maximum, across every run. Polling at 200 Hz
costs about one percent of a core.

**Playback pacing is near perfect.** Measured rate was between 1.000009 and
1.000282 (9 to 282 parts per million). Over a three hour film the worst of
those is about three seconds of drift, which any anchoring scheme removes.

**All of the observed error is frame quantisation.** `time-pos` reports the
presentation timestamp of the frame currently on screen, so it is exact but
stale by up to one frame interval. Predicted standard deviation for uniform
quantisation is `interval / sqrt(12)`:

| clip | frame interval | predicted sd | measured sd |
|---|---|---|---|
| 24 fps | 41.7 ms | 12.0 ms | 12.11 ms |
| 30 fps | 33.3 ms | 9.6 ms | 9.87 ms |
| 60 fps | 16.7 ms | 4.8 ms | 6.87 ms |

Confirmed by polling rate independence: at 24 fps, sampling at 10 Hz gave a
residual sd of 12.18 ms and sampling at 200 Hz gave 12.16 ms. Twenty times the
samples, no improvement. That is only possible if the error is quantisation of
the reported value rather than noise in sampling it.

- **Decision:** No PLL. The clock anchors on frame transitions rather than
  filtering noise.
- **Why:** Averaging treats quantisation as noise to be smoothed, which is the
  wrong model. The value is not noisy, it is stale by a known bounded amount.
  Polling faster than the frame rate and detecting the instant `time-pos` steps
  to a new value yields an anchor whose media time is exact and whose wall
  clock instant is known to within one polling interval. The error bound
  becomes the polling interval, not the frame interval: 5 ms at 200 Hz against
  41.7 ms for a single naive sample. Cheap, because the IPC costs 52 us.
- **Impact:** `internal/clock` implements edge anchoring plus a rate estimate
  from the anchor history, not a phase locked loop. Simpler than planned, and
  roughly eight times more accurate than a single sample at 24 fps.

**Caveats, none of them addressed yet.** The 60 fps case measured 6.87 ms
against a predicted 4.8 ms, so there is a small additional term at high frame
rates that quantisation alone does not explain, and pacing was also worst in
that run. Conditions throughout were favourable: local file, null video and
audio output, an otherwise idle machine, and no seeks or pauses. Real decode
and display, a loaded box, seeking, and variable frame rate content are all
untested. Seek and pause behaviour is the next thing to measure, because that
is where an anchoring clock is most likely to break.

### 2026-08-29 · VLC measured, and why TimeSource must report its own precision

VLC 3.0.23 over its HTTP interface (`--extraintf http`, `/requests/status.json`),
same 24 fps clip and same machine as the mpv runs.

First, a correction to how to read these numbers: `min step` is only meaningful
when polling faster than the player updates. The earlier mpv figure of 83 ms was
a sampling artifact of polling at 10 Hz. At 200 Hz mpv shows a true step of
**41.00 ms**, which is exactly one frame at 24 fps, confirming that mpv updates
its reported position every single frame.

| | mpv (JSON IPC) | VLC 3.0.23 (HTTP) |
|---|---|---|
| query round trip, p50 | 53 us | 21 ms |
| position update period | 41 ms, every frame | 247 ms, about 4 Hz |
| naive residual sd | 12.2 ms | 74 ms |
| naive residual max | 28 ms | 162 ms |

VLC is roughly **six times worse on quantisation and four hundred times worse
on query cost**. Two things make it worse than that table suggests:

- Its position updates about four times a second no matter how fast it is
  polled. The 247 ms step was identical at 10 Hz and at 45 Hz.
- Its HTTP interface saturates. Asking for 45 Hz produced only 313 samples in
  20 seconds (about 15 Hz actual) and the median round trip degraded from 21 ms
  to 64 ms. Polling harder makes it slower, so there is no way to buy precision
  with poll rate.

The quantisation model from the mpv runs holds here too: predicted sd for
uniform quantisation over a 247 ms update is 247/sqrt(12) = 71.3 ms against 74
measured.

- **Decision:** mpv is the reference `TimeSource`. VLC is supported, but
  `TimeSource` must expose an explicit precision estimate, and the conductor
  must refuse to fire cues whose required precision it cannot meet.
- **Why:** VLC is not unusable, it is unusable *for some effects*. Roughly 70 ms
  of clock error is irrelevant to a fogger with 1 to 3 seconds of its own lag,
  or a fan with 800 to 2000 ms of spin up. It is fatal to a bass shaker or a
  motion cue, where the whole point is landing on the frame. Silently degrading
  a motion track to VLC precision would feel broken with no visible cause.
- **Impact:** `TimeSource` gains a precision field alongside media time, wall
  clock and rate. Instruments, or tracks, declare the precision they require.
  This falls out of the existing principle that the conductor never assumes
  what is on the other end of the wire: it now also declines to assume what is
  on the other end of the clock.

Edge anchoring still helps VLC, since playback pacing is stable to about 230
ppm and extrapolating 250 ms from an anchor costs well under a millisecond.
The floor is the anchor timestamp uncertainty, which for VLC is its round trip
plus the poll interval, so roughly 25 to 70 ms depending on poll rate. Around
20 Hz looks like the sweet spot, untested.

Not measured, and worth a follow up: VLC also exposes MPRIS over D-Bus, which
reports position in microseconds and may update more often than the HTTP
interface. The HTTP path was measured first because it is the cross platform
one, and Componium should not require D-Bus.

### 2026-08-29 · Seek, pause and real load measured. The design holds.

Three remaining unknowns, all closed on machine-02.

**Host timer jitter** (`spikes/timer-jitter`, 200 Hz, 15 s). This is the floor
on dispatch accuracy: clock precision is worthless if the scheduler wakes late.

| | idle | 4 busy loops on 2 cores |
|---|---|---|
| mean lateness | 0.576 ms | 0.587 ms |
| sd | 0.316 ms | 0.484 ms |
| p99 | 1.158 ms | 3.069 ms |
| max | 1.209 ms | 4.096 ms |

The mean is a stable bias and barely moves under load. Only the tail degrades,
and even fully saturated the worst case is 4.1 ms, a tenth of a frame.

**Real decode load** (`spikes/clock-jitter`, 1080p24, 200 Hz). Every earlier
number came from a 640x360 clip on an idle box, so this was the caveat most
likely to bite.

| | 1080p idle | 1080p plus saturated CPU |
|---|---|---|
| IPC round trip p50 | 52.9 us | 41.5 us |
| position step | 41.00 ms | 41.00 ms |
| residual sd | 12.19 ms | 12.21 ms |
| residual max | 28.33 ms | 39.87 ms |

Residual sd is unchanged from the 640x360 idle measurement of 12.2 ms.
Quantisation dominates regardless of resolution or load, and the IPC stays in
the tens of microseconds even with the box oversubscribed. Only the tail moves,
to 39.87 ms, still just inside one frame.

**Seek and pause** (`spikes/player-events`, 100 Hz, scripted).

- A relative seek of +30 s was reflected in the reported position on the very
  next sample, 10 ms later, as an exact 6.000 to 36.000 jump. An absolute seek
  backwards behaved identically. No transient values, no unavailable window, no
  overshoot. Seek detection is easy.
- Pause freezes the position exactly. Media advanced 0.000 s across 2.990 s of
  wall time. A clock that failed to notice would have been **2.99 seconds
  wrong** by the end, which makes pause the single most destructive event for a
  naive clock and also the easiest to detect.
- The frame staircase is directly visible in the trace: position holds flat for
  about four samples at 100 Hz, then steps by exactly 0.041 to 0.042 s. That is
  the anchor edge, observed rather than inferred.

**Error budget for the anchoring clock.** Anchor timestamp uncertainty is the
polling interval plus the timer tail: 5 ms at 200 Hz plus 4.1 ms worst case
under load, so about 9 ms. Against 41.7 ms for one frame at 24 fps, and against
the 12.2 ms sd of a single naive sample. The approach is sound.

- **Decision:** Discontinuity thresholds are expressed in frame intervals, not
  in fixed milliseconds.
- **Why:** `spikes/player-events` used a fixed 25 ms threshold and duly flagged
  every normal 41.7 ms frame step as a discontinuity. At 24 fps a single frame
  advance exceeds any fixed threshold tight enough to catch a small seek. The
  comparison has to be against expected advance, in units of the content's own
  frame interval.
- **Impact:** `internal/clock` takes the frame interval as an input, derived
  per score from the media rather than measured. Stall detection is media time
  not advancing for more than about two frame intervals while wall time does;
  seek detection is media advance differing from wall advance by more than a
  few frame intervals.

mpv can also push notifications via `observe_property`, which would remove the
detection delay entirely. VLC has no equivalent, so polling based detection has
to exist regardless. Push is an optimisation for one source, not the mechanism.

### 2026-08-29 · internal/clock implemented, 3.34 ms worst case in test

Edge anchoring built and tested against a synthetic staircase source, with no
player, no socket and no sleeping. Worst prediction error across five seconds
of simulated playback is **3.34 ms**, against 41.67 ms for one frame at 24 fps
and 12.2 ms sd for a single naive sample. About twelve times better than
sampling naively, and better than the 9 ms the error budget predicted, because
the budget assumed worst case scheduler lateness on every sample rather than
occasionally.

- **Decision:** The clock is passive. It polls nothing, starts no goroutine and
  calls no timer of its own. The caller feeds it `Sample(wall, pos, ok)` and
  asks `At(wall)`, both with wall time passed in explicitly.
- **Why:** Every behaviour that matters here is a time dependent state machine,
  and time dependent state machines that read the clock internally can only be
  tested with sleeps. Passing wall time in makes pause detection, seek recovery
  and rate convergence testable deterministically and instantly. The whole test
  suite runs in 3 ms.
- **Impact:** The polling loop lives in the caller, which also owns the poll
  rate. `Config.PollInterval` is only a floor on reported precision.

- **Decision:** `Precision` must never be optimistic, and there is a test whose
  only job is to assert that.
- **Why:** The conductor refuses cues whose required precision it cannot meet,
  which is the mechanism that stops VLC silently degrading a motion track. That
  mechanism is worthless if precision can under report. `TestPrecisionIsNever
  Optimistic` checks the invariant on every sample of a run rather than at the
  end.
- **Impact:** Precision is deliberately conservative: the anchor's own window,
  plus the residual spread of the rate fit, plus accumulated rate uncertainty,
  floored at the poll interval. Before the first anchor it reports a whole
  frame interval, because a reading with no observed edge really can be that
  stale.

Paused readings do not decay. Precision stays at one frame interval however
long the pause lasts, because nothing is moving and the position on screen is
still exactly what it was.

A discontinuity keeps the rate estimate and drops only the anchors. Rate is a
property of the machine and the player, so it survives a seek; position is not,
so it does not.

### 2026-08-29 · M1 delivered. Latency compensation works.

```
cue at 10s, latency 1.2s, dispatched at 8.8s
```

`TestCueIsDispatchedEarlyByTheInstrumentsLatency` is the milestone, and
everything else in M1 existed to make that assertion possible. Nine conductor
tests and eight clock tests, the whole suite running in about 7 ms with no
player, no socket and no sleeping.

- **Decision:** The conductor is passive, like the clock. The caller drives it
  with `Tick(wall, reading)`.
- **Why:** Same reason as the clock. A scheduler that reads the time itself can
  only be tested by waiting, and a test that waits ten seconds to check a ten
  second cue would never be run often enough to be worth having.
- **Impact:** The polling loop, the ticker and the goroutine all live in the
  caller. Neither `internal/clock` nor `internal/conductor` starts anything.

- **Decision:** Nothing is dropped without a record. Every cue that is not
  dispatched produces a `Skip` carrying the reason.
- **Why:** A rig that silently omits effects is indistinguishable from a rig
  that is broken, and the cause is usually invisible from the room: a clock too
  imprecise for that cue, a seek that stepped over it, an instrument that
  errored. `componium doctor` will read these.
- **Impact:** Four reasons so far: seeked past, clock too imprecise, unknown
  instrument, instrument failed.

Three behaviours worth naming, each of which came out of the measurements
rather than out of imagination:

- **Nothing fires while paused.** A paused player still reports a position, and
  cueing against it would empty a fog machine into a motionless room.
- **A seek resyncs the cursor rather than bursting.** Seeking forward over
  twenty cues records twenty skips and fires none of them; seeking backwards
  re-arms cues so they fire again on the second pass.
- **Cues that cannot be dispatched in time are reported, not fired late.** A
  cue half a second into a film, for an instrument needing 1.2 s of warning, is
  physically impossible. That is a fact about the score the author should see,
  so `Load` returns it via `Unreachable` instead of quietly firing late.

Ramp is declared in the manifest but not yet used. Latency compensation lands
first; aligning the *peak* of a ramped effect needs the score format from M5.

### 2026-08-29 · M2 delivered. The whole chain runs against a real player.

```
CUE  wind.main gust  cue at 3s  sent at 1.803s  1.197s early, precision 6.4ms
```

`componium rehearse` connects to mpv, derives the frame interval from the
content, polls at 200 Hz, and dispatches cues through the conductor to a
virtual instrument. Measured against a live player, not a simulation: cues land
1.197 to 1.199 s early for a declared 1.2 s of latency, and reported precision
settles at 6 to 8 ms.

Running it found three defects that no test had:

1. **Frame rate was unavailable at connect time.** mpv reports no
   `container-fps` until it has loaded a file, so the first query came back
   empty and the CLI fell back to assuming 24 fps. Fixed by retrying for up to
   two seconds.
2. **The rate estimate oscillated by thousands of ppm** while real pacing error
   is tens. The default anchor history of 32 spans only 1.3 s at 24 fps, and a
   short baseline makes a least squares slope jump around. Raised the default
   to 512 anchors, about 21 s, after which the estimate settles to around 120
   ppm and stays there.
3. Status output using carriage returns interleaved illegibly with cue lines.
   Plain lines, which also log properly.

- **Decision:** `internal/show` is the only package that owns a goroutine, a
  ticker, or the passage of time.
- **Why:** The clock and the conductor are both passive so they can be tested
  instantly. Something must still drive them, and confining that to one small
  file means exactly one place is hard to test, rather than the timing logic
  being hard to test everywhere.
- **Impact:** `show.Run` is deliberately short. It reads wall time once per
  iteration and uses the same value for the sample, the reading and the tick,
  so all three agree on when the iteration happened.

## Links

- Branch: `feat-timing-core`
- PR: TBD
- Related ideas: none
- Related features: `feat-composer` depends on the score format, not on this
- External: mpv IPC protocol, `mpv --input-ipc-server`
