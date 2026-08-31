---
status: active
branch: feat-two-clocks
---

# feat-two-clocks · transient evidence and persistent judgement, asked apart

Three complaints arrived together and turned out to be one shape. The
descriptions read the same for every film. Wind blows on any movement. Scent is
a number with two values. Each was going to get its own patch until the
measurements said they are the same problem: the pipeline asks one question, of
one frame, at one rate, and then uses the answer for everything.

## The finding this is built on

Trying to let the model reason produced a result worth more than the feature
was. Measured on 444 frames of sintel, Qwen2.5-VL-7B-AWQ, full detail in
`LOGBOOK/experiments/README-scene-steadiness.md`.

The floor first, because nothing else means anything without it. vLLM batches
concurrent requests and the batch a request lands in changes the arithmetic, so
temperature zero is not deterministic. The same prompt asked twice:

    EFFECTS differ on 0.2% of frames
    SCENE   differs on 0.5%

Against that floor:

| prompt change | EFFECTS moved | vs floor |
|---|---|---|
| a LIKELY line for what it inferred | 6.1% | 30x |
| a line of film context | 6.8% | 34x |
| both | 16.0% | 80x |

Every one of those variants told the model, in the prompt, that the new
material was not evidence for EFFECTS. All of them moved EFFECTS anyway, and
in one direction: across the 71 frames that changed it gained `dust` 44 times.
A figure in a snowy landscape became dust. A person in fog became smoke, in a
prompt that says smoke is "not fog, mist, haze or low cloud".

**A rule in a prompt is a wish, not a mechanism.** Attention cannot be
partitioned by instruction. It can be partitioned by process, and that is the
whole of this plan.

## Two clocks

The questions being asked are not one kind, and separating them by *call* is
the only separation that has been shown to hold.

| | transient | persistent |
|---|---|---|
| asks | is this in the frame | what is this scene |
| answers | flash, splash, dust, smoke, blast | calm or active, where we are, what it smells of |
| evidence | the pixels, and nothing else | the pixels, plus what the film is |
| rate | every 2s | every 10s or per shot |
| wrong answer costs | a fogger fires in a quiet room | a scene reads slightly wrong |

Everything that has gone wrong sits on that line. Scent lingers for minutes and
was being driven by a frame. Wind wants a chase and was being driven by two
frames of pixels. SCENE is a judgement about a moment and was being asked of a
still, which is why it changes its mind 113 times in fifteen minutes.

## Where this stands

| stage | state |
|---|---|
| 1 · frame pass | **done**, and it was two frames that did it. Absorbing categories were falsified; so was every other wording. See `experiments/README-stage-one.md`. |
| 2 · scene pass | **not built, deliberately.** Its activity half is cancelled — measured worse than the frame pass at equal resolution. Its place-and-situation half is worth having and belongs with stage 4, where something consumes it. See `experiments/README-stage-two.md`. |
| 3 · wind | **done.** Expansion rather than translation, on an absolute per-second scale. |
| 4 · scent | not started. Needs the scene pass built alongside it. |
| 5 · dynamics | not started. Same. |

## Stage 1 · the frame pass keeps its job and gets better at it

Unchanged: closed vocabulary, evidence only, no context, 512px keyframes, every
two seconds. That prompt is the one thing here with a measured reason to be
left alone.

**Absorbing categories.** The snow-becomes-dust failure is not carelessness, it
is a missing correct answer: the model saw airborne white particulate and had
seven buckets, none of which was snow. So name the near-misses as explicitly
*not* effects — snow, fog, mist, haze, steam, sand, sparks, dappled light — and
let it choose one and stop. This is the extensive vocabulary idea pointed at
the failure that is actually happening, rather than at more effect words, which
the splash result says would make it worse.

**Two frames instead of one.** Every remaining failure is a temporal judgement
made from a still. Dust is thrown and snow is settled; a splash requires water
that is moving; activity is motion. One frame cannot answer any of those, and
no wording will make it. Two frames a second apart is the only change that
addresses the mechanism rather than the symptom, and it addresses detection and
activity at once. Roughly double the tokens per call, which a coarser cadence
could pay for.

*Measured by*: the control run, then the label diff. `dust` false positives
down, true `dust` held. Nothing lands without both numbers.

## Stage 2 · a scene pass, which is where context belongs

A second call at a fifth of the rate, allowed everything the frame pass is
denied: the film's context, the previous scene's answer, room to reason.

It produces what nothing currently owns — where we are, what is happening, and
how busy it is — and it is the only consumer of the context box. That box
shipped believing it was inert; it is not, it moves labels ~7%, and moving it
here is what makes it safe as well as useful.

*Measured by*: the recorded baseline in `calm.py` — 113 changes across fifteen
minutes of sintel, median run four seconds. A scene pass that does not beat
that is not a scene pass, it is the same guess more expensively.

## Stage 3 · wind is driven by the wrong quantity

Not merely over-sensitive. `Movement` is a single global translation and
`wind_series` takes its magnitude, so a **pan across a static room** is maximal,
and a **forward dolly** — driving, running, flying, the one case where air
actually rushes past — expands about the centre, cancels, and reads near zero.
It is backwards.

1. **Expansion, not translation.** Match the halves of the frame separately: a
   pan moves both the same way, forward travel moves them apart. That single
   discriminator separates "the camera swung" from "we are moving through air",
   and it reuses the block matcher already there.
2. **An absolute floor, not the film's own peak.** Peak normalisation
   guarantees wind is never absent; a talky film's mildest pan becomes full
   scale.
3. **Sustained, not continuous.** A fan takes over a second to spin up — the
   code says so. Wind wants chases and storms, which the scene pass knows about
   and a curve does not.
4. **Blasts are events**, and belong to stage 1, not to the motion curve.

*Measured by*: hand-marked minutes. A driving sequence must out-blow a
dialogue scene shot with a moving camera. Today it does the reverse.

## Stage 4 · scent is named, banked, and rationed

Today: `params: {channel: 1.0}`, two values. The pattern that fits is the one
the codebase already uses everywhere else — **the score names, the rig
resolves**, exactly as `light.ambient` is a name the rig maps to hardware.

    [instrument.scents]
    1 = "smoke"
    2 = "petrichor"
    3 = "sea"

A rig without that scent does not fire it, the way a rig without an instrument
does not. Five, ten or fifteen reservoirs is then a longer table and no code.

A bank that is buyable as diffuser oil, strongly distinguishable, and actually
cued by films:

**smoke · petrichor · sea salt · pine · cut grass · earth · gunpowder ·
ozone · floral · citrus · coffee · baked bread · leather · incense · spirits**

The first eight carry most films.

**What makes scent structurally different is that it lingers.** Light is
instant, wind decays in seconds, and a puff hangs in a room for minutes — and
unlike every other effect it cannot be taken back. Two scents inside a minute
are not two events, they are mud. So scent needs a refractory measured in
minutes rather than the seconds the budget uses, it wants one scent per scene
rather than one per event, and its driver is therefore stage 2 and never stage
1. A frame pass can say there is fire in this frame; scent needs to know this
is a burning village for the next four minutes.

*Measured by*: puffs per hour, and whether two ever land close enough to mix.

## Stage 5 · dynamics that buff as well as nerf

`calm.py` ranks and spends a budget in seconds, which is why it survived a
miscalibrated signal, and it only ever quiets. Giving an action sequence *more*
than baseline does not exist. That is the natural home for stage 2: a scene the
scene pass calls a battle is where budget should be spent, not merely where it
should not be cut.

## The order, and why it is this one

1. **Absorbing categories** — one prompt change, the harness exists, minutes to
   run. Tests the vocabulary instinct directly and cheaply.
2. **Two frames** — the mechanism fix. Everything downstream inherits it, so it
   comes before anything that consumes labels.
3. **Wind** — independent of the model entirely, so it can proceed in parallel
   and cannot be confounded by the passes above.
4. **The scene pass** — worth building only once stage 1 is settled, because it
   multiplies whatever the frame pass gets wrong.
5. **Scent and dynamics** — both consume stage 2 and neither can be judged
   before it exists.

## What lets any of this land

The discipline is the deliverable as much as the design.

**Control first, then the diff.** Same prompt twice, over the same frames, to
find the floor. Only then compare. A 6% move is invisible when reading
sentences and is forty extra fogger bursts across a feature.

**A number to beat, written down before the change.** The 113/4s baseline and
the 0.2% floor exist; anything new gets its own before it is built.

**Reverting is a result.** The scene-as-judgement change was reverted the same
hour it was measured, and the finding it produced is what this plan rests on.

## Related

- `LOGBOOK/experiments/README-scene-steadiness.md` — the measurement above
- `LOGBOOK/experiments/README.md` — the dust trace, and why 512px keyframes
- `feat-rest.md` — the amplitude and density work this leaves untouched
- `composer/calm.py` — the ranked budget stages 2 and 5 depend on
