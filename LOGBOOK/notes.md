# Notes

Codebase learnings, patterns, and anti-patterns. Add entries as they are
discovered, with the date.

## Patterns

## Gotchas

### 2026-08-29 · Line endings are LF. Everywhere. Always.

Everything runs on Linux. CRLF must never enter this repository, in any file
type, for any reason.

The trap is that CRLF is introduced **at the moment a file is written on the
Windows laptop**, not during sync or checkout. A file authored locally has CRLF
on every line before git ever sees it. The symptom on the box is a script that
fails with a confusing `not found` error, or firmware build steps that break
for no visible reason, and a plain read of the file shows nothing wrong.

Three layers defend against it, and all three must stay in place:

1. `.gitattributes` pins `* text=auto eol=lf`, so the committed blob is
   normalised even when the working file is not. This has been verified: the
   blobs in the genesis commit are clean LF despite every working file having
   CRLF at the time.
2. `.editorconfig` sets `end_of_line = lf` for editors and tooling that honour
   it.
3. Global git config on the Windows laptop pins `core.autocrlf=false` and
   `core.eol=lf`. There was previously no line ending configuration at all,
   which is why nothing normalised on checkout.

When writing files locally with shell heredocs, strip carriage returns before
committing, or the working tree drifts from the index:

```sh
find . -path ./.git -prune -o -type f -print | xargs sed -i 's/\r$//'
```

To check the whole tree at any point:

```sh
find . -path ./.git -prune -o -type f -print | xargs grep -lU $'\r'
```

### 2026-08-29 · Containers leave root owned files in bind mounted checkouts

Running a container as root against a bind mounted checkout leaves root owned
files behind (this has bitten the sibling projects repeatedly, in `/tmp`, as
`__pycache__`, and across Rails bootsnap cache directories). A plain `rm -rf`
then fails on hundreds of them, and under `set -e` a script dies there and
silently skips every later step. Use `sudo rm -rf`, and prefer running
containers as the checkout owner in the first place.

### 2026-09-01 · CI has no ffmpeg, and neither does anything that stubs it

A test that stands in for the decoder has to stand in for the decision to call
it. `vision.ffmpeg()` is that seam; patch it, not just `keyframe` and
`subprocess.run`, or the test is green where ffmpeg is installed and vacuous
where it is not. Nine tests were in that state for three commits, one of them
asserting an empty list and being handed one for free.

To check before pushing, hide it from `PATH` rather than trusting the box:

```sh
mkdir -p /tmp/noffbin
for f in /usr/bin/* /bin/*; do
  b=$(basename "$f")
  case "$b" in ffmpeg|ffprobe) ;; *) ln -sf "$f" "/tmp/noffbin/$b" ;; esac
done
(cd composer && PATH=/tmp/noffbin python3 -m unittest discover -p 'test_*.py')
```

## Anti-patterns

### 2026-09-01 · A library that describes and a consumer that guesses

Presets are declarative (a normalised envelope and a list of kinds) and the
insert translated them into a track. Every field the insert had to guess at
became a place the two could disagree, and all three of them did at once:
a twelve pulse strobe collapsed into a single flash, the levels landed in
`r`/`g`/`b` on a track written in `h`/`s`/`i`, and the peak was written into
the hue as well as the intensity. The picker also offered five motion presets
the insert then refused, so the button did nothing and said nothing.

The rule, and it generalises past this one library: **anything an index offers
must be usable where it is offered, and the check belongs in a test that walks
the index rather than in a comment asking the next person to remember.** See
`web/src/core/parity.test.ts` and `LOGBOOK/features/feat-effect-parity.md`.

Two details worth carrying elsewhere:

- Assert **both directions**. "Everything offered works" is satisfied by
  offering nothing. Pair it with "everything withheld would genuinely have
  failed" or the filter becomes the bug.
- A **default is not a refusal**. `CHANNELS_BY_KIND[kind] ?? ['intensity']`
  and `kinds.get(kind, kind + ".main")` are the same mistake in two languages:
  a missing entry produces a plausible answer instead of a complaint, and the
  plausible answer travels a long way before anybody notices.

### 2026-09-05 · One replay counter per board means one client per board

A node keeps a single `s_highest_counter` for the whole board, not one per
sender, and refuses any authenticated JSON whose `n` is not above it. Every
client seeds its counter from the wall clock in microseconds and then adds one
per message. So a client that connects later starts millions of counts ahead,
and from its first message every message from the earlier client is refused,
permanently, in silence.

Reproduced deliberately with `hack/poke-together.py`: one POST to
`/api/boards/check` in the middle of a run silenced the script for the rest of
it, 94 refusals, two of its four cues never landed. With nothing else talking,
the same run refuses nothing.

Three things make it hard to see from either end.

- Curve frames carry no counter, so they keep arriving. The outputs still move
  and only the cues, the stops and the heartbeats vanish, which reads as a
  scoring problem rather than a transport one.
- Cues sent before the other client spoke still land, so the failure starts
  part way through and looks intermittent.
- The refusal is counted on the board and logged nowhere the operator sees. The
  status page on port 80 is the only place the number appears.

What it costs in practice: opening the studio's Boards page during a show stops
the conductor being heard by that board until the conductor reconnects. The
watchdog then drops the board's outputs to safe after 300ms, which is the
correct behaviour and the wrong reason.

Not fixed. The shape of a fix is a counter per sender rather than per board,
keyed by whatever identifies a client, which is a protocol change and wants an
ADR. A smaller version is for a client to seed from the clock and keep seeding
from the clock rather than incrementing by one, so two clients interleave
instead of one overtaking the other for good. That is a one line change in
`internal/cip/client.go` and it does not fix a client that is not ours.
