# Componium Instrument Protocol (CIP) — 0.3

> **Licence:** this specification is dedicated to the public domain under
> CC0 1.0 ([LICENSE-spec.txt](LICENSE-spec.txt)), deliberately separate from
> the AGPL-3.0 covering Componium itself. Anyone may implement a CIP
> instrument, in any language, under any licence, without the project's
> copyleft reaching their implementation.

CIP is how the conductor talks to instruments that are not in its process. It
is deliberately small, so that an ESP32 running C, a Python script and a Go
binary are all first-class instruments.

0.3 is a node carrying several devices rather than being one device. What
changed and why is in [ADR 0007](adr/0007-nodes-carry-several-devices.md): `hello`
is a list, cues name an instrument, curve frames are bundled and carry an
index, a node can be configured over the wire, and authentication stops being
optional for any node that can be.

Implemented in `internal/cip`. A software node lives behind `componium node`,
and ESP32 firmware in `firmware/esp32`.

## Transport: UDP, all of it

Version 0.1 of this document specified WebSocket for control. That was written
before anyone considered the device at the other end. See
[ADR 0005](adr/0005-cip-over-udp.md).

| Traffic | Reliability | Rate |
|---|---|---|
| Control: hello, cue, safe | acknowledged, retried 3 times at 40 ms | rare |
| Curve frames | fire and forget | 50 Hz |
| Heartbeat | unacknowledged | 10 Hz |

Three kinds of traffic, three treatments. A cue is rare and must arrive, so it
is acknowledged. A curve frame is superseded 20 ms later, so retransmitting one
is worse than useless. A heartbeat's *absence* is the message.

Default port is UDP 5570.

## Registration

The node introduces itself. The conductor may also send an empty `hello` to
prompt one, so a node that booted first need not keep shouting.

A node carries several devices, so `hello` is a list. What is attached to which
pin comes from the node's own configuration; see ADR 0007.

```json
{
  "v": "0.3",
  "t": "hello",
  "node": { "name": "cinema-left", "firmware": "0.3.0", "chip": "ESP32" },
  "instruments": [
    {
      "index": 0,
      "id": "wind.main",
      "kind": "wind",
      "latency_ms": 1200,
      "ramp_up_ms": 1800,
      "ramp_down_ms": 3000,
      "max_continuous_ms": 120000,
      "duty_cycle": 0.6,
      "safe_state": { "intensity": 0 },
      "channels": [
        { "name": "intensity", "unit": "normalised", "range": [0, 1] }
      ]
    },
    {
      "index": 1,
      "id": "light.strip",
      "kind": "light",
      "latency_ms": 20,
      "safe_state": { "r": 0, "g": 0, "b": 0 },
      "channels": [
        { "name": "r", "unit": "normalised", "range": [0, 1] },
        { "name": "g", "unit": "normalised", "range": [0, 1] },
        { "name": "b", "unit": "normalised", "range": [0, 1] }
      ]
    }
  ]
}
```

A node with nothing configured announces an empty list. That is an ordinary
state, not an error: it is what every freshly flashed board is in.

**The manifest comes from the node, never from the conductor's configuration.**
The device is the only thing that actually knows its own latency, and a rig
file that disagrees with the hardware is worse than no rig file.

**An index is valid only for the session that announced it.** Configuration is
editable, so index 2 can be a different device after a reboot. A node that
restarts says `hello` again and every index is re-read; a conductor that has not
seen a `hello` has no indices and sends no curve frames.

`latency_ms` is the contract that makes the system work: the conductor
dispatches every cue for this instrument that many milliseconds early. An
instrument that lies here is the easiest way to make a rig feel wrong in a way
nobody can diagnose from the room.

`ramp_*` is distinct from latency. Latency is dead time before anything
happens; ramp is how long the effect takes to reach the commanded value once it
starts.

## Cues

```json
{ "v": "0.3", "t": "cue", "seq": 17, "i": "wind.main", "action": "gust",
  "params": { "intensity": 0.8 } }
```

`i` names the instrument on the node. A name rather than an index because cues
are rare, are read by people in logs, and survive a node reconfiguring itself
between the conductor deciding to send one and the node receiving it.

The node replies with an `ack` carrying the same `seq`. Unacknowledged cues are
retried; a cue that cannot be delivered becomes an error the conductor records
as a skip, because a silently lost cue is invisible.

## Spans: every effect carries its own ending

A cue may declare `hold_ms`. A node that receives one **must end the effect
itself when it expires**, without waiting to be told.

```json
{ "v": "0.3", "t": "cue", "seq": 17, "i": "fog.left", "action": "burst",
  "params": { "output": 0.8 }, "hold_ms": 4000 }
```

The conductor will *also* send a stop when the span ends. Both, deliberately.
The stop is a UDP datagram and can be lost; the conductor is a process and can
crash. An instrument that only stops when told is one dropped packet away from
running until somebody pulls a plug.

That gives three independent layers, in decreasing order of how much they can
be trusted:

1. **The node's own hold timer.** Needs nothing from the network.
2. **The conductor's stop**, which is retried like any other cue.
3. **The heartbeat watchdog**, which catches the case where the conductor
   stopped talking entirely.

Only the first survives every failure, which is why it exists even though the
other two would usually be enough.

An action of `stop`, `off`, `safe` or `neutral` ends an effect. A node that
does not recognise an action should do nothing, with that one exception:
failing to understand a stop must never mean carrying on.

## Curve frames

Binary, because at 50 Hz per instrument the JSON parser is the expensive part
on a microcontroller. One frame carries every output on the node that is due
this tick.

```
byte 0   'C'
byte 1   'F'
byte 2   frame version, currently 1
byte 3   output count
then, that many times:
  byte 0   instrument index, as announced in hello
  byte 1   channel count
  byte 2+  that many big endian float32 values
```

Channel meaning comes from the manifest, by position. float32 gives seven
significant digits, far more than any physical output resolves.

**Bundling is not an optimisation.** Four datagrams for four outputs that must
move together arrive at four different times, and on wifi occasionally at very
different ones. One datagram arrives once and the node applies every output in
it before returning, so outputs on one board move within microseconds of each
other rather than milliseconds. It is the only way to make simultaneous
actually simultaneous over a transport that reorders and drops.

An index the node does not recognise is skipped, and the rest of the frame is
applied. A frame is fifty times a second and superseded 20ms later; refusing all
of it because one output has gone would be the wrong trade.

Frame version 1 rather than 0, so a 0.2 node refuses a bundled frame rather
than reading the output count as a channel count and driving an output with
somebody else's index.

## Safety

- The conductor sends a heartbeat at 10 Hz.
- **A node that receives no heartbeat for 300 ms enters its safe state on its
  own authority** and stays there until traffic resumes. This does not depend
  on the network being healthy or the conductor being correct.
- A `safe` message orders an immediate return to the safe state, bypassing
  everything.
- Limits are enforced **by the node**, not the conductor. A fogger refuses to
  exceed its own duty cycle even when the score demands it.

The ordering principle: an instrument must be safe when the conductor is
absent, malicious, or wrong.

### With several devices on one node

The rules are the same and the scope is larger, which is where the care goes.

**One watchdog, every output.** Heartbeats are per node, not per instrument.
When they stop, every output goes to its safe state, not the one that happened
to be addressed most recently.

**A hold that expires takes one output safe, not the node.** A four second fog
burst ending must not stop a fan in the middle of a scene.

**Safe before configured.** Between boot and a configuration being applied,
every pin sits at its safe value. A configuration that fails to parse leaves the
node with no instruments rather than with some of them.

**No configuration is an ordinary state.** A freshly flashed board announces an
empty instrument list. It does not refuse to boot; that is the state every board
is in when it arrives.

## Configuration

A node is told what is attached to it, and remembers.

```json
{
  "v": "0.3", "t": "configure", "seq": 31, "n": 412,
  "devices": [
    { "id": "wind.main", "type": "pwm", "gpio": 18, "freq_hz": 25000,
      "kind": "wind", "latency_ms": 1200, "ramp_up_ms": 1800,
      "ramp_down_ms": 3000, "safe": 0 },
    { "id": "light.strip", "type": "ws28xx", "gpio": 5, "pixels": 30,
      "order": "grb", "kind": "light", "latency_ms": 20 },
    { "id": "fog.left", "type": "relay", "gpio": 21, "active": "high",
      "kind": "fog", "latency_ms": 2000, "safe": 0 }
  ]
}
```

The node validates the list, writes it to its own storage, applies it, and
replies with an `ack`. It then sends a fresh `hello`, because the instruments
and their indices have just changed and anything holding the old ones is now
wrong.

A configuration that does not parse, names a type the firmware does not have,
or claims a pin another device in the same list already claimed, is refused
whole. Half a configuration is worse than none: it is a node that looks
configured and is not.

Three types today. A type is what a firmware build contains; a device is what a
configuration says is attached.

| type | for | carries |
|---|---|---|
| `pwm` | fans, dimmable lights, misters | `gpio`, `freq_hz` |
| `ws28xx` | addressable strips | `gpio`, `pixels`, `order` |
| `relay` | foggers, valves, anything switched | `gpio`, `active` |

Every device also carries the physical facts about the thing attached: `kind`,
`latency_ms`, `ramp_up_ms`, `ramp_down_ms`, `safe`. A type ships defaults,
because a fan of a known model does have a known spin up. All of them are
overridable, and that is the point of this message existing: **the number the
whole timing model rests on becomes something a person who has measured it can
set, rather than something they would have to recompile to change.**

## Authentication

**Required on any node that accepts configuration.** A shared secret, and no
way to run without one.

Under 0.2 this was optional, and reasonably so: the worst a stranger on the
network could do was start a fogger. A node that takes configuration is a
different proposition. A stranger can move a relay onto a pin nobody intended,
or declare a latency of zero and corrupt the timing of every cue after it in a
way that reads as the score being wrong rather than as an attack.

So the requirement follows from the capability. A node that accepts `configure`
**ignores every unauthenticated datagram, including `hello`**. A node that does
not accept configuration, such as a virtual one used for development, does not
need a secret, which is what keeps a demonstration with no hardware working
without key management.

The secret is written over USB, with the wifi credentials, by whoever is holding
the board. **There is no recovery path over the network, deliberately.** Losing
the secret means reconnecting USB and reflashing. A remote way back in is a way
in, and the entire security model of this device is that it ignores anyone who
cannot prove they know the key.

Every datagram carries a 16 byte HMAC-SHA256 prefix computed over the rest of
the datagram:

```
byte 0..15   HMAC-SHA256(secret, body)[0:16]
byte 16..    body: JSON control message, or a curve frame
```

The tag is a prefix on the raw bytes rather than a field inside the JSON, so
verifying it is a hash and a comparison rather than a parse. That matters
because the other implementation is C on a microcontroller, where
canonicalising a document to check a signature would be slow and easy to get
subtly wrong.

**This is authentication, not encryption.** Anyone on the network can still see
what the rig is doing. They cannot make it do anything. That is the right trade
here: the content is a fan speed, and the risk is a stranger starting a fog
machine.

Control messages carry a monotonic counter `n`, and a node rejects any counter
it has already seen. Without it, an attacker who cannot forge a tag can still
record a genuine "gust at full intensity" and replay it whenever they like.

Curve frames are authenticated but carry no counter. A replayed frame is
superseded by the next genuine one 20 ms later, and counting every frame would
mean dropping frames that arrived out of order, which UDP does routinely.

A node configured with a secret **ignores unauthenticated traffic entirely**
rather than refusing it politely. Replying at all would confirm the node exists
and is worth attacking.

Both ends must agree. In a rig file:

```toml
[[instrument]]
id = "fog.left"
driver = "cip"
addr = "192.168.1.52:5570"
secret = "something long and not guessable"
```

Leaving it out is possible only against a node that cannot be configured.

## Versioning

A message whose `v` differs from the receiver's version is refused rather than
half understood. A message from a protocol you do not speak could mean
anything, including something dangerous.

## Open questions

- Key distribution. The secret is configured by hand at both ends, which is
  fine for one household and would not scale to anything larger.
- Discovery. A node still has to be found by address. Announcing itself on the
  network is a separate decision with its own security shape, and configuration
  is useful without it.
- Discovery: mDNS, or static addresses in the rig file? Currently static.
- Per-instrument curve rates. A light is happy at 50 Hz; a motion platform may
  want more and a mister far less.
- Whether nodes need a synchronised clock of their own, or whether acting on
  arrival is sufficient given latency compensation. Probably sufficient on a
  wired LAN, probably not over Wi-Fi.
