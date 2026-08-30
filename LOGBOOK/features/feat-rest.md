---
status: proposed
branch: feat-rest
---

# feat-rest — the platform should be still by default

Written 2026-08-30 after the observation that there is "way too much shake and
motion platform, and/or they seem too brutal too often, even on some absolutely
calm scenes".

## What was measured first

The obvious reading is that the amplitudes are too high. They are not:

| score | track | points | mean | median | above 0.5 |
|---|---|---|---|---|---|
| big-buck-bunny | `shake.seat` | 917 | 0.140 | 0.104 | 1% |
| big-buck-bunny | `motion.platform` | 1706 | 0.096 | 0.046 | 1% |
| sintel | `shake.seat` | 1284 | 0.176 | 0.138 | 4% |
| sintel | `motion.platform` | 3396 | 0.114 | 0.063 | 1% |

Almost everything is gentle. What is wrong is not how hard it pushes, it is
that **it never stops**. Sintel carries 3,396 motion points across 888
seconds — nearly four a second, for fifteen minutes, with a median of 0.063.
A platform that is always doing a little is more tiring than one that is
usually still and occasionally decisive, and it reads as "too much" even when
every individual value is small. Constant low-level movement is also the thing
that stops a viewer forgetting the chair is there, which is the whole point.

So this is not a gain problem. Turning `--motion-gain` down would make a
score that is still never at rest, only weaker.

## Why it happens

**Calm regions gate cues and never touch curves.** `dynamics.protect_calm`
takes cues, is called on cues, and returns cues. Shake, motion and wind are
all curve tracks, so the analysis works out where the film should be left
alone, drops the *events* in those stretches, and lets the curves run straight
through. That is the direct cause of movement in an absolutely calm scene, and
it is a one-line-to-describe, several-lines-to-fix defect.

**Peak normalisation manufactures activity in quiet material.**
`rms_windows` divides by the loudest window in the film, with the stated
reason that "a quiet film still produces a usable range". That is true and it
is also the problem: it guarantees every film, however calm, is scaled until
something is happening. Within a film it means a quiet scene's room tone is
measured against the loudest explosion and comes out small but never zero.

**Nothing has to start.** There is no threshold below which the platform is
simply at rest — a value of 0.03 is written down and played, so estimation
noise becomes movement. A deadband, and better a hysteresis (a higher level to
begin moving than to keep moving), is what makes stillness the default state
rather than a value that happens not to have been reached.

**The rest budget counts only cues too.** `enforce_budget` has the same shape
as `protect_calm` and the same blind spot, so a film can be over its budget
entirely in curve movement and the budget will report itself satisfied.

**`compress` preserves wandering.** It keeps a point whenever the signal has
moved by the threshold since the last kept one, which is right for a signal
that means something and wrong for one drifting around a noise floor: it turns
drift into a dense sequence of small, real-looking points.

## What to change

In the order they are likely to matter:

1. **Gate curves by calm.** Generalise `protect_calm` to take a curve and
   return one that is at rest inside calm regions, with a short ramp either
   side rather than a step — a platform snapping to zero at a region boundary
   is its own kind of event. The "worth interrupting for" exemption that cues
   already have should apply: a genuine peak inside a calm stretch may survive.

2. **A rest threshold with hysteresis.** Below a start level, output is zero;
   once moving, it continues until it falls below a lower stop level. This is
   what turns "small numbers everywhere" into "still, then moving, then still",
   and it is the change most likely to fix the complaint on its own.

3. **Reconsider peak normalisation.** Options, in increasing ambition: keep
   the peak but floor it, so a genuinely quiet film stays quiet rather than
   being amplified to fill the range; normalise against a rolling window so
   loud and quiet passages keep their relation to each other; or calibrate
   against absolute level and let the operator set the rig's overall intensity,
   which is where that decision arguably belongs anyway.

4. **Extend the rest budget to curves**, measuring time-under-movement rather
   than event count, so the budget means what it says.

5. **Deadband before compress**, so drift does not become points.

## How to tell whether it worked

Opinion is a poor instrument for this and the current numbers show why: every
individual value looked fine. The composer should report, and the tests should
assert, a few things about a whole score:

- **fraction of the film at rest** per track — the headline number, and the one
  that is currently near zero for motion;
- **longest continuous stretch of movement**, which is what fatigue actually
  tracks;
- **number of distinct movements** — a count of times a track left rest, which
  should be tens per film and is presently meaningless because it never
  returns;
- **movement inside calm regions**, which should be zero or close to it.

Those belong in the composer's existing report line next to "N calm regions,
Ns of the film left alone", and in `test_semantics.py` as assertions against a
synthetic film that is loud, then silent, then loud again. A score with no rest
should fail the suite.

## What not to do

Do not fix this by lowering the gain. It makes a score that is still never at
rest and merely weaker, and it would make genuine events too small at the same
time — the 1% of points above 0.5 are the ones that should survive intact.

## Related

- `feat-analysis-engine.md` — what the composer nominates and how.
- `feat-safety.md` — the duty cycle and maximum continuous run, which are the
  hardware-side version of this same concern.
- `feat-body-haptics.md` — deferred, and dependent on this: a shoulder punch is
  meaningless if the seat is already moving all the time.
