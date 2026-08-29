---
status: shipped
branch: feat-safety
---

# Safety supervisor

## Intent

Componium drives water, heaters, high power fans and eventually moving mass. It
ships before any of those exist so that none of them are ever driven by a
system without it.

## The ordering principle

From ADR 0001: an instrument must be safe when the conductor is absent,
malicious, or wrong. Three mechanisms, in decreasing order of how far they can
be trusted:

1. The instrument's own limits, in the device or its firmware. Nothing in this
   package can override those and nothing should try.
2. The watchdog. If heartbeats stop, everything is driven to its declared safe
   state.
3. Limits enforced here, which are a second line and a convenience, not the
   real defence.

## Decisions

### 2026-08-29

- **Decision:** The watchdog runs on its own goroutine, never inside the show
  loop.
- **Why:** A watchdog fed and checked by the same loop cannot detect that loop
  stopping, which is the single failure it exists to catch. If the conductor
  wedges with a fogger open, nothing else will notice.
- **Impact:** `safety.Watch(ctx, sup, interval)` is started explicitly by the
  caller. The show loop only calls `Heartbeat`.

### 2026-08-29

- **Decision:** 300 ms without a heartbeat trips it.
- **Why:** Long enough to survive a slow tick or a garbage collection pause,
  short enough that a wedged conductor cannot empty a fog machine.

### 2026-08-29

- **Decision:** All-stop latches. Nothing is dispatched again until an explicit
  reset.
- **Why:** A rig that resumes on its own after a fault is worse than one that
  stays down, because the person who stopped it is probably standing next to it.

### 2026-08-29

- **Decision:** A clean exit also drives everything safe.
- **Why:** Ending a film should not leave a fan running or a valve open, and
  the tidy path is the one people take most often.

### 2026-08-29

- **Decision:** `isActive` is a crude heuristic: any non-zero parameter counts
  as active.
- **Why:** The supervisor cannot know what a given instrument's parameters
  mean. Guessing wrongly errs toward treating things as active, which errs
  toward stopping, which is the right direction to be wrong in. An instrument
  that knows better enforces its own limits, which is where the real defence
  belongs.

## Verification

Eight tests, all deterministic because the supervisor is told the time rather
than reading it. The one that matters most drives a fogger, stops the
heartbeats, and asserts that the last thing the instrument received was its
safe state.

**Not verified against hardware.** No fogger, valve or platform exists to test
against. The duty cycle and continuous run limits are enforced in Go, and their
firmware equivalents on a real node do not exist yet.

## Links

- Branch: `feat-safety`
- Related features: `feat-timing-core`
