"""Does naming the near-misses stop the model reaching for the effect words?

The shipped prompt already says smoke is "not fog, mist, haze or low cloud" and
then labels a snowy, misty frame as smoke anyway. The theory is that this is
not carelessness but a missing correct answer: the model sees pale airborne
matter, has seven buckets, and none of them is snow.

So the near-misses get named as explicitly not effects. That is the extensive
vocabulary idea aimed at the failure that is actually happening, rather than at
more effect words, which the splash result says would make it worse.

Judged without external ground truth by using the model's own sentence against
its own label. Where it labels smoke or dust and then describes snow or mist
and nothing burning, one of the two is wrong, and it is not the sentence — the
sentence is written last and keeps contradicting the label. Where it labels
smoke or dust and describes smoke or dust, they agree.

    suspect    labelled smoke/dust, sentence says snow/mist/fog/haze/steam
               and says nothing about smoke, dust, ash or debris
    agreed     labelled smoke/dust, sentence says smoke/dust/ash/debris

Success is suspects down and agreed held. Suspects down because everything
went down is a prompt that stopped answering, not a prompt that got it right.
"""

import glob
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

# Kept deliberately short. Every addition to this prompt moves labels, so the
# smallest one that names the observed confusions is the one to try.
ABSORBERS = """

Some things look like an effect and are not. These in particular:

  snow falling or lying, fog, mist, haze, low cloud, steam, a pale powder or
  substance at rest, dust already settled, smoke that has thinned to haze.

If the frame shows one of those and nothing from the list above, the answer is
EFFECTS: none."""

WITH_ABSORBERS = vlm.PROMPT.replace(
    "If none of those are plainly visible, answer exactly: EFFECTS: none",
    "If none of those are plainly visible, answer exactly: EFFECTS: none"
    + ABSORBERS, 1)
assert WITH_ABSORBERS != vlm.PROMPT, "the anchor moved"

NEARLY = re.compile(r"\b(snow|snowy|mist|misty|fog|foggy|haze|hazy|steam|"
                    r"cloud|powder|white substance)\b", re.I)
REALLY = re.compile(r"\b(smoke|smoky|dust|dusty|ash|debris|rubble|soot|"
                    r"explosion|blast|burning|fire)\b", re.I)

FILMS = [
    ("sintel", "/home/claude/componium-media/sintel.mp4", 0),
    ("crab-rave", "/home/claude/componium-media/noisestorm-crab-rave_138410.mp4", 0),
]


def score(labels, seens):
    suspect = agreed = flagged = 0
    for ls, said in zip(labels, seens):
        hit = "smoke" in ls or "dust" in ls
        if hit:
            flagged += 1
            if REALLY.search(said or ""):
                agreed += 1
            elif NEARLY.search(said or ""):
                suspect += 1
    return flagged, suspect, agreed


out = {}
for name, path, seconds in FILMS:
    images = trial.frames(path, "/tmp/absorb", seconds)
    print("%s: %d frames" % (name, len(images)))
    out[name] = {}
    for cond, prompt in (("before", vlm.PROMPT), ("absorbers", WITH_ABSORBERS)):
        replies, took = trial.run(prompt, images)
        labels = [vlm.parse(r or "") for r in replies]
        seens = [vlm.described(r or "") for r in replies]
        flagged, suspect, agreed = score(labels, seens)
        effects = sum(1 for ls in labels
                      if any(l for l in ls if not l.startswith("scene-") and l != "water"))
        out[name][cond] = {
            "frames": len(images), "seconds": round(took, 1),
            "smoke_or_dust": flagged, "suspect": suspect, "agreed": agreed,
            "any_effect": effects,
            "labels": labels, "seens": seens,
        }
        print("   %-10s %5.0fs  smoke/dust %3d  of which suspect %3d, agreed %3d"
              "   any effect %3d"
              % (cond, took, flagged, suspect, agreed, effects))
    print()

print("=" * 68)
for name in out:
    b, a = out[name]["before"], out[name]["absorbers"]
    print("%s" % name)
    print("   suspect      %3d -> %3d" % (b["suspect"], a["suspect"]))
    print("   agreed       %3d -> %3d   <- must hold" % (b["agreed"], a["agreed"]))
    print("   any effect   %3d -> %3d   <- a collapse here is a prompt that stopped answering"
          % (b["any_effect"], a["any_effect"]))
    print()

trace = os.path.join(here, "..", "LOGBOOK", "experiments",
                     "absorbers-sintel-crab--qwen2-5-vl-7b-awq.jsonl")
with open(trace, "w", encoding="utf-8", newline="\n") as f:
    f.write(json.dumps({
        "kind": "absorbers", "model": trial.MODEL,
        "when": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "addition": ABSORBERS.strip(),
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
