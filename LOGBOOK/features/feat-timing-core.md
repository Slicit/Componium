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
3. Clock filter: maintain a local monotonic estimate between polls, resync on
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

## Links

- Branch: `feat-timing-core`
- PR: TBD
- Related ideas: none
- Related features: `feat-composer` depends on the score format, not on this
- External: mpv IPC protocol, `mpv --input-ipc-server`
