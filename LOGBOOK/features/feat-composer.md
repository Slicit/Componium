---
status: draft
branch: feat-composer
---

# Composer · AI assisted score generation

## Intent

Authoring a score by hand takes hours per film, so hand authoring alone limits
Componium to a handful of titles. The composer analyses a film (video plus its
soundtrack, plus its subtitle track) offline and proposes a complete score,
which a human then reviews and refines in the studio. This is what makes "any
movie in 4D" possible rather than "the six films someone scored by hand".

Fitting, given the name: Winkel's Componium of 1821 was notable precisely
because it composed by itself.

## Plan

The composer is **offline and slow by design**. It never runs during playback.
It consumes media and emits a score file. The conductor stays realtime and
dumb, and knows nothing about how a score was produced.

Signals, ordered by value per unit of effort:

1. **Audio, LFE channel.** Sub bass energy maps almost directly onto shake and
   rumble. Nearly free to compute and immediately convincing. Start here.
2. **Video, per frame brightness and colour.** Drives the ambient light curve.
   This is what Ambilight does, it is cheap, and it demonstrates the whole
   pipeline end to end.
3. **Subtitle tracks (SDH).** Badly underrated. Hearing impaired subtitles
   already contain timestamped semantic effect labels: `[thunder rumbles]`,
   `[rain patters]`, `[explosion]`. Precise, free, and machine readable.
4. **Scene cut detection.** Prevents effects bleeding across hard cuts, which
   is the most obvious tell of a bad automatic score.
5. **Audio onset and spectral flux.** Impacts, gunshots, explosions.
6. **Optical flow.** Global camera motion becomes 6DOF pose for motion rigs.
7. **Vision language model over keyframes.** Semantic events that none of the
   above catch: it is raining, we are underwater, the room is on fire. This is
   the expensive pass, run on candidate windows the cheap detectors flagged
   rather than on every frame.

Then:

8. **Constrain to the rig.** Generated output passes through a limiter that
   enforces each instrument's declared duty cycle and travel before the score
   is ever playable. A model must never be able to hold a water valve open for
   four minutes.
9. **Human review in the studio.** The output is a proposal. "AI assisted",
   not "AI automatic", and the review step is a safety control, not a nicety.

## Decisions

### 2026-08-29

- **Decision:** The composer is a separate offline pipeline, not a conductor
  component, and communicates only by emitting a score file.
- **Why:** Keeps all ML and CV dependencies out of the realtime path, lets the
  composer be written in Python while the conductor stays Go, and means a
  generated score is inspectable and editable before anything moves.
- **Impact:** Requires the score format (M4) to be stable first. The composer
  cannot start in earnest until then, though the audio and brightness
  extractors can be prototyped against a draft format.

### 2026-08-29

- **Decision:** Scores bind to media by content hash plus duration, not by
  filename.
- **Why:** A score then follows the film across rips and renames, which is
  what makes a shared community score library possible. Crowd sourced scores
  are plausibly the project's most valuable asset, and generated scores are
  how that library gets seeded.
- **Impact:** Distribution question to settle later: a score is timing
  metadata and not the film, but the library needs a clear position on this
  before it is published.

### 2026-08-29

- **Decision:** Motion cueing (washout filtering) comes back into scope, but
  only here.
- **Why:** `docs/adr/0001-principles.md` argues washout is unnecessary because
  authored motion is written directly within rig limits. That holds for hand
  authoring. It does not hold for generated motion derived from optical flow,
  which is unbounded and must be washed out to fit actuator travel.
- **Impact:** The washout filter belongs in the composer, offline, not in the
  conductor. The realtime path stays free of it.

## Links

- Branch: `feat-composer`
- PR: TBD
- Related ideas: `LOGBOOK/ideas.md` 2026-08-29
- Related features: none yet
- External: none
