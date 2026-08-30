---
status: in progress
branch: feat-chunked-analysis
---

# feat-chunked-analysis · analysing a feature in pieces you can resume

Analysing a two hour film is a single run of tens of minutes that either
finishes or is worth nothing. It is often worth nothing: a restart of the
studio, a container recreated, a machine under load, and the whole thing
starts again from zero. Both features in this library are currently sitting
at `interrupted`, having each been most of the way through more than once.

So the work is cut into pieces that are recorded as they finish, and a run
that stops part way is continued rather than repeated.

## What a chunk is

**A time range, not a file.** The obvious reading of "chunks of 100MB" is to
split the prepared film with ffmpeg and analyse the pieces. Measured against
this library that is the wrong shape: the prepared copy of Wanted is 7.6GB and
the box has 14.5GB free, so splitting it would need most of the remaining
disk to hold a second copy of something we already have. A range costs
nothing, seeks are cheap in a faststart mp4, and it resumes exactly as well.

**The size target is converted to a duration.** 100MB is a good instinct —
it makes the piece proportional to the work rather than to the running time —
but it is measured in bytes and the analysis is paid for in seconds, and the
exchange rate is not a constant. In this library it varies by a factor of ten:

| prepared film | size | duration | 100MB is |
|---|---|---|---|
| Rebel Moon | 853 MB | 2h04 | 15.2 minutes |
| Wanted | 7.6 GB | 1h50 | 1.5 minutes |

Unclamped, that is 8 chunks of one film and 73 of the other, and 73 chunks
means 73 seeks into a 7.6GB file and four ffmpeg processes each time, which is
overhead standing in for work. So the byte target picks the duration and the
duration is then clamped to something sensible — no shorter than five minutes,
no longer than twenty. Wanted becomes 22 chunks, Rebel Moon 8.

## The part that would have been silently wrong

`rms_windows` normalises the audio envelope **by the loudest window in what it
was given**. Handed a chunk instead of a film, it normalises each chunk against
its own peak — so a quiet chunk is amplified until it matches an action chunk,
and the shake track jumps in character at every boundary. Nothing would fail;
the score would simply be wrong in a way that only shows up when you play it.

The fix is a cheap pass over the whole film's audio before any chunk runs,
which finds the real peak and hands it to every chunk. It is affordable:
measured at **21.5 seconds for a two hour film**, because the pass is already
downsampling to 1kHz mono and that is 345 times realtime.

This is worth stating plainly because it is the general hazard of chunking an
analysis: anything that normalises, averages or ranks across the whole film is
silently redefined when the film becomes a piece of one. The audio peak is the
one that exists today. Anything added later that looks across the whole film
has to be checked against this.

## Boundaries

Motion estimation compares each frame to the one before it, so the first frame
of a chunk has nothing to compare against and reports no movement. One dead
sample per boundary is not much, but it is avoidable: a chunk decodes from a
little before its start and discards what it produces before that start. The
composer does the discarding, so the merge stays a concatenation rather than
becoming an edit.

## Resuming

Chunk state is part of the job and is persisted with it, so it survives the
restart that caused the problem. A chunk becomes `done` only once its partial
score is written, so a chunk that is `done` is known good.

Resume restarts at **the chunk before the first one that is not done**, which
is what was asked for. It costs one chunk of repeated work and buys not having
to reason about how far a killed process got.

Reset throws away every partial and starts again from zero.

## Where the pieces live

Partial scores go in `<scores>/.partial/<film stem>/chunk-000.componium` and
are deleted once the merge has written the real score. A partial is a complete,
valid score covering a time range — not a fragment — so anything that can read
a score can read one, which is what makes the state debuggable by hand.

## Related

- `feat-rest.md` — the scoring review. Its first finding and this feature's
  hazard are the same mechanism seen from two sides: normalising by the peak
  of whatever you were handed.
- `feat-analysis-engine.md` — what the composer nominates and how.
