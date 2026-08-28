# ADR 0003 — Bridge to existing motion rigs

Status: proposed · 2026-08-29

## Context

Motion is the most expensive, most dangerous and least standardised effect.
The DIY sim-racing community has spent years on actuators, controller boards
and rig software.

## Decision

Componium emits **6DOF pose** (surge, sway, heave in metres; roll, pitch, yaw
in degrees) as curve tracks, and adapters map that onto specific rigs. We do
not write actuator control loops.

Candidate bridge targets, **licences unverified** — each must be checked
before we depend on it, as several widely used tools in this space are free
but not open source:

- SFX-100 / SimFeedback — DIY linear actuator ecosystem
- Thanos AMC controller boards — accept serial position commands
- FlyPT Mover — many input and output paths, strong filtering
- Direct serial/CAN to a controller, for rigs that expose one

The first adapter should target whatever accepts a plain UDP pose packet, as
that requires no reverse engineering.

## Consequences

- Motion is last in the roadmap not because it is unimportant but because it
  depends on the timing core, the safety supervisor and an adapter layer that
  only make sense once light and wind work.
- Bass shakers are a deliberate exception: cheap, nearly risk-free, and
  driven as an audio channel rather than an actuator. They are a good early
  win once the clock is trustworthy.
