---
status: shipped
branch: feat-spans
---

# Spans · start and stop, not a stream

## Intent

Scores are generated ahead of time, so nothing needs a high refresh rate at
playback. What is actually needed is good sync and, more importantly, a
guarantee that no effect can be left running.

## The problem with streaming

A curve is a stream: the device's state depends on continuously receiving
messages, and "stop" is the *absence* of traffic. That is exactly backwards for
anything that can hurt someone. A fan holds its last value, and the only thing
that ends it is a 300 ms watchdog noticing silence.

A span inverts it. The device is told "run at 0.8 for 4 seconds" and ends the
effect itself. Stopping becomes something that happens by default rather than
something that requires a message to arrive.

It is also far less traffic: a four second fog burst is two messages instead of
two hundred.

## Decisions

### 2026-08-29

- **Decision:** A cue with a `Hold` becomes two schedule entries, a start and a
  stop, and the duration is *also* sent to the device as `hold_ms`.
- **Why:** Belt and braces, and both halves earn their place. The stop is a UDP
  datagram and can be lost. The conductor is a process and can crash. An
  instrument that only stops when told is one dropped packet away from running
  until somebody pulls a plug.
- **Impact:** Three independent layers end an effect: the node's own timer,
  which needs nothing from the network; the conductor's stop, retried like any
  cue; and the heartbeat watchdog. Only the first survives every failure.

### 2026-08-29

- **Decision:** A stop is never refused for imprecision.
- **Why:** Declining to *end* an effect because the clock is vague would leave
  hardware running, which is strictly worse than ending it slightly early or
  late. The precision gate protects the feel of an effect; it must not protect
  it into a hazard.

### 2026-08-29

- **Decision:** Pausing stops every running span.
- **Why:** A viewer who pauses for twenty minutes should not come back to a
  room full of smoke. Curves still hold when paused, because holding a light at
  its current colour is correct; a fog burst continuing is not.

### 2026-08-29

- **Decision:** A seek that steps over a stop fires it anyway, rather than
  recording it as skipped.
- **Why:** Found by thinking about what a seek does mid-span. Skipping the
  start *and* the stop is harmless. Skipping only the stop, because the start
  already fired, leaves the instrument running with nothing left in the
  schedule to end it. Seeking backwards out of a span has the same problem from
  the other direction, and is handled the same way.

### 2026-08-29

- **Decision:** An instrument declaring `MaxContinuous` refuses to load a cue
  with no duration, or one longer than its limit.
- **Why:** This is the difference between a score that is merely wrong and a
  score that empties a fog machine. It fails at load, before a film starts,
  rather than halfway through.

## What this fixed

The composer already wrote `duration = "4.0s"` on every subtitle cue, the
parser already read it, and `Score.Cues()` silently dropped it. The score said
when to stop and nothing acted on it. That had been true since M10 and no test
caught it, because no test asked what happened after a cue.

## Verification

Ten conductor tests covering start and stop pairing, latency compensation of
the stop, pause, seeking forward and backward out of a span, refusal of
unbounded cues on limited instruments, and that momentary cues get no stop.

Three CIP tests, including the one that matters: a node ends a span on its own
with heartbeats still flowing, so the watchdog demonstrably is not what stopped
it.

Firmware updated to honour `hold_ms`, still uncompiled.

## Links

- Branch: `feat-spans`
- Related features: `feat-safety`, `feat-esp32-node`, `feat-score-format`
