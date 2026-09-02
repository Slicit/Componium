---
status: active
branch: main
---

# feat-nodes-carry-several-devices · one board, several instruments

One ESP32 driving one fan is a demonstration, not a rig. A board has thirty odd
usable pins, eight RMT channels and eight LEDC channels, and a room needs more
instruments than it has boards. Recorded as ADR 0007.

The shape, decided before any of it was written:

- A build declares what it *can* drive (PWM, WS28xx, relay). A configuration
  says what is actually plugged in, and lives in the board's own flash.
- At boot the board reads that configuration and knows which device is on which
  pin. `hello` lists them. Attachment is therefore two phases: discovery, which
  needs nobody to have written anything down, and configuration, which is
  remembered.
- CIP multiplexes by index. One socket, several devices, cues addressed to the
  index the board announced.

## Decisions

### 2026-09-02

- **Decision:** A configuration can be written over CIP, and only with the
  secret. The secret is mandatory on any board that accepts one.
- **Why:** The capability is the reason, not the hardware. Somebody who can
  write a configuration can move a relay onto a pin nobody intended, or declare
  a latency of zero and corrupt the timing of every cue after it in a way that
  reads as the score being wrong rather than as an attack. Losing the secret
  means reconnecting USB and reflashing, which is the correct cost.
- **Impact:** A node with no secret takes no configuration at all, which keeps a
  demonstration with no hardware working with no key management.

- **Decision:** An index is only valid for the session that announced it, and
  `Configure` does not return until the board has re-announced.
- **Why:** Reconfiguring moves the indices. Anything holding an old one is
  holding a way to drive the wrong output, and there is nothing in the room to
  show for it: the fan that should not be running is running, the one that
  should be is silent.
- **Impact:** A reboot pushes the configuration back through the same handshake,
  so a board that restarts mid show comes back with the indices everyone
  already has.

- **Decision:** Latency is a field on the configuration, not a constant in the
  firmware.
- **Why:** It was compiled in, so measuring a fan meant editing C and
  reflashing, and nobody ever did. The shipped 1.2 s has been a guess since the
  day it was written.
- **Impact:** `examples/bench-rig.toml` can finally hold measured numbers. It
  still holds guesses.

## Verification

The client half, the studio endpoint and the Boards page, all driven end to end
against a running node rather than against a mock:

- A board is asked what it has, configured with three devices at once (a PWM
  fan, a WS28xx strip and a relay), and asked again on a fresh connection. It
  reports all three, with the latencies it was given.
- Every refusal path, checked through the studio's own API: two devices on one
  pin is a 400 carrying the board's sentence, a wrong secret and a missing
  secret are both a 502, and the missing one says that a board which takes
  configuration requires it. A refused configuration leaves the board holding
  exactly what it had, because it is refused whole.

Five deliberate mutations, each one the bug as it actually was, each caught:
a client counting from one, a refusal thrown away, returning before the board
re-announced, the page keeping its own copy of the kind list, and looking at a
board also configuring it.

The third of those was not caught at first. `Configure` promises the indices are
fresh when it returns, and on loopback a real node acknowledges and re-announces
back to back, so the promise cost nothing and deleting it changed no test. It
was being observed rather than tested. `slownode_test.go` is a board that
acknowledges immediately and announces late, the way one on a switch does while
it writes flash and restarts outputs.

## What is not done

- **No board has run this firmware.** The multi-device firmware compiles; the
  device on the bench still has the single-device build on it.
- **The fan's latency is still a guess.** The field exists now, which was the
  blocker. The measurement is a camera, the sliders, and counting frames.
- **`light.event` shares universe 1 address 1 with `light.ambient`**, so two
  instruments drive the same slots. It wants its own start address or it wants
  to go back to `virtual`.

## Links

- External: `docs/adr/0007-nodes-carry-several-devices.md`, `docs/cip.md` (0.3)
