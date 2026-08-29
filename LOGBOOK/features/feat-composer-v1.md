---
status: shipped
branch: feat-composer-v1
parent: feat-composer-v0
---

# Composer v1 · subtitles and scene cuts

## Intent

The expensive half. v0 proved the pipeline with two dumb signals; v1 adds the
two that carry meaning.

## Subtitle mining is the best signal in the project

Subtitles for the deaf and hard of hearing already contain timestamped, human
authored descriptions of exactly the events a rig wants to react to:

```
00:14:22,100 --> 00:14:24,000
[thunder rumbles]
```

A precise timestamp and a semantic label, written by a person who watched the
film, requiring no inference at all. Nothing else in the composer comes close
for accuracy per unit of effort, and it costs one ffmpeg invocation and a
regular expression.

## Decisions

### 2026-08-29

- **Decision:** Only bracketed or parenthesised text counts as a description.
- **Why:** Dialogue is not an effect. Matching against spoken lines would fire
  the rig every time a character said the word "wind".

### 2026-08-29

- **Decision:** The word to effect mapping is data, not code, and can be
  replaced with `--mapping`.
- **Why:** Somebody scoring a film in another language needs to replace it
  entirely, and should not have to edit Python to do so.

### 2026-08-29

- **Decision:** The mapping is deliberately conservative and small.
- **Why:** A false positive fires a physical effect in somebody's living room
  at a moment the film did not call for. That is worse than a miss: a miss is
  merely absent, a false positive is actively wrong.

### 2026-08-29

- **Decision:** Cues for the same instrument within half a second are deduped.
- **Why:** "[thunder rumbles and crashes]" matches three words and would
  otherwise fire the same shake three times in the same instant.

### 2026-08-29

- **Decision:** Scene snapping happens after curve compression, not before.
- **Why:** Compression drops points that resemble their neighbours, which is
  exactly what a holding point inserted at a cut looks like. Doing it in the
  other order would have the compressor delete the very points the snapper
  added.

### 2026-08-29

- **Decision:** Subtitle cues reuse the instrument ids the curve tracks already
  use, rather than defaulting to `<kind>.main`.
- **Why:** Found by running it: the first version generated a score naming both
  `light.ambient` and `light.main` for the same fixture, which no rig would
  satisfy.

## Not implemented: the vision model

A vision language model over keyframes was in the plan and is not here. There
is no model available in this environment, and inventing an interface to
something that has never been run would be worse than leaving the gap visible.

The shape it should take is clear from what is built: the cheap detectors
already produce candidate windows, and the expensive pass should run only on
those rather than on every frame.

## Verification

Twenty three Python tests. End to end against a purpose built clip with an
embedded subtitle track: six cues from four sound descriptions, correctly
routed to light, shake, mist and wind, with dialogue ignored, and every cue
annotated in the output with the subtitle it came from so a reviewer can judge
it.

**Scene cut detection is implemented but unexercised.** The test clip is a
continuous synthetic pattern with no cuts in it, so `detect` returned zero and
the snapping path has only unit test coverage.

## Links

- Branch: `feat-composer-v1`
- Related features: `feat-composer-v0`
