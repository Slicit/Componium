# Stage one: what the frame pass responds to

Four experiments against `feat-two-clocks` stage 1. Sintel, 444 frames every
two seconds, plus crab-rave's 96 as a film with verified ground truth. All on
Qwen2.5-VL-7B-Instruct-AWQ. Floor from the same-prompt-twice control: EFFECTS
0.2%, SCENE 0.5%.

Judged without external labels by setting the model's own sentence against its
own label. It labels smoke or dust and then describes snow or mist and nothing
burning — one of the two is wrong, and it is not the sentence, which is written
last and keeps contradicting the label.

    suspect   labelled smoke/dust, sentence says snow/mist/fog/haze/steam
              and nothing about smoke, dust, ash or debris
    agreed    labelled smoke/dust, sentence says smoke/dust/ash/debris

## Absorbing categories: falsified

The theory was that snow-becomes-dust is a missing correct answer rather than
carelessness — the model sees pale airborne matter, has seven buckets, and none
of them is snow. So the near-misses were named as explicitly not effects.

| sintel | before | absorbers |
|---|---|---|
| suspect | 8 | 8 |
| agreed | 23 | 20 |
| any effect | 54 | 62 |

No change to the false positives, fewer true ones, and more effects overall.
The additions it made are plainly wrong:

    a person swinging on a vine through a dense bamboo forest -> +rain +splash
    a dimly lit cave with glowing crystals                    -> +fire

That is the third time: emphasising `splash` produced splashes, allowing
reasoning produced dust on snow, and naming things as **not** effects produced
more effects. **Any addition to this prompt increases label production,
whatever the addition says.**

## The prompt is already at a good operating point

If additions increase production, what do removals do? And is suppression a
different lever from length? Both are testable.

| sintel | any effect | suspect | agreed | agreed/suspect |
|---|---|---|---|---|
| shipped | 56 | 8 | 23 | **2.9** |
| the seven definitions removed | 155 | 23 | 45 | 2.0 |
| a base-rate line added | 33 | 4 | 10 | 2.5 |

Removing the definitions nearly **triples** production, so the negations inside
them — "Not fog, mist, haze or low cloud", "Not a fire that is merely burning"
— are the suppressor doing all the work. Adding a base-rate line cuts
indiscriminately: crab-rave's dust, verified at ten out of ten, halves.

The shipped wording has the best precision of the three. **Prompt text is spent
as a lever, in both directions.**

## Two frames: the one that worked

Everything that keeps failing is a temporal judgement made from a still. Dust
is thrown and snow is settled. A splash needs water that is moving. Activity is
motion. No wording will get that out of one frame, which is why every wording
has failed the same way.

So: the same prompt, the same frame, and the frame one second before it, with a
note saying which is which and why the earlier one is there.

First attempt ended that note with "If nothing has changed between them,
nothing is happening", which is a suppressor in its own right and cannot be
told apart from the second frame by one run. It over-suppressed: SCENE changes
114 -> 14, but active collapsed to 8 frames of 444, which for a film with a
dragon fight is silence rather than steadiness.

Without that sentence:

| | one frame | two frames |
|---|---|---|
| sintel SCENE changes | 114 | **40** |
| sintel active | 99 | 25 |
| sintel any effect | 54 | 56 |
| sintel suspect / agreed | 8 / 22 | 9 / 21 |
| crab-rave SCENE changes | 22 | **18** |
| crab-rave agreed | 8 | **9** |
| cost | — | **x1.4** |

SCENE is 65% steadier. EFFECTS is unmoved — 54 to 56 on 444 frames, suspects
and agreed within a frame or two of each other. Crab-rave's true dust goes up
by one.

That is the shape stage 1 wanted and never got from wording: it fixes the
temporal question and leaves the evidence question alone. And 1.4x rather than
the 2x a second image suggests, because the prompt is most of the tokens.

## What this settles

**Evidence, not instruction.** Three attempts to fix detection by telling the
model things made it worse. One attempt to fix it by showing the model more
worked. The frame pass does not need a better prompt; it needed a second frame.

**The calm baseline moves.** `calm.py` documents 113 changes across fifteen
minutes with a median run of four seconds, and smooths over fourteen seconds to
cope. Forty changes is a different signal, and the smoothing window is worth
revisiting once the pipeline actually sends pairs.

**A negation is worth more than a rule.** The seven definitions suppress by
saying what each word is *not*. That is the only prompt device here that has
ever been shown to work, and it is the pattern to reach for before any other.

## Trace files

- `absorbers-sintel-crab--qwen2-5-vl-7b-awq.jsonl`
- `suppressors-sintel-crab--qwen2-5-vl-7b-awq.jsonl`
- `twoframes-sintel-crab--qwen2-5-vl-7b-awq.jsonl` (with the suppressive line)
- `twoframes-nosuppress--qwen2-5-vl-7b-awq.jsonl` (the one that counts)

Harness under `hack/`: `prompt-trial.py`, `prompt-control.py`,
`prompt-isolate.py`, `absorbers.py`, `suppressors.py`, `twoframes.py`.
