# Stage two: the scene pass, and what it is actually for

Measured, and the answer changed what stage two should be. The plan had a
second pass owning "where we are, what is happening, and how busy it is". It
should own the first two and must not own the third.

Sintel and the Rebel Moon battle cut, Qwen2.5-VL-7B-AWQ.

## The measure

Stage one moved the baseline — two frames took SCENE from 114 changes across
sintel to 40 — so "steadier" was no longer a test, and at a fifth of the rate
fewer changes is true whatever the answers are.

So: agreement with evidence the model never sees. `calm.py` already blends its
judgement with the low frequency audio and the camera's own speed. If a pass
tracks those better, it is reading the film; if worse, it is guessing more
comfortably. The number below is how much louder and faster the film is where a
pass says "active" — one means it is saying nothing the evidence agrees with.

| sintel | calls | time | changes | agreement |
|---|---|---|---|---|
| frame pass, every 2s | 444 | 76s | 92 | **2.68** |
| the same, held over 10s windows | 444 | 76s | 14 | **2.64** |
| scene pass, every 10s | 88 | 13s | 23 | 1.68 |

| rebel moon cut | calls | time | changes | agreement |
|---|---|---|---|---|
| frame pass, every 2s | 450 | 94s | 136 | 1.59 |
| the same, held over 10s windows | 450 | 94s | 24 | **1.94** |
| scene pass, every 10s | 89 | 16s | 31 | 1.44 |

The held row is the control that matters. A signal held for ten seconds cannot
track evidence moving faster than that, so some of the frame pass's advantage
could have been resolution rather than judgement. It is not: holding its own
answers over the same windows costs nothing on sintel and *improves* the battle
cut, while the scene pass stays well below both.

**The scene pass judges activity worse, at equal resolution.** It does not get
to own BUSY.

## What it is for instead

    5.0s   A snowy landscape        A slow-moving object
    35.0s  A battlefield            A fight
    45.0s  A village                A conversation

Place and situation are the things nothing in the pipeline currently owns, they
look stable and plausible, and they cost a fifth of the frame pass — about 17%
on top of it. They are what scent needs (a burning village for four minutes, not
fire in this frame) and what dynamics needs to buff rather than only quiet.

## And a design that was already right

The obvious action from the table is to hold the frame pass's activity over a
window before using it. `calm.py` already does: a centred fourteen second
rolling mean over the blended score, which is the same thing done better —
centred rather than trailing, and continuous rather than binary.

So there is nothing to change here. The measurement validates the existing
design rather than replacing it, which is worth recording precisely because the
temptation was to add a second mechanism beside it.

## What this means for the plan

Stage two is **not built**, deliberately. A pass producing place and situation
with nothing consuming them is machinery waiting for a purpose, and the two
things that would consume them are stages four and five. It should be built
with scent, where it has a consumer and can be judged by whether the scent it
chooses is right.

The activity half of stage two is cancelled outright: the frame pass with two
frames, smoothed by calm as it already is, is the better signal and it is
already in place.

## Trace

Harness: `hack/scenepass.py`. No trace file — the numbers above are the result,
and the scene prompt lives in the harness for whoever builds stage four.
