---
status: active
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

## Links

- Branch: `feat-timing-core`
- PR: TBD
- Related ideas: none
- Related features: `feat-composer` depends on the score format, not on this
- External: mpv IPC protocol, `mpv --input-ipc-server`
