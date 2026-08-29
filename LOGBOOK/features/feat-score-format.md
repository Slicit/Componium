---
status: shipped
branch: feat-score-format
---

# Score format v1 and the rig file

## Intent

A score says what should happen. A rig says what is in the room. Keeping them
apart is the whole reason a score can be shared: the score names
`light.ambient`, and somebody else's rig knows that on their installation that
is an RGB fixture at DMX address 10 on universe 1.

## Decisions

### 2026-08-29

- **Decision:** Two kinds of track, cue and curve, and they behave differently
  under latency compensation.
- **Why:** A cue is an event, so latency is compensated by dispatching
  *earlier*. A curve is a value, and sending a value early just sends the wrong
  value. So the curve driver compensates by sampling the curve *ahead* of the
  current media time by the instrument's latency instead.
- **Impact:** `CurveDriver` samples at `media + latency`. This is subtle enough
  that it has its own test.

### 2026-08-29

- **Decision:** Curves hold at their endpoints rather than extrapolating.
- **Why:** Extrapolating a wind speed past the end of its authored range
  invents an effect the author never wrote, and the failure is silent.

### 2026-08-29

- **Decision:** Tracks are sorted on load rather than trusted to be in order.
- **Why:** A hand edited score with a cue inserted in the wrong place is the
  common case, and out of order playback is a miserable thing to debug.

### 2026-08-29

- **Decision:** An unknown rig driver is an error, never a silent fallback to
  virtual.
- **Why:** A rig that quietly pretends to drive hardware is worse than one that
  refuses to start. The same applies to a score naming an instrument the rig
  does not have: `play` refuses before the film begins rather than discovering
  it halfway through.

### 2026-08-29

- **Decision:** Timecodes accept `01:04:22.100`, `04:22.5`, `22`, and Go
  durations like `1h4m22s`.
- **Why:** Scores are hand edited by people who think in timecode. The format
  should accept what they will actually type.

## Verification

Twenty tests across score, curve and rig. Demonstrated end to end against a
live mpv with `examples/demo.componium`:

```
CUE  wind.main  gust  cue at 10s  sent at 8.802s  1.198s early
curve updates 649 over 14s, about 47 per second at a 20ms interval
```

## Links

- Branch: `feat-score-format`
- Related features: `feat-timing-core`, `feat-composer` consumes this format
