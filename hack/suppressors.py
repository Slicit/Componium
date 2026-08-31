"""If adding text makes it reach, what does taking text away do?

Three experiments now agree that any addition to this prompt increases label
production, whatever the addition says. Emphasising splash produced splashes,
allowing reasoning produced dust on snow, and naming the near-misses as not
effects produced rain and splash in a bamboo forest.

That points at two different levers, and they are distinguishable.

    shorter     the same prompt with the seven effect definitions cut to seven
                words. If length is what drives production, this reduces it.

    base-rate   the same prompt plus one line telling it what the usual answer
                is. SCENE already has exactly that — "most frames of most films
                are calm" — and SCENE is the one line that behaves. EFFECTS has
                no such statement. If suppression is the lever, this reduces it
                even though it is an addition.

Same judgement as the absorber run: suspects down, agreed held, and a collapse
in the total is a prompt that stopped answering rather than one that got it
right.
"""

import importlib.util
import json
import os
import re
import sys
import time

here = os.path.dirname(os.path.abspath(__file__))
sys.argv = [sys.argv[0]]
spec = importlib.util.spec_from_file_location("trial", os.path.join(here, "prompt-trial.py"))
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)
vlm = trial.load(os.path.join(here, "vlm-label.py"), "vlm_now")

DEFINITIONS = """  explosion  a fireball or blast going off. Not a fire that is merely burning.
  lightning  a lightning bolt, or a flash so bright it lights the whole scene.
  fire       visible flames. Not smoke alone, not glowing embers.
  smoke      a plume or cloud of smoke. Not fog, mist, haze or low cloud.
  dust       a burst of dust or debris thrown into the air by an impact.
  splash     water thrown into the air: spray, a breaking wave, something
             hitting water. The water must be moving.
  rain       rain visibly falling."""
BARE = "  explosion, lightning, fire, smoke, dust, splash, rain"

SHORTER = vlm.PROMPT.replace(DEFINITIONS, BARE, 1)
assert SHORTER != vlm.PROMPT, "the definitions moved"

BASE_RATE = vlm.PROMPT.replace(
    "If none of those are plainly visible, answer exactly: EFFECTS: none",
    "Most frames of most films contain none of these. EFFECTS: none is the\n"
    "ordinary answer, and the one to give unless something on the list is\n"
    "plainly there.", 1)
assert BASE_RATE != vlm.PROMPT, "the none-anchor moved"

NEARLY = re.compile(r"\b(snow|snowy|mist|misty|fog|foggy|haze|hazy|steam|"
                    r"cloud|powder|white substance)\b", re.I)
REALLY = re.compile(r"\b(smoke|smoky|dust|dusty|ash|debris|rubble|soot|"
                    r"explosion|blast|burning|fire)\b", re.I)

FILMS = [("sintel", "/home/claude/componium-media/sintel.mp4"),
         ("crab-rave", "/home/claude/componium-media/noisestorm-crab-rave_138410.mp4")]


def score(labels, seens):
    suspect = agreed = flagged = 0
    for ls, said in zip(labels, seens):
        if "smoke" in ls or "dust" in ls:
            flagged += 1
            if REALLY.search(said or ""):
                agreed += 1
            elif NEARLY.search(said or ""):
                suspect += 1
    return flagged, suspect, agreed


out = {}
for name, path in FILMS:
    images = trial.frames(path, "/tmp/suppress", 0)
    print("%s: %d frames" % (name, len(images)))
    out[name] = {}
    for cond, prompt in (("before", vlm.PROMPT), ("shorter", SHORTER),
                         ("base-rate", BASE_RATE)):
        replies, took = trial.run(prompt, images)
        labels = [vlm.parse(r or "") for r in replies]
        seens = [vlm.described(r or "") for r in replies]
        flagged, suspect, agreed = score(labels, seens)
        effects = sum(1 for ls in labels
                      if any(l for l in ls if not l.startswith("scene-") and l != "water"))
        active = sum(1 for ls in labels if "scene-active" in ls)
        out[name][cond] = {"frames": len(images), "smoke_or_dust": flagged,
                           "suspect": suspect, "agreed": agreed,
                           "any_effect": effects, "active": active,
                           "labels": labels, "seens": seens}
        print("   %-10s smoke/dust %3d  suspect %3d  agreed %3d   any effect %3d   active %3d"
              % (cond, flagged, suspect, agreed, effects, active))
    print()

print("=" * 70)
for name, cs in out.items():
    b = cs["before"]
    print(name)
    for cond in ("shorter", "base-rate"):
        c = cs[cond]
        print("   %-10s suspect %2d -> %2d   agreed %2d -> %2d   any effect %2d -> %2d"
              % (cond, b["suspect"], c["suspect"], b["agreed"], c["agreed"],
                 b["any_effect"], c["any_effect"]))
    print()

trace = os.path.join(here, "..", "LOGBOOK", "experiments",
                     "suppressors-sintel-crab--qwen2-5-vl-7b-awq.jsonl")
with open(trace, "w", encoding="utf-8", newline="\n") as f:
    f.write(json.dumps({
        "kind": "suppressors", "model": trial.MODEL,
        "when": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "summary": {n: {c: {k: v for k, v in r.items() if k not in ("labels", "seens")}
                        for c, r in cs.items()} for n, cs in out.items()},
    }, ensure_ascii=False) + "\n")
    for name, cs in out.items():
        for i in range(cs["before"]["frames"]):
            f.write(json.dumps({
                "film": name, "t": round((i + 0.5) * trial.EVERY, 3),
                **{c: {"labels": cs[c]["labels"][i], "seen": cs[c]["seens"][i]}
                   for c in cs},
            }, ensure_ascii=False) + "\n")
print("trace written to", os.path.normpath(trace))
