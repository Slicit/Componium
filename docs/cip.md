# Componium Instrument Protocol (CIP) — 0.2

> **Licence:** this specification is dedicated to the public domain under
> CC0 1.0 ([LICENSE-spec.txt](LICENSE-spec.txt)), deliberately separate from
> the AGPL-3.0 covering Componium itself. Anyone may implement a CIP
> instrument, in any language, under any licence, without the project's
> copyleft reaching their implementation.

CIP is how the conductor talks to instruments that are not in its process. It
is deliberately small, so that an ESP32 running C, a Python script and a Go
binary are all first-class instruments.

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

```json
{
  "v": "0.2",
  "t": "hello",
  "manifest": {
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
  }
}
```

**The manifest comes from the node, never from the conductor's configuration.**
The device is the only thing that actually knows its own latency, and a rig
file that disagrees with the hardware is worse than no rig file.

`latency_ms` is the contract that makes the system work: the conductor
dispatches every cue for this instrument that many milliseconds early. An
instrument that lies here is the easiest way to make a rig feel wrong in a way
nobody can diagnose from the room.

`ramp_*` is distinct from latency. Latency is dead time before anything
happens; ramp is how long the effect takes to reach the commanded value once it
starts.

## Cues

```json
{ "v": "0.2", "t": "cue", "seq": 17, "action": "gust",
  "params": { "intensity": 0.8 } }
```

The node replies with an `ack` carrying the same `seq`. Unacknowledged cues are
retried; a cue that cannot be delivered becomes an error the conductor records
as a skip, because a silently lost cue is invisible.

## Spans: every effect carries its own ending

A cue may declare `hold_ms`. A node that receives one **must end the effect
itself when it expires**, without waiting to be told.

```json
{ "v": "0.2", "t": "cue", "seq": 17, "action": "burst",
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
on a microcontroller.

```
byte 0   'C'
byte 1   'F'
byte 2   frame version, currently 0
byte 3   channel count
byte 4+  that many big endian float32 values
```

Channel meaning comes from the manifest, by position. float32 gives seven
significant digits, far more than any physical output resolves.

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

## Authentication

Optional, off by default, and a shared secret when on.

When a secret is configured, **every datagram carries a 16 byte HMAC-SHA256
prefix** computed over the rest of the datagram:

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

Leaving it out is reasonable on a wired network you control, and unwise on
anything shared or wireless.

## Versioning

A message whose `v` differs from the receiver's version is refused rather than
half understood. A message from a protocol you do not speak could mean
anything, including something dangerous.

## Open questions

- Key distribution. The secret is configured by hand at both ends, which is
  fine for one household and would not scale to anything larger.
- Discovery: mDNS, or static addresses in the rig file? Currently static.
- Per-instrument curve rates. A light is happy at 50 Hz; a motion platform may
  want more and a mister far less.
- Whether nodes need a synchronised clock of their own, or whether acting on
  arrival is sufficient given latency compensation. Probably sufficient on a
  wired LAN, probably not over Wi-Fi.
