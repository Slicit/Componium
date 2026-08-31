"""Show it two frames instead of one.

Prompt text is spent. Emphasis, reasoning room, absorbing categories and
base-rate suppression have all been measured, and the shipped wording has the
best precision of anything tried — removing its negations nearly triples label
production, and adding suppression halves the true positives with the false.

What is left is that the questions are temporal and the evidence is not. Dust
is thrown and snow is settled. A splash needs water that is moving. Activity is
motion. None of those is answerable from one still, and no wording will make it
so, which is why every wording has failed the same way.

So: the same prompt, the same frame, plus the frame a second before it. The
model is told only that the pair is consecutive and which is which.

Judged as before — suspects down, agreed held — plus what only a pair can fix:
whether snow stops being dust while thrown sand stays dust.
"""

import base64
import concurrent.futures as futures
import glob
import importlib.util
import json
import os
import re
import subprocess
import sys
import time
import urllib.request

here = os.path.dirname(os.path.abspath(__file__))
sys.argv = [sys.argv[0]]
spec = importlib.util.spec_from_file_location("trial", os.path.join(here, "prompt-trial.py"))
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)
vlm = trial.load(os.path.join(here, "vlm-label.py"), "vlm_now")

GAP = 1.0  # seconds between the pair

PAIR_NOTE = """

You are given two frames from the same shot, one second apart: the first is
earlier, the second is later. Answer about the LATER frame. The earlier one is
there so you can tell what is moving from what is merely present — dust is
thrown and settles, snow falls or lies, water that splashes is water that
moved. If nothing has changed between them, nothing is happening."""

PAIR_PROMPT = vlm.PROMPT + PAIR_NOTE

NEARLY = re.compile(r"\b(snow|snowy|mist|misty|fog|foggy|haze|hazy|steam|"
                    r"cloud|powder|white substance)\b", re.I)
REALLY = re.compile(r"\b(smoke|smoky|dust|dusty|ash|debris|rubble|soot|"
                    r"explosion|blast|burning|fire)\b", re.I)


def pairs(film, into, gap=GAP):
    """Frames on the usual grid, and the frame `gap` before each."""
    for sub, offset in (("late", 0.0), ("early", -gap)):
        d = os.path.join(into, sub)
        subprocess.run(["rm", "-rf", d], check=False)
        os.makedirs(d)
        args = ["ffmpeg", "-v", "error"]
        if offset:
            # Shift the whole grid back; the first frame has no earlier twin
            # and is dropped below.
            args += ["-ss", "%.3f" % max(0.0, trial.EVERY / 2 - gap)]
        args += ["-i", film, "-vf",
                 "fps=%.6f,scale=512:-2" % (1.0 / trial.EVERY), "-q:v", "3",
                 os.path.join(d, "f%05d.jpg")]
        subprocess.run(args, check=True)
    late = sorted(glob.glob(os.path.join(into, "late", "*.jpg")))
    early = sorted(glob.glob(os.path.join(into, "early", "*.jpg")))
    n = min(len(late), len(early))
    return list(zip(early[:n], late[:n]))


def ask_pair(images):
    parts = [{"type": "text", "text": PAIR_PROMPT}]
    for p in images:
        with open(p, "rb") as f:
            parts.append({"type": "image_url", "image_url": {
                "url": "data:image/jpeg;base64," + base64.b64encode(f.read()).decode()}})
    body = json.dumps({"model": trial.MODEL, "temperature": 0, "max_tokens": 220,
                       "messages": [{"role": "user", "content": parts}]}).encode()
    for attempt in range(3):
        try:
            req = urllib.request.Request(trial.HOST + "/v1/chat/completions",
                                         data=body, headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=240) as r:
                return json.loads(r.read())["choices"][0]["message"]["content"]
        except Exception:
            if attempt == 2:
                return ""
            time.sleep(1)
    return ""


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


FILMS = [("sintel", "/home/claude/componium-media/sintel.mp4"),
         ("crab-rave", "/home/claude/componium-media/noisestorm-crab-rave_138410.mp4")]

out = {}
for name, path in FILMS:
    ps = pairs(path, "/tmp/pairs")
    singles = [b for _a, b in ps]
    print("%s: %d pairs %.0fs apart" % (name, len(ps), GAP))
    out[name] = {}

    replies, took = trial.run(vlm.PROMPT, singles)
    began = time.time()
    two = [None] * len(ps)
    with futures.ThreadPoolExecutor(max_workers=trial.WORKERS) as pool:
        jobs = {pool.submit(ask_pair, p): i for i, p in enumerate(ps)}
        for job in futures.as_completed(jobs):
            two[jobs[job]] = job.result()
    took2 = time.time() - began

    for cond, rs, secs in (("one frame", replies, took), ("two frames", two, took2)):
        labels = [vlm.parse(r or "") for r in rs]
        seens = [vlm.described(r or "") for r in rs]
        flagged, suspect, agreed = score(labels, seens)
        effects = sum(1 for ls in labels
                      if any(l for l in ls if not l.startswith("scene-") and l != "water"))
        active = sum(1 for ls in labels if "scene-active" in ls)
        changes, _r, _s = trial.scene_runs(labels)
        out[name][cond] = {"frames": len(ps), "seconds": round(secs, 1),
                           "suspect": suspect, "agreed": agreed,
                           "any_effect": effects, "active": active,
                           "scene_changes": changes,
                           "labels": labels, "seens": seens}
        print("   %-10s %5.0fs  suspect %3d  agreed %3d   any effect %3d   active %3d   SCENE changes %3d"
              % (cond, secs, suspect, agreed, effects, active, changes))
    print()

print("=" * 74)
for name, cs in out.items():
    a, b = cs["one frame"], cs["two frames"]
    print("%-10s suspect %2d -> %2d   agreed %2d -> %2d   any effect %2d -> %2d   SCENE %3d -> %3d   cost x%.1f"
          % (name, a["suspect"], b["suspect"], a["agreed"], b["agreed"],
             a["any_effect"], b["any_effect"], a["scene_changes"], b["scene_changes"],
             b["seconds"] / max(a["seconds"], 0.1)))

trace = os.path.join(here, "..", "LOGBOOK", "experiments",
                     "twoframes-sintel-crab--qwen2-5-vl-7b-awq.jsonl")
with open(trace, "w", encoding="utf-8", newline="\n") as f:
    f.write(json.dumps({
        "kind": "twoframes", "model": trial.MODEL, "gap_seconds": GAP,
        "when": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "addition": PAIR_NOTE.strip(),
        "summary": {n: {c: {k: v for k, v in r.items() if k not in ("labels", "seens")}
                        for c, r in cs.items()} for n, cs in out.items()},
    }, ensure_ascii=False) + "\n")
    for name, cs in out.items():
        for i in range(cs["one frame"]["frames"]):
            f.write(json.dumps({
                "film": name, "t": round((i + 0.5) * trial.EVERY, 3),
                **{c: {"labels": cs[c]["labels"][i], "seen": cs[c]["seens"][i]}
                   for c in cs},
            }, ensure_ascii=False) + "\n")
print("trace written to", os.path.normpath(trace))
