# ADR 0001 — Scope and founding principles

Status: accepted · 2026-08-29

## Context

Componium drives physical effects in sync with film playback. The temptation
in this space is to build everything: a player, a lighting protocol, actuator
firmware, a motion cueing engine. Each of those has a mature ecosystem already.

## Decisions

### 1. Componium owns time, and delegates everything else

The conductor's irreducible job is answering *"what media time is it, right
now, and what should each instrument be doing?"* Anything that is not timing
is delegated to an existing ecosystem wherever one exists.

### 2. Emit intent in standard units; adapt at the edge

Instruments receive **domain values**, not device commands. A motion track
carries 6DOF pose (surge, sway, heave, roll, pitch, yaw) in metres and degrees.
A light track carries colour. A wind track carries a normalised intensity.
Mapping those onto a specific actuator, fixture or controller is the adapter's
job, at the edge.

This is what makes the same score portable across wildly different rigs, and
it is why we can bridge to existing hardware ecosystems instead of replacing
them:

| Domain | We emit | Adapter targets |
|---|---|---|
| Light | Colour / intensity | sACN, Art-Net, WLED |
| Motion | 6DOF pose | Existing sim-rig controllers and software |
| Custom | CIP native | Componium ESP32 nodes |

### 3. Do not build actuator control for motion

DIY motion platforms are a solved problem with an active community. Componium
emits 6DOF pose and hands off. Candidate bridge targets are recorded in
[0003](0003-motion-bridging.md); their licences must be verified before we
depend on any of them.

Note that **motion cueing (washout filtering) is largely not our problem**.
In sim racing, motion is derived from physics telemetry and must be washed out
to fit limited actuator travel. In cinema the motion is *authored*, by a person
working directly within the rig's declared limits. Washout only becomes
relevant if we later auto-generate motion from video analysis.

### 4. Safety is a v0 feature

Instruments declare `safe_state` and duty-cycle limits in their manifest. The
conductor heartbeats; instruments fail safe on their own when it stops. An
instrument must be safe when the conductor crashes, when the network drops,
and when a score is malformed. This ships before anything that involves heat,
water or moving mass.

### 5. Virtual instruments are a feature, not a test fixture

Most contributors will never own a motion platform. Every instrument kind has
a virtual implementation that participates fully in the protocol, so the
timing core can be developed, tested and demonstrated with no hardware at all.

## Consequences

- Milestones M1–M5 and M7 are completable with zero hardware.
- Componium is useless on its own for lighting or motion without an adapter.
  That is the intended trade.
- The protocol must carry units and limits explicitly, since the conductor
  never knows what device is on the other end.

## Out of scope

Stereoscopic 3D rendering. Componium may later need to *know* a film is 3D,
but it will not render or manage it.
