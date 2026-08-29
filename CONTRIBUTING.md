# Contributing to Componium

## Before your first pull request

**You need to sign the CLA.** See [CLA/README.md](CLA/README.md) for what it is
and why. The short version: Componium is AGPL-3.0 and also offered
commercially, and that second option only survives while one party holds
sufficient rights. One unsigned merge ends it permanently.

The check runs automatically on your first pull request and tells you what to
do. It is a comment and a reply, not paperwork.

## What is most useful

**Instruments.** The protocol is public domain
([docs/cip.md](docs/cip.md)) precisely so anyone can implement one, in any
language, under any licence. If you have hardware nobody has driven yet, that
is the highest value thing you can bring.

**Hardware reports.** Nothing in this project has ever driven a physical
device. If you run it against a real fixture, fan, fogger or platform, say what
happened, including the declared latency you ended up using. That number is
currently a guess everywhere it appears.

**Scores.** Scores bind to media by content hash, so they are shareable. A
library of them is plausibly this project's most valuable asset.

## House rules

- **LF line endings, everywhere.** `.gitattributes` and `.editorconfig` handle
  it; do not fight them.
- **`gofmt` and `go vet` clean.** CI is not doing this for you yet, so please
  run them.
- **Tests that assert behaviour, not implementation.** The good tests here are
  the ones named after the thing that would go wrong: a cue is dispatched early
  by the instrument's latency, a node goes safe when heartbeats stop, precision
  is never optimistic.
- **Nothing that can hurt someone gets a default.** Duty cycles, travel limits
  and safe states are declared, never guessed. A guessed limit that is too
  generous is worse than an obvious absence. See
  [docs/wet-and-hot.md](docs/wet-and-hot.md).
- **Record why in the LOGBOOK.** Decisions live in
  `LOGBOOK/features/feat-*.md`, and the reasoning is worth more than the diff.

## Running things

```sh
go test ./...
cd composer && python3 -m unittest discover -p 'test_*.py'
cd internal/studio/assets && node timecode.test.js
sh hack/make-testclips.sh      # fixtures for the manual end to end checks
```

No hardware is needed for any of it. Every instrument kind has a virtual
implementation, and `componium node` is a complete software instrument over the
network.
