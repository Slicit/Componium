# ADR 0006 — Postgres for derived data, files for authored artefacts

Status: accepted · 2026-09-02, amended 2026-09-02

## Context

Componium keeps everything on disk: scores and rigs as TOML, vision
observations as JSONL, the analysis queue as a JSON file rewritten on every
state change, kept scores as a directory of files, layouts as small JSON blobs
beside them.

Most of that was right when there was one process. There are now three that
touch the same data, in two languages: the studio (Go) edits scores and rigs
and owns the job queue, the composer (Python) writes observations while it
analyses, and the conductor (Go) reads a score and a rig to run a show.

The strain shows in one place rather than generally. A vision file is
rewritten whole to append to it, a bug that stacked chunks needed a bespoke
repair script, and questions anybody would ask of the observations, which
scenes mention fire, what the coverage of a chunk is, are answered by grepping
JSONL from scripts in `hack/`. None of that is a size problem: the whole
library is a few megabytes and 8,746 observations.

The first version of this document also said the job queue was a JSON file with
two writers that had produced a race. That was wrong and is corrected below.

## Decision

**Postgres, in a side container, for data the system derives. Files for the two
artefacts a person authors.**

| Stays a file | Moves to Postgres |
|---|---|
| Scores (`.componium`) | Vision observations |
| Rigs (`.toml`) | |
| Media | |
| Firmware images | |
| The analysis queue | |
| Score history | |

The right hand column was longer when this was written. See the amendment.

The rule, and the reason it is a clean line: **anything in the database can be
deleted and regenerated; anything in a file is something a person made.** A
corrupt database costs a re-analysis. It never costs somebody's work.

## Amendment, the same day: only observations move

Stages 2 and 3 were dropped before being built, because the reasons given for
them here did not survive being checked. Recorded rather than quietly deleted,
since the next person to look at a JSON file beside a database will have the
same idea.

**The queue does not have two writers.** `.jobs.json` is written from three call
sites, all in the Go studio, all inside one process holding a mutex. The
composer never touches it. The race cited was in a test, between `t.TempDir()`
and a pump goroutine, and was fixed by not starting the pump in that test. It
had nothing to do with the file format.

**Nor is it rewritten often.** `update` takes an explicit `persist` flag and
progress passes `false`, so the hot path was already avoided by whoever wrote
it. The file is 21KB and is written a handful of times per analysis.

**The history index is a directory read of about a dozen entries per film**,
performed when somebody opens the version picker.

**And every table costs a file fallback, permanently.** The studio has to work
without a database, so anything moved here has to keep working when there is
none. For observations that was free: the file path already existed and moving
them deleted no code. For the queue and the history it would be new duplicated
code, maintained for ever, so that files which are not hurting can also live in
Postgres. That makes those two stages net negative on complexity today.

**What would change it.** The day analysis runs somewhere other than the studio
process, a worker on another machine taking jobs off a queue, stage 2 stops
being tidiness and becomes necessary. That is the trigger to watch for. It is
not a reason to build it now.

## Why not SQLite

It was the first proposal and it was wrong for reasons worth recording, because
they are not the usual ones.

**Two processes, two languages, one bind mount.** The studio and the composer
both write, from Go and from Python, to a file mounted into containers. That is
exactly where SQLite's locking stops being a property of the library and starts
being a property of the host filesystem. "Probably works if the filesystem
cooperates" is a poor foundation for something that runs unattended for an hour
of analysis.

**cgo.** `mattn/go-sqlite3` needs it, and cgo is precisely what breaks
cross-compiling a static binary to a Pi. The pure Go alternative is less
proven. `pgx` needs no cgo at all, so **Postgres is the better choice for the
static binary property than SQLite is**, which inverts the argument SQLite was
being defended with.

**And the constraint did not apply.** Everything moving to a database is owned
by the studio and the composer, both of which already run in containers beside
a compose file. The conductor, which is the thing that must run on a Pi with no
services, touches none of it.

## Why the conductor still reads files

Unchanged, and not for any reason to do with which database.

ADR 0001 §4 requires the rig to stay safe when the conductor crashes and when
the network drops. A show that will not start because a database is down is a
strictly worse home cinema than one that reads a file. The conductor's
dependencies at showtime are a safety property, not a convenience.

So a score and a rig remain what they are: static assets, readable with `cat`,
diffable, committable, and sendable to somebody who wants to watch the same
film with the same effects. For an open source 4D cinema, a shared library of
scores is plausibly the whole ecosystem, and a row in a database cannot be
emailed.

## Consequences

- **A new service in the deployment.** One compose block, one volume, and a
  failure mode that did not exist: the studio cannot analyse or list history
  while the database is down. It can still open a score, edit it and save it,
  because those are files.
- **Two drivers to keep working**, `pgx` in Go and `psycopg` in Python. Both are
  ordinary; it is still two.
- **Migrations become a thing that exists.** They are versioned, applied at
  startup, and forward only.
- **The test suite grows a dependency.** Today the whole Go suite runs with
  nothing installed, which is worth something and is about to cost something.
  See below.
- **The contract test empties the database it is given**, because every subtest
  has to start from nothing. `COMPONIUM_TEST_DB` therefore refuses a URL whose
  name does not contain `test`. This is not hypothetical: it was pointed at the
  real database an hour after being written, and deleted 8,746 rows that had
  just been imported. They were rebuildable, which is the property this whole
  decision leans on, and it should still not be one paste away.
- **Nothing in the database is precious.** Every table can be rebuilt by
  re-running an analysis, which is what makes backup a nice-to-have rather than
  a prerequisite.

## The test story, which is the part that gets underestimated

600 or so tests currently need no service at all. The honest options are a real
database in CI, containers spun up per package, or a storage interface with an
in-memory implementation. All three cost something on every test that touches
data, and that cost is usually the largest line in a migration like this and
never appears in the estimate.

The decision: **a `store` interface with two implementations**, Postgres and
in-memory. Unit tests use memory and stay instant. A smaller set of contract
tests runs the same assertions against both, and those are the only ones that
need a database.

That also gives the test runner something to work with. Tests run for what
changed and its close neighbours, with the full suite reserved for shared types,
exported surfaces and cross-cutting infrastructure; the contract tests are
exactly the set that a change to storage should pull in, and nothing else needs
to.

## Alternatives considered

**Everything in Postgres, files exported on save.** Coherent, and it keeps one
source of truth: the file becomes a build artefact rather than a second master.
Rejected for now because it costs hand-editing, which we deliberately preserved
when the rig gained an editor, and because the export can be stale in exactly
the moment that matters. Revisit if concurrent editing becomes a requirement.

**Leave it as files and fix the sharp edges.** Defensible on today's data
volume. Rejected because the sharp edges are not incidental: appending to a
vision file means rewriting it, and the queue's second writer is not going away.

**SQLite.** Above.
