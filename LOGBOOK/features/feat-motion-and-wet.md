---
status: shipped
branch: feat-motion-and-wet
---

# Motion, fog and water

## Intent

The last milestone, and last for a reason: these are the effects that can hurt
someone. Everything protecting them had to exist first.

## Decisions

### 2026-08-29

- **Decision:** Fog, water and mist get no driver of their own. They are CIP
  nodes with strict manifests.
- **Why:** A fogger is a relay. Writing a bespoke driver would add a code path
  to maintain and would tempt someone to put the safety logic in it, when the
  safety logic belongs in the node, where it survives the conductor crashing.
  What these instruments actually need is not code but declared limits and a
  bring-up procedure, which is `docs/wet-and-hot.md`.

### 2026-08-29

- **Decision:** `instruments/motion` emits 6DOF pose and nothing else. No
  actuator control, no inverse kinematics, no washout filter.
- **Why:** ADR 0003. The sim racing community has spent years on actuators and
  controller boards. Also, washout solves a problem cinema does not have:
  authored motion is written inside the rig's declared limits already. Only
  generated motion needs it, and that belongs in the composer, offline.

### 2026-08-29

- **Decision:** Undeclared travel limits default to 5 cm and 5 degrees.
- **Why:** A rig that has not said how far it can move should move timidly, not
  confidently. The alternative, defaulting to generous limits, means the first
  person to forget the `travel` block finds out by listening to their platform
  hit its end stops.

### 2026-08-29

- **Decision:** Clamping is counted and reported, not silent.
- **Why:** A score that clamps constantly was written for a different rig. The
  operator should be told, rather than left wondering why nothing ever reaches
  the extremes.

### 2026-08-29

- **Decision:** The wire format is plain CSV by default, with a labelled
  variant.
- **Why:** Unglamorous and readable by anything, including a script somebody
  wrote in an afternoon to drive their own rig. A clever binary format would
  buy nothing at 50 Hz and cost every adapter author an hour.

## Verification

Nine tests over a real UDP socket: CSV and labelled encoding, clamping to
declared travel, clamp counting, NaN becoming zero rather than garbage, `safe`
commanding neutral, tiny defaults, and refusal of unusable configuration.

**No hardware of any kind was involved.** No motion platform, no fogger, no
valve, no mister. Every claim about physical behaviour in `docs/wet-and-hot.md`
is reasoning, not measurement. The 60 ms motion latency in the example rig is
invented.

## Links

- Branch: `feat-motion-and-wet`
- External: `docs/adr/0003-motion-bridging.md`, `docs/wet-and-hot.md`
