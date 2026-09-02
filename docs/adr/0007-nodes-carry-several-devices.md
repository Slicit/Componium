# ADR 0007 — A node carries several devices, and knows what they are

Status: accepted · 2026-09-02

## Context

A CIP node is one instrument. The id, the kind, the latency, the ramps and the
GPIO are all compiled in, so a board is a fan or it is a strip, and being both
requires two boards. Point two rig entries at one address and they come back as
the same instrument, because the manifest comes from the device and the device
has exactly one.

Two things follow, and the second is worse than the first.

An ESP32 that could comfortably drive three or four outputs drives one. That is
waste, and it is the obvious thing to notice.

The number that the entire timing model rests on cannot be measured. The
conductor dispatches every cue `latency_ms` early, and ADR 0001 makes the device
the authority on that number because a rig file that disagrees with the hardware
is worse than no rig file. But `latency_ms` is a `#define`. Measuring a fan's
real dead time means editing C, rebuilding, and reflashing, so nobody does, and
the shipped guess of 1200ms stays a guess for the life of the rig.

## Decision

**A node holds a configuration in its own storage that says which devices are
attached to which pins. It reads that at boot, announces the resulting
instruments in `hello`, and accepts a new configuration over CIP.**

Three device types to begin with, which is what an ESP32 usefully drives:

| type | for | carries |
|---|---|---|
| `pwm` | fans, dimmable lights, misters | gpio, frequency, resolution |
| `ws28xx` | addressable strips | gpio, pixel count, colour order |
| `relay` | foggers, valves, anything switched | gpio, active level |

Every device also carries the facts about the physical thing attached to it:
`kind`, `latency_ms`, `ramp_up_ms`, `ramp_down_ms`, `safe_state`. A device type
ships sensible defaults, because a bought fan of a known model does have a
known spin up, and every one of them is overridable, because the point is that
somebody who has measured theirs can say so.

This is the decision that matters. **It moves the honesty requirement from a
build step to a field.** "Measure your fan and put the real number in" stops
being advice nobody can act on.

## The protocol, at 0.3

`hello` carries a list rather than a manifest. Each entry has a stable `id` and
an `index`.

Cues name their instrument, because they are JSON and rare and a name is what a
person reads in a log.

Curve frames carry the index, because they are binary and arrive fifty times a
second per output and a string comparison per frame is precisely the cost the
binary format exists to avoid. One frame carries every output due this tick.

**An index is only valid for the session that announced it.** Configuration is
editable, so index 2 can become a different device between reboots, and a
conductor holding a stale index would drive the wrong output with nothing to
show for it in the room. A node that reboots says `hello` again and every index
is re-read; a conductor that has not seen a `hello` has no indices at all.

Bundling is not an optimisation. Four datagrams for four outputs that must fire
together arrive at four different times, and on wifi occasionally at very
different times. One datagram arrives once and the node applies every output in
it before returning, so the skew between outputs on one board is microseconds
rather than milliseconds. It is the only way to make simultaneous actually
simultaneous over a lossy transport.

## Authentication stops being optional

A node that accepts configuration requires a secret. Not a recommendation.

The reasoning is a change in what an attacker gets. Under 0.2 the worst a
stranger on the network could do was start a fogger, which is why the shared
secret was reasonable to leave off on a wired network you control. A node that
takes configuration is a different proposition: a stranger can move a relay onto
a pin you never intended, or declare a latency of zero and corrupt the timing of
every cue after it in a way that looks like the score being wrong.

So the requirement follows from the capability rather than from the hardware:
**any node that accepts `configure` refuses every unauthenticated datagram,
including `hello`.** The ESP32 firmware always accepts configuration, so it
always requires a secret. A virtual node that does not accept configuration does
not need one, which is what keeps a demonstration with no hardware working with
no key management.

The secret is written over USB, with the wifi credentials, by the person holding
the board. **Losing it means reconnecting USB and reflashing**, and that is
accepted rather than worked around: a recovery path over the network is a
recovery path an attacker can use, and this is a device whose entire security
model is that it ignores anyone who cannot prove they know the key.

## Safety

Unchanged in intent and larger in scope, which is where the care goes.

- **One watchdog, every output.** Heartbeats are per node, not per instrument.
  When they stop, every output goes to its safe state, not the one that was
  most recently addressed.
- **A hold that expires takes one output safe**, not the node. A four second
  fog burst ending must not stop a fan mid-scene.
- **Safe before configured.** Between boot and the configuration being applied,
  every pin is at its safe value. A configuration that fails to parse leaves the
  node with no instruments rather than with half of them.
- **No configuration is an ordinary state.** A freshly flashed board announces
  zero instruments and says so. It does not refuse to boot, because that is the
  state every board is in when it arrives.

## Consequences

- `Built.Collisions()` inverts. It was added to explain that one node is one
  instrument and to advise a second board; it becomes the check that a rig only
  asks for instruments the node actually declared, which is a more useful error
  and a shorter one.
- One `cip.Client` per address, with instruments as views onto it. Exactly the
  shape the sACN package took when a `Light` stopped owning a universe, and for
  the same reason: the transport is shared, the instruments are not.
- One heartbeat per board rather than per instrument.
- The firmware stops hardcoding a role. The same image is correct for every
  contributor, which is what makes the browser flasher worth having: today it
  hands you a board convinced it is a fan on GPIO 18 whether you own a fan or
  not.
- A rig entry gains nothing. It still names an instrument and an address; the
  node is what decides whether that instrument exists.

## Not doing yet

**A firmware builder that selects which device types are compiled in.** There
are three types and the image has room for all of them, so a build time
selector would be a thing to maintain before there is anything worth selecting.
It earns its place when there are ten types and flash is tight.

**Discovery.** A node still has to be found by address. Announcing itself on the
network is a separate decision with its own security shape, and configuration is
useful without it.
