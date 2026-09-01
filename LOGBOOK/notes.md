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
