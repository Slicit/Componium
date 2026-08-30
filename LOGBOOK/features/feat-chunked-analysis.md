---
status: shipped
branch: feat-chunked-analysis
---

# feat-chunked-analysis · analysing a feature in pieces you can resume

Analysing a two hour film is a single run of tens of minutes that either
finishes or is worth nothing. It is often worth nothing: a restart of the
studio, a container recreated, a machine under load, and the whole thing starts
again from zero. Both features in this library were sitting at `interrupted`,
having each been most of the way through more than once.

So the work is cut into pieces that are recorded as they finish, and a run that
stops part way is continued rather than repeated.

## What a chunk is

**A time range, not a file.** The obvious reading of "chunks of 100MB" is to
split the prepared film with ffmpeg and analyse the pieces. Measured against
this library that is the wrong shape: the prepared copy of Wanted is 7.6GB and
the box has 14.5GB free, so splitting it would need most of the remaining disk
to hold a second copy of something we already have. A range costs nothing,
seeks are cheap in a faststart mp4, and it resumes exactly as well.

**The size target is converted to a duration.** 100MB is a good instinct — it
makes the piece proportional to the work rather than to the running time — but
it is measured in bytes and the analysis is paid for in seconds, and the
exchange rate is not a constant. In this library it varies by a factor of ten:

| prepared film | size | duration | 100MB is |
|---|---|---|---|
| Rebel Moon | 853 MB | 2h04 | 15.2 minutes |
| Wanted | 7.6 GB | 1h50 | 1.5 minutes |

Unclamped, that is 8 chunks of one film and 73 of the other, and 73 chunks
means 73 seeks into a 7.6GB file with four ffmpeg processes each time, which is
overhead standing in for work. So the byte target picks the duration and the
duration is then clamped — no shorter than five minutes, no longer than twenty.
Rebel Moon plans as **9 chunks of 15.2 minutes**, which is what it actually did.

## Chunks read the prepared copy

Not the original. This started as a detail of the request — "100MB of the
processed version" — and turned out to be most of the speed:

| decoding 120s to the grayscale pass | time |
|---|---|
| the H.265 original | 25.0s |
| the prepared H.264 copy | 4.7s |

Identical output, byte for byte, **5.3 times faster**. Everything the analysis
looks at is downscaled to 64x36 and 1kHz mono, so there is nothing in the
original for it to see that the copy has lost. Measured over the whole film
that is the difference between 26 minutes of decoding and 5.

The score still binds to the film rather than to the copy, via `--hash-file`.
Hashing the copy would mean regenerating a preview silently unbound every score
made from it.

## The part that would have been silently wrong

`rms_windows` normalises the audio envelope **by the loudest window in what it
was given**. Handed a chunk instead of a film it normalises each chunk against
its own peak — so a quiet chunk is amplified until it matches an action chunk,
and the shake track jumps in character at every boundary. Nothing would fail;
the score would simply be wrong in a way that only shows when you play it.

The fix is a cheap pass over the whole film's audio before any chunk runs,
finding the real peak and handing it to every chunk. Measured at **21.5 seconds
for a two hour film**, because the pass is already downsampling to 1kHz mono.

This is worth stating plainly because it is the general hazard of chunking an
analysis: anything that normalises, averages or ranks across the whole film is
silently redefined when the film becomes a piece of one. The audio peak is the
one that exists today. Anything added later that looks across the whole film
has to be checked against this.

## Four faults, all found by running a real film

Unit tests passed throughout every one of these.

**The flash pass was still reading the whole film.** It decodes the film a
second time at a higher rate, and threading the range through most of the
decodes is not the same as threading it through all of them: every chunk found
the same flashes and reported them at its own offset, so a chunk starting at
38s reported the film's 23s lightning at 59s.

**A curve steady across a boundary lost its value.** Compression runs before
the trim and keeps a point only where the signal changed, so a curve could have
no point between a range's start and seventeen seconds inside it. Merged, that
ramps from the previous chunk's last value through a stretch the film spent
perfectly still. Curves now hold their value at the boundary; cues do not,
because moving an event there would report something that did not happen.

**A partial could be a curve of one point, which is not a valid score.** The
format refuses a single-point curve — it is ambiguous between a level and an
instant — and a chunk over a still stretch produces exactly that. It would have
failed at the merge, after all the work was done. A lone point is now repeated
at the far edge of the range, which is what a flat curve looks like written
down.

**Queueing a film again dropped the record of what was finished.** `Enqueue`
built a fresh job, so asking to resume silently meant starting over: the
finished pieces were still on disk and were redone anyway. Found by
interrupting a real analysis after two of its nine pieces had landed.

## Resuming

Chunk state is part of the job and is persisted with it, so it survives the
restart that caused the problem. A chunk becomes `done` only once its partial
score is written, so a `done` chunk is known good.

Resume restarts at **the chunk before the first one that is not done**, which
is what was asked for. It costs one chunk of repeated work and buys not having
to reason about how far a killed process got.

Reset throws away every partial and starts again from zero. It is a separate
request rather than a flag, and it asks first, because what it discards can be
an hour of work that nothing else in the studio can get back.

## Where the pieces live

Partial scores go in `<scores>/.partial/<film stem>/chunk-000.componium` and
are deleted once the merge has written the real score — never before, because
until then they are the only copy of the work. A partial is a complete, valid
score covering a time range, not a fragment, so anything that can read a score
can read one. The numbering is fixed width so that sorting the filenames sorts
the chunks; a feature is more than ten pieces, and `chunk-10` sorts before
`chunk-2`.

## What was verified, and how

- A whole-film run and a two-chunk run of the same film, sampled every half
  second and compared: **median difference 0.0000** on shake, motion and light;
  mean 0.0012 on motion.
- A single chunk film analysed through the new path produces the same score as
  before it existed: 5 tracks, 25 cues, 596.48s.
- A real two hour film planned as 9 chunks, interrupted after two had finished,
  and resumed: it restarted at chunk 1 and did not redo chunk 0.
- Reset removed the partials and the state.

## Related

- `feat-rest.md` — the scoring review. Its first finding and this feature's
  hazard are the same mechanism seen from two sides: normalising by the peak of
  whatever you were handed.
- `feat-analysis-engine.md` — what the composer nominates and how.
