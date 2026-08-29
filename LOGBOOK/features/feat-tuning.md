---
status: draft
branch: feat-tuning
---

# Tuning · per machine and per player calibration

## Intent

The clock spike showed that timing quality depends entirely on what is at the
other end: mpv resolves to 41 ms and answers in 53 us, VLC resolves to 247 ms
and answers in 21 ms. Those numbers are properties of a particular player, on a
particular machine, under a particular load. Hard coding any of them would be
wrong everywhere except the machine they were measured on.

So measure them, on startup, on the actual rig, and let the conductor
compensate from real numbers instead of assumptions.

## The distinction the whole feature rests on

**Bias is compensatable. Variance is not.**

If a fan consistently takes 1200 ms to spin up, dispatch its cue 1200 ms early
and the effect lands correctly. If it takes somewhere between 800 and 2000 ms
depending on temperature, no amount of measurement fixes that, and the best the
conductor can do is know the error bound and report it honestly.

Every measurement below is therefore reported as a pair: a mean, which is
subtracted, and a spread, which becomes part of the precision estimate and is
surfaced rather than hidden.

## What can be measured automatically

1. **Host timer quality.** How late does a 5 ms ticker actually fire? A Pi
   under load and an idle desktop differ by an order of magnitude, and this is
   the floor on dispatch accuracy no matter how good the clock is.
2. **TimeSource characterisation.** Query round trip, position update period,
   rate stability. This is exactly what `spikes/clock-jitter` already does; the
   spike graduates into this command rather than being thrown away.
3. **Network path per instrument.** Round trip and jitter to each registered
   instrument. Wired and Wi-Fi differ enough to matter, and a Wi-Fi instrument
   that looks fine at startup is worth flagging as a risk.

## What cannot, and must not be guessed

**Physical actuation latency.** A fogger's heat and burst lag, a fan's spin up,
a platform's travel time. Nothing in software can observe these without a
sensor. They stay declared in the instrument manifest.

A later feature can offer assisted calibration: fire a cue, have a human tap a
key when they perceive the effect, repeat, take the median. Crude, better than a
guess, and it needs a human in the loop by definition. A microphone or
photodiode could automate it for effects that make noise or light.

## Plan

1. `componium tune` measures the three automatic items above and writes a
   tuning profile.
2. Profile is keyed by machine, player and player version, and cached. Startup
   must not cost the user thirty seconds of staring at a black screen before a
   film begins.
3. **Frame rate is not part of the profile.** mpv's quantisation is one frame
   interval, so precision depends on the film, not the machine. It is derived
   per score from the media, not measured.
4. Continuous refinement during playback. The conductor already receives a
   stream of clock anchors and can compute residuals live, so the running
   precision estimate is free. The startup benchmark exists to have a number
   before the first cue fires and to refuse a configuration that cannot work
   at all, not to be the only source of truth.
5. `componium doctor` prints the profile in human terms, so that "why is my fog
   always late" has an answer that is not guesswork.

## Decisions

### 2026-08-29

- **Decision:** Calibrate on startup, then keep refining during playback, rather
  than trusting a single startup measurement.
- **Why:** Every number in the clock spike was taken on an idle machine with
  null video and audio output. A real session decodes video, drives a display,
  and loads the scheduler. A benchmark taken before playback is measuring a
  machine that is not the machine that will be running the show.
- **Impact:** The precision field on `TimeSource` is a live value, not a
  constant read from a file at startup.

## Links

- Branch: `feat-tuning`
- PR: TBD
- Related ideas: `LOGBOOK/ideas.md` 2026-08-29
- Related features: `feat-timing-core` produces the measurements this consumes
- External: none
