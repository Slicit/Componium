---
status: shipped
branch: feat-sacn-light
---

# sACN lighting

## Intent

The first instrument that talks to real hardware. Light is the right one to do
first: cheap, safe, and the most visibly wrong when timing is off, so it tests
the whole chain honestly.

## Plan

Speak E1.31 (streaming ACN) rather than inventing anything. Every modern
lighting node, most LED controllers and all of WLED already understand it, so
Componium inherits the entire DMX ecosystem for the cost of one encoder.

## Decisions

### 2026-08-29

- **Decision:** The instrument accepts domain values only. Params are `r`, `g`,
  `b`, `w`, `intensity`, all 0 to 1. Nothing outside `instruments/sacn` ever
  sees a channel number.
- **Why:** ADR 0001. A score written against `light.ambient` must work on
  somebody else's rig where that fixture sits at a different start address, in
  a different mode, on a different universe.
- **Impact:** Start address, mode and universe live in the rig configuration,
  never in a score.

### 2026-08-29

- **Decision:** Out of range values clamp, they do not wrap.
- **Why:** A colour of 1.5 is a mistake. Wrapping turns it into darkness, which
  looks like a different mistake and hides the real one. Clamping makes it look
  saturated, which is closer to the intent and easier to spot.

### 2026-08-29

- **Decision:** Keepalive is the caller's goroutine, exposed as
  `Keepalive(ctx, interval)` rather than started by `New`.
- **Why:** E1.31 receivers commonly fall back to idle after about 2.5 seconds
  without traffic, so a fixture set once and left alone goes dark on its own.
  Something must keep talking. But nothing in Componium should start a
  goroutine the caller did not ask for.

## Verification

Nine tests, exercised over a real UDP socket rather than through a seam
invented for testing: packet round trip, the 638 byte standard length,
rejection of malformed packets, multicast address convention, channel mapping
from a 1 based start address, clamping, sequence numbering, `off`, and
rejection of a fixture that does not fit in a universe.

**Not verified against physical hardware.** No DMX fixture or sACN node was
available. The packet is correct by construction against E1.31-2018 section
4.1 and parses back cleanly, but a real node has not confirmed it, and
`DefaultLatency` of 20 ms is declared rather than measured.

## Links

- Branch: `feat-sacn-light`
- Related features: `feat-timing-core`
- External: ANSI E1.31-2018
