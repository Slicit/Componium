"""How much does the same prompt disagree with itself?

The trial said EFFECTS moved on 16% of frames, which by the test set out would
condemn the change. That number only means something against a floor: vLLM
batches concurrent requests and the batch a request lands in can change the
arithmetic, so temperature zero is not the same as deterministic. If the same
prompt asked twice also differs on 16%, the trial measured the server rather
than the prompt.

Same frames, same prompt, twice.
"""

import importlib.util
import os
import sys

sys.argv = [sys.argv[0]]
here = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("trial", os.path.join(here, "prompt-trial.py"))
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)

old = trial.load("/tmp/vlm-old.py", "vlm_old")
new = trial.load(os.path.join(here, "vlm-label.py"), "vlm_new")

images = sorted(__import__("glob").glob("/tmp/trial/*.jpg"))
print("%d frames, the same prompt asked twice" % len(images))
print()

runs = []
for n in (1, 2):
    replies, took = trial.run(old.PROMPT, images)
    labels = [new.parse(r or "") for r in replies]
    changes, _runs, states = trial.scene_runs(labels)
    runs.append((labels, states))
    print("pass %d  %5.0fs   SCENE: %3d changes, %3d of %d active"
          % (n, took, changes, states.count("active"), len(states)))

(a_labels, a_states), (b_labels, b_states) = runs
moved = sum(1 for a, b in zip(a_labels, b_labels)
            if trial.effects_of(a) != trial.effects_of(b))
scene = sum(1 for a, b in zip(a_states, b_states) if a != b)

print()
print("the same prompt, disagreeing with itself:")
print("   EFFECTS differ on %d of %d frames (%.1f%%)"
      % (moved, len(images), 100 * moved / len(images)))
print("   SCENE   differs on %d of %d frames (%.1f%%)"
      % (scene, len(images), 100 * scene / len(images)))
print()
print("That is the floor. Anything the prompt change moved has to beat it.")
