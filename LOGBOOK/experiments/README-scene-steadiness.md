# Asking SCENE as a judgement

Reverted. It failed the test it was committed with, and the way it failed says
something worth keeping.

`scene-steadiness-sintel--qwen2-5-vl-7b-awq.jsonl` — 444 frames of sintel, one
every two seconds, Qwen2.5-VL-7B-Instruct-AWQ.

## What was tried

The prompt asked four things of one frame and forbade inference outright.
Two of those answers are evidence — EFFECTS and WATER fire foggers and misters
— and two are not: "how much is happening to the audience" is a judgement about
the moment a frame came from, and one still is a poor sample of movement.

SCENE is half of what decides where a film is quieted, and `calm.py` records
what starving it costs: 113 changes of mind across fifteen minutes of sintel, a
median run of four seconds, "most of that not the film changing but one frame
of a shot looking unlike the next".

So SCENE was allowed to reason, a LIKELY line was added for whatever it
inferred, and the prompt was told twice over that none of this was evidence for
EFFECTS or WATER.

## What happened

The floor first, because without it none of the rest means anything. vLLM
batches concurrent requests and the batch a request lands in changes the
arithmetic, so temperature zero is not the same as deterministic. The same
prompt asked twice over the same 444 frames:

    EFFECTS differ on 0.2% of frames
    SCENE   differs on 0.5%

Against that:

| change | EFFECTS moved | vs floor |
|---|---|---|
| the LIKELY line alone | 6.1% | 30x |
| a line of film context alone | 6.8% | 34x |
| both together | 16.0% | 80x |

And the direction is the thing. Across the 71 frames that moved, the new prompt
gained `dust` 44 times, `splash` 7, `fire` 6, `smoke` 3, `rain` 3 — against 22
lost. The examples are unambiguous:

    a figure in a fur outfit stands in a snowy landscape    -> dust
    a figure in dark clothing sits amidst icy ruins         -> dust
    a person stands in a foggy environment                  -> smoke

Snow is not dust and the prompt says in as many words that smoke is "not fog,
mist, haze or low cloud". This is the splash result again: give the model room
and it reaches for the vocabulary.

Meanwhile the thing it was for barely moved. SCENE went from 116 changes to
100, median run unchanged at four seconds, against a floor of plus or minus
two. LIKELY was offered on 9 frames of 444, and said "battle" on most of them.

## What it means

**The wall cannot be built out of instructions.** Every variant here told the
model, explicitly, that the new material was not evidence for EFFECTS. All of
them moved EFFECTS by more than thirty times the noise floor. The model does
not partition its attention along the lines a prompt draws.

**Any prompt edit needs this measurement.** Not "does it look better" — the
same-prompt-twice control, then the label diff. A 6% move reads as nothing when
you are looking at sentences and is forty extra fogger bursts across a feature.

**Context is not free**, and it shipped believing it was. It is opt in and
empty by default, so a film with nothing said about it is unaffected, but a
film with a line of context has its labels moved by about 7% in a direction
nobody has measured for quality.

## What might work instead

Separate calls rather than separate lines. If EFFECTS must be stable and SCENE
wants context, one request cannot serve both — but two can, each with a prompt
tuned for its own job and no shared attention to contaminate.

It need not double the cost. Activity is a property of a scene rather than a
frame, so the judgement pass could run at a fifth of the cadence — every ten
seconds against every two — which is both cheaper and probably steadier, since
most of the 113 changes were one frame of a shot looking unlike the next.

Not tried. Written down because the measurement above is what makes it worth
trying, and because the next person to reach for the prompt should read this
first.
