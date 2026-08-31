"""Which half of the change leaked?

Two things went in together: SCENE was allowed to judge, and a LIKELY line was
added for whatever was inferred. EFFECTS then moved on 16% of frames against a
0.2% floor, mostly by calling snow and fog "dust". Reverting both without
knowing which did it would throw away whichever one was innocent.

    likely-only   the old prompt, plus a LIKELY line, with the no-inference
                  rule scoped to the three lines that still must obey it
    context-only  the old prompt, plus a line about what the film is

Same frames as the trial, so everything is comparable to it.
"""

import glob
import importlib.util
import os
import sys

here = os.path.dirname(os.path.abspath(__file__))
sys.argv = [sys.argv[0]]
spec = importlib.util.spec_from_file_location("trial", os.path.join(here, "prompt-trial.py"))
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)

old = trial.load("/tmp/vlm-old.py", "vlm_old")
new = trial.load(os.path.join(here, "vlm-label.py"), "vlm_new")

BASE = old.PROMPT

# The old prompt with an inference line bolted on, and nothing else changed
# except scoping the rule it would otherwise contradict.
LIKELY_ONLY = BASE.replace(
    "Report only what is plainly visible IN THIS FRAME. You are not describing the\n"
    "story, guessing what happens next, or inferring from context.",
    "For EFFECTS, WATER and SCENE, report only what is plainly visible IN THIS\n"
    "FRAME. Do not describe the story, guess what happens next, or infer from\n"
    "context. Anything you infer belongs on the LIKELY line and nowhere else."
).replace(
    "Reply with exactly four lines", "Reply with exactly five lines"
).replace(
    "SEEN: <one plain sentence describing what is in the frame and what is\n"
    "       happening, as you would tell someone who cannot see it>",
    "SEEN: <one plain sentence describing what is in the frame and what is\n"
    "       happening, as you would tell someone who cannot see it>\n"
    "LIKELY: <what this frame appears to be part of, in a few words, or the word\n"
    "         none if it suggests nothing beyond itself>"
) + """

LIKELY is where anything you have inferred belongs — a battle, a chase, a
funeral. It drives nothing at all."""

CONTEXT = ("Animated fantasy short. A woman searches for a dragon she raised; "
           "snow, caves, a village, one fight.")
CONTEXT_ONLY = BASE + """

About this film, from the person who set it up:

  %s

That is background for the SEEN line only. It is not evidence. EFFECTS, WATER
and SCENE are about this frame and nothing else.""" % CONTEXT

images = sorted(glob.glob("/tmp/trial/*.jpg"))
print("%d frames" % len(images))
print()

base_replies, _ = trial.run(BASE, images)
base_labels = [new.parse(r or "") for r in base_replies]
base_changes, _r, base_states = trial.scene_runs(base_labels)
print("%-14s SCENE: %3d changes, %3d active" % ("before", base_changes, base_states.count("active")))

for name, prompt in [("likely-only", LIKELY_ONLY), ("context-only", CONTEXT_ONLY)]:
    replies, took = trial.run(prompt, images)
    labels = [new.parse(r or "") for r in replies]
    changes, _r, states = trial.scene_runs(labels)
    moved = sum(1 for a, b in zip(base_labels, labels)
                if trial.effects_of(a) != trial.effects_of(b))
    scene = sum(1 for a, b in zip(base_states, states) if a != b)
    print("%-14s SCENE: %3d changes, %3d active   EFFECTS moved %d (%.1f%%), SCENE moved %d (%.1f%%)"
          % (name, changes, states.count("active"),
             moved, 100 * moved / len(images), scene, 100 * scene / len(images)))
    if name == "likely-only":
        guesses = [new.inferred(r or "") for r in replies]
        print("               LIKELY offered on %d frames" % len([g for g in guesses if g]))

print()
print("floor from the control: EFFECTS 0.2%, SCENE 0.5%")
