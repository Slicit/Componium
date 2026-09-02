---
status: shipped
branch: feat-postgres
---

# feat-postgres · move derived data off the filesystem

The plan for ADR 0006. Four stages, each usable on its own, ordered so that the
riskiest unknowns are proved on the least valuable data.

Nothing here touches a score or a rig. Those stay files, and the conductor
keeps reading them, for the reasons in the ADR.

## Stage 0 · the seam, before any data moves

The whole migration is bearable or not depending on this stage, so it comes
first and it is small.

`internal/store` defines what the studio and the composer need from storage,
and nothing else. Two implementations from the start:

- `store/pg` on `pgx`
- `store/mem`, a map behind a mutex

Unit tests use `mem` and stay instant. One `store/storetest` package holds the
assertions both must satisfy, run against `mem` always and against `pg` when a
database is reachable. That is the only place a test needs a service.

Migrations are plain numbered `.sql` files, embedded with `go:embed`, applied
at startup, forward only. No framework. A migration runner is about eighty
lines and every framework in this space is a dependency with opinions.

**Done when** a table nobody uses can be created, migrated, written and read
through both implementations, and `go test ./...` still passes with no database
installed.

## Stage 1 · vision observations

The right first move: one writer, one reader, a schema already implicit in the
JSONL, and the data that actually hurts today.

```sql
create table observation (
  film        text        not null,
  at          double precision not null,   -- seconds into the film
  place       text,
  doing       text,
  seen        text,
  labels      text[],
  built_at    timestamptz not null default now(),
  primary key (film, at)
);
create index on observation (film, at);
```

The primary key is the fix for a bug we have already had. Chunks that stack
became 3720 rows where 459 were distinct, and the repair was a script; here it
is a conflict.

What it buys immediately: appending stops meaning rewriting the file, coverage
is a `count(*)` rather than a scan, and the scent scene pass and `hack/`
scripts stop grepping JSONL.

The composer writes through `psycopg`. Reads for the studio's vision panel go
through `store`.

**Done when** an analysis writes observations to Postgres, the vision panel and
the scent pass read them, and the existing `.seen.jsonl` files are imported by
a one-shot command that can be run twice safely.

## Stage 2 · the analysis queue, dropped and not built

Sold on a fact nobody had checked. `.jobs.json` has one writer, not two: three
call sites, all in the studio, all under one mutex. The race cited was between
`t.TempDir()` and a pump goroutine in a test, and was fixed by not starting the
pump there. `update` already takes a `persist` flag that progress passes as
false, so the frequent write this was going to save does not happen either.

Kept below as the design it would take, for the day it is needed: analysis
running somewhere other than the studio process. That day it becomes necessary
rather than tidy.

The file with one writer and a race that was in a test.

```sql
create table job (
  kind       text not null,
  film       text not null,
  state      text not null,
  label      text,
  progress   double precision,
  look_again boolean not null default false,
  limit_s    double precision,
  updated_at timestamptz not null default now(),
  primary key (kind, film)
);
create table chunk (
  kind text, film text, idx int, state text, seconds double precision,
  primary key (kind, film, idx),
  foreign key (kind, film) references job (kind, film) on delete cascade
);
```

Chunks become rows rather than a nested array, which is what makes resume a
query instead of a walk.

**Done when** the queue survives a studio restart through the database rather
than through `.jobs.json`, and the resume tests pass against both store
implementations.

## Stage 3 · history and measurements, dropped and not built

A directory read of about a dozen entries per film, when somebody opens the
version picker. Kept below for the same reason as stage 2.


Kept scores stay files; what moves is the *index* of them, which is what the
version picker actually reads.

```sql
create table kept (
  film text not null,
  id   text not null,
  path text not null,          -- the file is still the file
  note text,
  label text,
  kept_at timestamptz not null default now(),
  primary key (film, id)
);
```

Measurements from `LOGBOOK/experiments` get a home at the same time, so that a
prompt trial or a wind calibration is a query rather than a directory of
scratch files.

**Done when** the version picker reads the index from the database and a kept
score is still a file on disk that can be opened without one.

## Deployment

One compose service, one named volume, and the studio and composer given a URL:

```yaml
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_DB: componium
      POSTGRES_USER: componium
      POSTGRES_PASSWORD: ${COMPONIUM_DB_PASSWORD:-componium}
    volumes:
      - db:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U componium"]
      interval: 5s
```

The conductor gets nothing. It has no reason to know the database exists, and
keeping it that way is the point.

**A studio with no database still opens, edits and saves a score**, because
those are files. It refuses to analyse and shows no history, and says which.
That degradation is deliberate and should be tested, not discovered.

## What was actually built

Stage 0 and stage 1. Observations live in Postgres, the studio reads and writes
them there, `import-vision` moves what was already on disk, and a studio with no
database keeps them in files exactly as before.

One departure from the plan, in the right direction: **the composer needs no
driver and no database.** It already wrote one JSONL per chunk and the studio
already joined them, so there was a single writer in a single language sitting
at exactly the point where a run's observations become a film's. That join is
where they land. Python gained nothing but two fields it had been dropping.

## What this does not do

- No score or rig moves. The export-on-save design is written down in the ADR's
  alternatives and is not being built.
- No multi-user anything. Postgres makes it possible later; nothing here
  depends on it.
- No backups beyond a volume. Everything in the database is rebuildable from
  the films and the scores, which is the property that makes that acceptable.

## Decisions

- **2026-09-02 · Postgres rather than SQLite**, for the reasons in ADR 0006:
  two processes in two languages over a bind mount, and cgo breaking the
  cross-compile that SQLite was being defended for.
- **2026-09-02 · A store interface with a memory implementation.** Not for
  purity. It is what keeps six hundred tests instant and dependency free, and
  it means only the contract tests need a service.
- **2026-09-02 · Migrations are numbered SQL files and a small runner.** Every
  framework here brings opinions about a schema this project is capable of
  describing in plain SQL.
