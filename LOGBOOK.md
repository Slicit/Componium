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
- **studio** · TypeScript / React. Timeline authoring UI.
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

| ID | Milestone | Hardware needed |
|---|---|---|
| M1 | Conductor, virtual instrument, fake clock, latency compensation | none |
| M2 | Real time source (mpv / VLC) and clock filter | none |
| M3 | Tuning: startup calibration and continuous refinement | none |
| M4 | First real instrument, DMX light over sACN | cheap |
| M5 | Score format v1, `play` and `rehearse` CLI | none |
| M6 | Safety supervisor and watchdogs, hardened | none |
| M7 | Composer v0, LFE to shake and brightness to light | none |
| M8 | Wind, ESP32 node with PWM fan | moderate |
| M9 | Studio timeline editor | none |
| M10 | Composer v1, semantic detection and subtitle mining | none |
| M11 | Fog, water, then motion via 6DOF bridge | expensive |

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
