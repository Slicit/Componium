# Componium Instrument Protocol (CIP) — draft 0.1

> Draft. Nothing implements this yet; it exists to be argued with.

CIP is how the conductor talks to instruments. It is deliberately
language-agnostic so that an ESP32 running C, a Python script and a Go binary
are all first-class instruments.

## Transport

| Traffic | Transport | Rate |
|---|---|---|
| Registration, cues, control | WebSocket, JSON | event-driven |
| Curve frames | UDP, compact binary | 50 Hz |
| Heartbeat | UDP | 10 Hz |

Cues are reliable and infrequent. Curve frames are high-rate and disposable —
a dropped frame is superseded 20 ms later, so retransmission is worse than
useless. Heartbeat is separate from both so that a stalled cue path cannot
mask a dead conductor.

## Registration

An instrument announces itself with a manifest:

```json
{
  "id": "wind.main",
  "kind": "wind",
  "channels": [{ "name": "intensity", "unit": "normalised", "range": [0, 1] }],
  "latency_ms": 1200,
  "ramp": { "up_ms": 1800, "down_ms": 3000 },
  "limits": { "max_continuous_s": 120, "duty_cycle": 0.6 },
  "safe_state": { "intensity": 0 }
}
```

`latency_ms` is the contract that makes the whole system work: the conductor
dispatches every cue for this instrument that many milliseconds early. An
instrument that lies about its latency is the single easiest way to make a
rig feel wrong.

`ramp` is distinct from latency. Latency is dead time before anything happens;
ramp is how long the effect takes to reach the commanded value once it starts.
The scheduler needs both to decide when a gust must begin in order to peak on
the right frame.

## Safety

- The conductor emits a heartbeat at 10 Hz.
- An instrument that receives no heartbeat for **300 ms** enters `safe_state`
  on its own authority and stays there until a heartbeat resumes.
- `limits` are enforced **by the instrument**, not the conductor. A fogger
  refuses to exceed its own duty cycle even if the score demands it.
- All-stop is a distinct message that bypasses the scheduler entirely.

The ordering principle: an instrument must be safe when the conductor is
absent, malicious or wrong.

## Open questions

- Discovery: mDNS, or static configuration in `rig.toml`? mDNS is friendlier
  and one more failure mode.
- Do curve frames need per-instrument rates? A light is happy at 50 Hz; a
  motion platform may want 100 Hz+ and a mister 5 Hz.
- Clock distribution: do instruments need their own synchronised clock, or is
  "act on arrival" sufficient given latency compensation? Probably sufficient
  on a wired LAN, probably not over Wi-Fi.
- Authentication. Currently none. A LAN-only protocol that can start a fogger
  deserves at least a shared secret.
