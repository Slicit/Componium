# ADR 0005 — CIP is UDP, including for cues

Status: accepted · 2026-08-29
Supersedes the transport section of the original `docs/cip.md` draft.

## Context

The first protocol draft specified WebSocket with JSON for control traffic and
UDP for curve frames. That was written before anyone had thought about what it
would mean on the device at the other end.

## Decision

Everything is UDP, including cues.

## Why

**A websocket is expensive on an ESP32.** It needs a TCP stack, HTTP upgrade
handling and frame masking, on a device where memory and certainty are both
scarce. The whole point of ADR 0002 was that an ESP32 node should be easy to
write, including by somebody who is not primarily a programmer.

**TCP would let a curve stream delay a cue.** Head of line blocking is exactly
wrong here. Curve frames are frequent and disposable; a cue is rare and must
arrive on time. Putting them on one ordered stream means the important, rare
message waits behind the unimportant, frequent one.

**Reliability was the only argument for TCP, and it is about twenty lines.**
Cues carry a sequence number and are acknowledged; the client retries three
times at 40ms. That is 120ms worst case, well inside the latency of any
instrument slow enough to be reached over a network in the first place.

## Consequences

- Three kinds of traffic with three different treatments: control is
  acknowledged and retried, curve frames are fire and forget because a dropped
  frame is superseded 20ms later, heartbeats are unacknowledged because their
  absence is the message.
- An undeliverable cue becomes an error the conductor records as a skip. This
  matters: a lost cue is otherwise invisible, the effect simply never happens
  and nothing in the room explains why.
- Curve frames carry float32. Seven significant digits is far more than any
  physical output resolves, and halving the frame size at 50Hz per instrument
  is worth more than digits nothing can act on.
- Still unaddressed: there is no authentication of any kind. A LAN protocol
  that can start a fog machine deserves at least a shared secret, and that is
  a gap rather than a decision.
