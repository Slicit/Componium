# Componium

An open source engine for 4D home cinema. It reads a score (a timeline of cues
bound to a piece of media) and drives physical instruments (motion, light,
wind, water, smoke, scent, haptics) in sync with playback.

## Stack

Nothing is implemented yet. Intended shape, revisit before committing to it:

- **conductor** · Go. Realtime scheduling, clock discipline, safety supervisor.
  Chosen for goroutines, timer quality, and single static binaries that cross
  compile to a Pi.
- **composer** · Python. Offline AI assisted score generation from video and
  audio. Python because the CV and ML tooling lives there.
- **studio** · Plain HTML, CSS and JavaScript, served by the Go binary and
  embedded in it. No bundler, no node_modules, no build step. Revisit if the
  editor outgrows it.
- **node** · C / ESP-IDF. Componium firmware for ESP32 instruments.
- Wire formats: WebSocket + JSON for control, UDP binary for curve frames,
  sACN / Art-Net for lighting.

## Conventions

- **No em-dashes.** Use commas, parentheses, or middle dots (`·`).
- **Dates: `YYYY-MM-DD`.** Always absolute.
- **File naming:** lowercase, dash-separated.
- **Branch naming:** `feat-<slug>` for features, `fix-<slug>` for fixes.
- **Instruments declare their own limits.** The conductor never assumes it
  knows what is on the other end of the wire. Units and ranges are explicit
  in every manifest and every track.
- **Safety belongs to the instrument.** Anything that can fail dangerous must
  fail safe without the conductor's help, and must be testable with the
  conductor deliberately killed.
- **Every instrument kind needs a virtual implementation** before its hardware
  driver is written. Contributors without a rig must be able to run everything.
- **Development runs on `claude-machine-02`.** Author locally, commit, push,
  pull on the box, run there. See the claude-machine skill.

## Non-goals

- Not a media player. Componium follows a player, it does not replace one.
- Not stereoscopic 3D. That is the display's job.
- Not a lighting protocol. We speak sACN and Art-Net rather than competing.
- Not actuator control loops for motion. We emit 6DOF pose and bridge to
  existing sim rig ecosystems.

## Milestones

Ordering principle: prove the timing core against virtual instruments before
spending money on hardware, and put anything involving heat, water or moving
mass last.

| ID | Milestone | Status | Verified against |
|---|---|---|---|
| M1 | Conductor, virtual instrument, latency compensation | done | tests |
| M2 | Time source (mpv) and clock | done | live mpv |
| M3 | Tuning: `componium tune` and `doctor` | done | live mpv |
| M4 | DMX light over sACN | done | real UDP, **no fixture** |
| M5 | Score format v1, rig files, `play` | done | live mpv |
| M6 | Safety supervisor and watchdogs | done | tests |
| M7 | Composer v0, LFE and frame colour | done | real video |
| M8 | CIP, `componium node`, ESP32 firmware | done | live node, **firmware uncompiled** |
| M9 | Studio timeline editor | done | API and node tests, **not in a browser** |
| M10 | Composer v1, subtitles and scene cuts | done | real video with subtitles |
| M11 | Motion 6DOF bridge, fog and water | done | real UDP, **no hardware** |

Nothing has been tested against physical hardware of any kind. Every driver is
verified over a real socket against a real listener, which proves the protocol
and the logic and proves nothing about a fixture, a fan, a fogger or a
platform.

## Reading order for agents

1. This file (`LOGBOOK.md`).
2. `LOGBOOK/notes.md` for codebase patterns, gotchas, anti-patterns.
3. The active feature file matching the current branch
   (`LOGBOOK/features/feat-<slug>.md`) if any.
4. `docs/adr/` for why the architecture is shaped the way it is, and
   `docs/cip.md` for the instrument protocol.

Do not read `LOGBOOK/ideas.md` unless the user explicitly asks, it is the
human owned inbox.

## Writing guidance for agents

- Append to the current feature's `## Decisions` log when making non-trivial
  choices. Use today's date.
- Propose additions to `notes.md` when you discover a transferable pattern,
  gotcha, or anti-pattern. Show a diff and wait for confirmation.
- Never edit `LOGBOOK.md` (this file) without showing the proposed change first.
- Never modify `ideas.md` without an explicit user request.
- Use `git mv` for renames and archive moves.

## Status

- LOGBOOK adopted: 2026-08-29
- Index regenerated: manual
- Active feature count: see `LOGBOOK/features/INDEX.md`
