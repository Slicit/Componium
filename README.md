# Componium

**One score, many instruments.** An open-source engine for building 4D home
cinema — motion, light, wind, water, smoke, scent and haptics, all cued to
the film.

Componium reads a *score* (a timeline of cues bound to a piece of media) and
drives your *instruments* (motion rig, fans, DMX fixtures, foggers, misters,
shakers) through a single *conductor* that keeps everything locked to playback
timecode. Instruments are plugins: if you can control it, you can score it.

> Status: **alpha.** Every part exists and is tested, and none of it has ever
> driven a physical device. Drivers are verified over real sockets against real
> listeners, which proves the protocol and proves nothing about a fixture, a
> fan, a fogger or a platform. Read [docs/wet-and-hot.md](docs/wet-and-hot.md)
> before pointing it at anything that can hurt you.

## Try it with no hardware

```sh
mpv --input-ipc-server=/tmp/mpv.sock film.mkv
componium play -score examples/demo.componium -rig examples/demo-rig.toml
```

Everything in that rig is virtual, so it prints what a real rig would have been
told. `componium node` adds a software instrument over the network, and
`componium studio` opens the timeline in a browser.

To generate a score from a film:

```sh
python3 composer/compose.py film.mkv -o film.componium
componium validate -score film.componium -rig my-rig.toml
```

## Commands

| | |
|---|---|
| `componium play` | play a score against a rig |
| `componium rehearse` | dry run against a player, virtual instruments only |
| `componium validate` | check a score, optionally against a rig |
| `componium tune` | measure this machine and player |
| `componium doctor` | print the tuning profile and what it means |
| `componium node` | run a software instrument node |
| `componium studio` | edit a score in a browser |


## What it is not

- Not a media player. Componium follows a player; it does not replace one.
- Not a 3D video system. Stereoscopic 3D is the display's job.
- Not a lighting protocol. Componium speaks sACN/Art-Net and inherits the
  existing DMX ecosystem rather than competing with it.

## Vocabulary

| Term | Meaning |
|---|---|
| **score** | Timeline of cues and curves bound to one piece of media |
| **instrument** | A driver for one physical device |
| **conductor** | Runtime that keeps every instrument locked to playback timecode |
| **cue** | One timed, discrete event |
| **curve** | A continuous, sampled channel (sway, wind speed, colour) |
| **pit** | The hub instruments register with |
| **rehearse** | Dry run with all hardware stubbed out |

## The three hard problems

1. **Clock.** Media players report position by polling, at roughly 1 Hz and
   with jitter. The conductor runs a filter over those samples to maintain a
   sub-frame media clock between them, and resyncs on seek and pause.
2. **Latency compensation.** A DMX fixture responds in ~20 ms; a fan takes
   800–2000 ms to reach speed; a fogger has 1–3 s of lag. Every instrument
   declares its own latency and the conductor dispatches each cue early by
   exactly that much. This is first-class in the protocol, not an afterthought.
3. **Safety.** Instruments declare a safe state and duty-cycle limits. The
   conductor heartbeats at 10 Hz; any instrument that goes 300 ms without one
   fails safe on its own, independently of the conductor.

## Score sketch

```toml
[score]
componium = "0.1"
title = "Dune"
media = { duration = "2:35:12", hash = "sha256:…" }

[[track]]
instrument = "light.ambient"
type = "curve"
interpolation = "linear"
points = [
  { t = "00:12:04.000", value = { r = 0,   g = 0,   b = 0  } },
  { t = "00:12:06.500", value = { r = 255, g = 180, b = 90 } },
]

[[track]]
instrument = "wind.main"
type = "cue"
cues = [
  { t = "01:04:22.100", action = "gust", intensity = 0.8, duration = "4s" },
]
```

Binding by content hash and duration means a score follows the film, not a
filename.

## Composing scores automatically

Hand-authoring a score takes hours per film. The **composer** is an offline
pipeline that analyses a film — its video, its soundtrack and its subtitle
track — and proposes a complete score for a human to refine.

It is deliberately offline and slow. It never runs during playback; it emits a
score file, and the conductor knows nothing about how that file was made. The
cheapest signals are also the most effective: LFE sub-bass energy maps almost
directly onto shake, per-frame brightness onto ambient light, and SDH subtitle
tracks already carry timestamped semantic labels like `[thunder rumbles]`.

Generated output passes through a limiter that enforces each instrument's
declared limits before it is playable, and a human reviews it in the studio.
AI-assisted, not AI-automatic — the review step is a safety control.

See [LOGBOOK/features/feat-composer.md](LOGBOOK/features/feat-composer.md).

## Documentation

- [ROADMAP.md](ROADMAP.md) — milestones and their ordering rationale
- [CONTRIBUTING.md](CONTRIBUTING.md) — how to help, and the CLA
- [docs/cip.md](docs/cip.md) — Componium Instrument Protocol
- [docs/hardware.md](docs/hardware.md) — what to buy, in what order, and why
- [docs/wet-and-hot.md](docs/wet-and-hot.md) — fog, water, heat and moving mass
- [docs/adr/](docs/adr/) — architecture decisions and why

## Licence

Componium is licensed under the **GNU Affero General Public License v3.0**.
You may use, study, modify and redistribute it, including over a network, on
the condition that derived works are released under the same licence.

The **specification** in [docs/cip.md](docs/cip.md) is separately dedicated to
the public domain under CC0 1.0. Implementing a CIP instrument does not make
your instrument a derived work of Componium — write one in any language, under
any licence you like.

Commercial licences that lift the AGPL's obligations are available separately.

Contributions require a signed agreement before they can be merged; see
[CLA/README.md](CLA/README.md) for why and what it grants.

## Name

After the Componium, a music automaton built by Diederich Nicolaus Winkel in
Amsterdam in 1821 that generated endless variations from a single mechanism.
It is in the Musical Instruments Museum in Brussels.
