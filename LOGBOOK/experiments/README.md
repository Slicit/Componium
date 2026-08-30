# Experiments

Traces of what a model said, kept so the next model is a diff rather than a
memory. Only one can be running at a time on the machine that has the GPU, so
comparing them any other way means remembering, and remembering is what this
directory exists to replace.

Each trace is JSON lines: a header saying what was asked and by what, then one
row per frame with its time, its labels and its sentence. Keyed by time, so two
models are compared frame for frame — which is where the interesting
disagreements are, not in the totals.

## dust-crab-rave

Whether the crabs throwing up sand reach the fogger.

Reported as: the score has no fog events, but there is clearly dust in the
film. It turned out to be true, and the cause was not the one that looked
likely.

`dust-crab-rave--qwen2-5-vl-7b-instruct-awq.jsonl` — 96 frames, one every two
seconds, 14 seconds of GPU. Found dust or smoke in eight of them.

### What it settled

**Not sampling.** The pipeline had shown the model the right frame. Its
description of 00:59.47 was "A group of red crabs walks across a sandy beach",
labelled calm, on the very frame where the sand cloud is unmistakable.

**Not consistency.** Asked ten times at 512 wide, that frame came back `dust`
ten times out of ten. Asked ten times at full resolution, `none` ten times out
of ten. Deterministic both ways.

**The size of the frame.** Everything measured during development downscaled to
512 wide; the pipeline sent the film's own resolution, because `keyframe` had
no scale filter. Across six moments the two disagreed on three, and disagreed
the wrong way round — full resolution saw less. It also cost five times more to
see less: 3091 prompt tokens against 580.

The likely reason is that a large image becomes many patches and a diffuse,
low-contrast thing like a dust cloud is spread thin across them, where
downscaled it occupies enough of one patch to register. That is a guess; the
measurement is not.

### After the fix

Crab rave, analysed through the studio, produced its first fog cue:

    { t = "00:00:59.467", action = "burst", params = { output = 0.7 },
      duration = "3.000s", source = "vision: dust" }

at exactly the moment in question.

### Worth repeating with another model

The frame-size effect is the thing to check first on qwen3, because if it does
not have it then 512 is a limitation being worked around rather than a setting.
The trace has every frame, so the diff will say.
