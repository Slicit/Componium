---
status: shipped
branch: feat-composer-v0
parent: feat-composer
---

# Composer v0

## Intent

The cheap half of `feat-composer`: prove the pipeline end to end with the two
signals that cost least and deliver most, before spending anything on semantic
detection.

## Decisions

### 2026-08-29

- **Decision:** No Python dependencies at all. ffmpeg and the standard library.
- **Why:** The composer has to run wherever ffmpeg does, including on a
  headless box someone set up once and never touched again. A numpy or scipy
  requirement is a barrier for a project whose audience is people wiring fans
  to microcontrollers.
- **Impact:** RMS is computed with `array` and a loop, and TOML is written by
  hand. Both are fast enough: a two minute clip takes 1.16 seconds.

### 2026-08-29

- **Decision:** ffmpeg scales each frame to one pixel so that ffmpeg does the
  averaging.
- **Why:** Reading frames into Python to average them would be far slower and
  considerably more code, for an identical result.

### 2026-08-29

- **Decision:** Audio is low passed and resampled to 1 kHz before analysis.
- **Why:** Nothing below 120 Hz needs 48 kHz to measure. This is a 48 times
  reduction in the data before any Python touches it.

### 2026-08-29

- **Decision:** Curve points within a threshold of the previous kept point are
  dropped, endpoints always retained.
- **Why:** A two hour film at 4 Hz is 28,800 points per track. A score nobody
  can open is a score nobody will edit, and editing is the point. Measured
  compression on a two minute clip was 480 samples to 87 points.

### 2026-08-29

- **Decision:** Media is hashed over the first 64 MB plus the file size by
  default, labelled `sha256-first64mb`.
- **Why:** Hashing a ten gigabyte remux in full is slow enough to discourage
  use. The prefix is in the value itself so nothing can mistake a partial hash
  for a full one later.

## Verification

Twelve unit tests over the pure functions. End to end against a real clip:
score generated in 1.16 s and accepted by the Go parser via `componium
validate`, which was added for exactly this purpose.

One real bug found by the tests: the original timecode formatter decomposed
before rounding, so a value like 59.9995 could produce `00:00:60.000`. It now
rounds to whole milliseconds first and decomposes afterwards.

**Caveat on the demonstration.** The test clip is synthetic, a test pattern
with a constant 440 Hz sine. Its audio energy barely varies, which is why the
shake track compressed to six points. The pipeline is proven; its output has
not been judged against real film content.

## Links

- Branch: `feat-composer-v0`
- Related features: `feat-composer` is the parent, `feat-score-format` is what
  it emits
