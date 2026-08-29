# feat-body-haptics — premium seat shake and directional shoulder punch

Status: **deferred, not started.** Recorded 2026-08-29 at the user's request so
the idea is not lost. Nothing here is implemented.

## What

Two effects that act on the body rather than on the room:

- **Premium seat shake** — a richer rumble than the current single `shake`
  level. Texture and frequency, not just amplitude: an engine idle, a distant
  impact and a hull breach should not feel like the same vibration louder.
- **Directional shoulder punch** — a sharp, short impulse to the left or right
  shoulder, taken separately. This is the interesting one. It is what sells a
  hit that comes from off screen, and it is the difference between "something
  exploded" and "something exploded *on your right*".

## Why it is deferred

Both need the analysis engine to answer questions it currently cannot.

The composer today nominates from projections, luma and simple motion
estimation. It can tell that the frame is moving and roughly how much. It
cannot tell **what** moved, **who** it happened to, or **which side** it came
from — and a shoulder punch is worthless without the side, worse than
worthless if it gets it backwards. A left punch fired on the right is not a
degraded effect; it is a wrong one, and the viewer feels the mistake directly.

So this wants real scene understanding: object and person detection, tracking,
probably audio spatialisation as a second opinion on direction, and some way of
distinguishing an impact *to the camera/protagonist* from an impact merely
visible in the frame. That is a machine learning problem, and a considerably
larger one than anything in `composer/` today. It would also break the current
promise that analysis needs nothing but ffmpeg and Python — a model is a
dependency with weights, a licence and a download.

## What to keep in mind if this is picked up

- **Direction must be confirmed, not nominated.** The existing nominate/detect
  split already says water and plunges are candidates needing confirmation. A
  shoulder punch is stronger than that: it should be opt in per score, and
  wrong beats absent only if the operator chose it.
- **Stereo/surround audio is probably the cheaper signal** for laterality than
  vision is, and the composer does not look at audio at all yet. That may be
  the honest first step, well before any model.
- **The safety story does not change but gets sharper.** A punch is a short
  span; it wants the same start/stop discipline, duty cycle and calm budget as
  everything else, and it is exactly the sort of effect a viewer would want
  excluded from a calm region even when the film is briefly loud.
- Seat shake texture may be reachable *without* ML, by shaping the existing
  motion estimate — frequency content from frame to frame differences rather
  than a single amplitude. That part could be done first and separately.

## Related

- `feat-analysis-engine.md` — what the composer nominates today.
- `feat-motion-and-wet.md` — the existing motion path and washout filter.
- `feat-safety.md` — spans, duty cycles, all-stop.
