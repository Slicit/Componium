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
   `Now() (mediaTime, wallClock, rate, confidence)`.
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

## Links

- Branch: `feat-timing-core`
- PR: TBD
- Related ideas: none
- Related features: `feat-composer` depends on the score format, not on this
- External: mpv IPC protocol, `mpv --input-ipc-server`
