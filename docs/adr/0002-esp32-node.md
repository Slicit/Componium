# ADR 0002 — A Componium ESP32 node, not ESPHome

Status: proposed · 2026-08-29

## Context

Most custom effects — fans, relays, misters, scent, small actuators — are an
ESP32 and a driver board. ESPHome already solves declarative configuration,
OTA updates and device discovery beautifully, and is the obvious thing to
reuse.

## Decision

Adopt ESPHome's *ergonomics* — declarative YAML describing pins and devices,
OTA, discovery — but **not its control path**.

ESPHome's runtime talks to Home Assistant over its native API or MQTT. That
path is optimised for home automation, where 100–300 ms and non-deterministic
jitter are fine. Componium needs a cue to land on a frame. So the node speaks
CIP natively over UDP, with the heartbeat and autonomous `safe_state`
behaviour from [cip.md](../cip.md) implemented in firmware.

```yaml
# componium-node.yaml — illustrative
node: wind.main
instruments:
  - kind: wind
    output: { pin: GPIO18, type: pwm, frequency: 25000 }
    latency_ms: 1200
    ramp: { up_ms: 1800, down_ms: 3000 }
    safe_state: { intensity: 0 }
```

## Consequences

- The safety watchdog lives in firmware and works even if the LAN drops.
- We own a firmware build and OTA story, which is real ongoing cost.
- A bridge *to* Home Assistant remains possible later, as an instrument
  adapter, for effects where 300 ms genuinely does not matter (room lights
  coming up at the end of a film).
