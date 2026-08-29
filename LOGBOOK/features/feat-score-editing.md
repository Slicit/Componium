# feat-score-editing — hand editing a track to finish the render

Status: **deferred, not started.** Recorded 2026-08-29 at the user's request.

## What

Real editing of the generated score, not just correcting one cue at a time.
The composer's output is a first pass; finishing a film means shaping it by
hand. That wants, roughly:

- **Smoothing** a curve, or a stretch of one, without redrawing it point by
  point. The light curves in particular come out of the analysis noisy at the
  scale of a few frames, and the fix is a filter with a handle on it, not a
  hundred small edits.
- **Selecting a time range across tracks**, then cutting, copying and pasting
  it. Effects that belong together — a flash, its wind gust, the rumble under
  it — are authored together and should move together.
- **Transforming a selection**: scale levels, shift in time, stretch, reverse,
  thin out cues that are too dense.
- **Reusable light effects.** A flicker, a pulse, a slow wash-to-red are
  patterns an operator will want to name once and apply repeatedly.

## What exists today

The studio's inspector edits a single cue: time, duration, parameters, saved
back through the same parser `componium play` uses, so nothing can be written
that the player would refuse. Tracks can be muted and soloed. That is the whole
of it — everything above is new.

## Why it is tricky

- **Two different shapes of data.** Cues are discrete events with a start and
  optionally a hold; curves are sampled control points. "Copy this range" means
  something different for each, and a selection usually spans both.
- **Undo.** Range operations are not survivable without it, and there is no
  undo in the studio at all right now. This probably has to come first.
- **Smoothing must not violate what the score guarantees.** Duty cycles,
  maximum continuous run, the calm budget and the latency-compensated start and
  stop of every span are properties of the score, not decorations on it. A
  filter that rounds a corner can quietly extend a span past a device's
  `MaxContinuous`, and the machine that pays for that is one that moves a
  person. Every edit path needs to run back through validation, and the editor
  should refuse rather than warn.
- **Where the truth lives.** If a hand edit is a diff against the analysis,
  re-running the composer can keep it; if it overwrites, "rebuild all" silently
  destroys a day of work. The current library has a Rebuild button next to
  every film, so this is a real hazard, not a hypothetical one. Decide this
  before writing any editing UI.

## Related

- `feat-studio.md` — the editor as it stands, and the save round trip.
- `feat-score-format.md` — cues, curves, spans and what the parser accepts.
- `feat-safety.md` — the constraints an edit must not be able to break.
