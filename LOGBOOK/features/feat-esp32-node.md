---
status: shipped
branch: feat-esp32-node
---

# CIP and the ESP32 node

## Intent

Most custom effects are a microcontroller and a driver board. Componium needs a
way to reach them that is fast enough to land a cue on a frame and simple
enough that somebody who is not primarily a programmer can implement it.

## Decisions

### 2026-08-29

- **Decision:** All of CIP is UDP, including cues. This supersedes the
  WebSocket transport in the original spec. Recorded as ADR 0005.
- **Why:** A websocket needs a TCP stack, HTTP upgrade and frame masking on a
  device where memory is scarce. Worse, TCP would let a 50 Hz curve stream
  delay a cue behind it, which is exactly the wrong tradeoff: the rare
  important message waiting on the frequent unimportant one. Reliability was
  the only argument for TCP, and it turned out to be twenty lines of sequence
  numbers and retries.
- **Impact:** Three kinds of traffic in one socket, distinguished by what they
  need rather than by what is convenient.

### 2026-08-29

- **Decision:** The manifest comes from the node, not from the rig file.
- **Why:** The device is the only thing that actually knows its own latency. A
  rig file that disagrees with the hardware is worse than no rig file, because
  it looks authoritative.
- **Impact:** A `cip` instrument in a rig file states only an address.

### 2026-08-29

- **Decision:** `componium node` implements the whole protocol in Go.
- **Why:** It makes CIP testable end to end with no hardware, and it lets
  somebody with no microcontroller run a complete distributed rig. Same
  reasoning as virtual instruments.
- **Impact:** The firmware and the Go node implement the same spec, so the spec
  is exercised even though the firmware is not.

## Verification

Ten tests, including the one the protocol exists for: a node that stops
receiving heartbeats drives itself safe without being asked. Demonstrated end
to end with `componium play` against a live node over UDP, cue delivered
1.197 s early and acknowledged.

**The firmware is written and not compiled.** No ESP32 and no ESP-IDF toolchain
were available. The protocol it implements is verified through the Go node; the
C is a careful draft that has never run on a device.

**There is no authentication.** A LAN protocol that can start a fog machine
deserves at least a shared secret. Recorded as an open question rather than
quietly ignored.

## Links

- Branch: `feat-esp32-node`
- External: `docs/adr/0002-esp32-node.md`, `docs/adr/0005-cip-over-udp.md`
