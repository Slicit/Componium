"""Does asking SCENE as a judgement make it steadier, without moving EFFECTS?

The claim committed to was specific and falsifiable, so this checks it rather
than admiring it. Against the baseline recorded in calm.py — on sintel the
model changed its mind about calm and active 113 times in fifteen minutes, a
median run of four seconds — the change is supposed to leave EFFECTS untouched
and make SCENE settle. If SCENE gets jumpier, or EFFECTS moves, it comes out.

Three conditions over the same frames, so nothing but the prompt differs:

    before   the prompt as it was, four lines, no reasoning allowed
    after    the prompt as it is, five lines, SCENE may judge
    context  the same, plus a line about what the film is

One decode, one set of JPEGs, three passes. Writes a trace to LOGBOOK.
"""

import argparse
import base64
import concurrent.futures as futures
import glob
import importlib.util
import json
import os
import platform
import shutil
import statistics
import subprocess
import sys
import time
import urllib.request

HOST = os.environ.get("COMPONIUM_VLM_HOST", "http://192.168.1.110:8123")
MODEL = os.environ.get("COMPONIUM_VLM_MODEL", "Qwen/Qwen2.5-VL-7B-Instruct-AWQ")
WORKERS = 16
EVERY = 2.0


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def frames(film, into, seconds):
    shutil.rmtree(into, ignore_errors=True)
    os.makedirs(into)
    subprocess.run(
        ["ffmpeg", "-v", "error", "-i", film,
         "-vf", "fps=%.6f,scale=512:-2" % (1.0 / EVERY), "-q:v", "3"]
        + (["-t", str(seconds)] if seconds else [])
        + [os.path.join(into, "f%05d.jpg")], check=True)
    return sorted(glob.glob(os.path.join(into, "*.jpg")))


def ask(prompt, image):
    with open(image, "rb") as f:
        img = base64.b64encode(f.read()).decode()
    body = json.dumps({
        "model": MODEL, "temperature": 0, "max_tokens": 220,
        "messages": [{"role": "user", "content": [
            {"type": "text", "text": prompt},
            {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + img}},
        ]}],
    }).encode()
    for attempt in range(3):
        try:
            req = urllib.request.Request(HOST + "/v1/chat/completions", data=body,
                                         headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=180) as r:
                return json.loads(r.read())["choices"][0]["message"]["content"]
        except Exception:
            if attempt == 2:
                return ""
            time.sleep(1)
    return ""


def run(prompt, images):
    out = [None] * len(images)
    began = time.time()
    with futures.ThreadPoolExecutor(max_workers=WORKERS) as pool:
        jobs = {pool.submit(ask, prompt, p): i for i, p in enumerate(images)}
        for job in futures.as_completed(jobs):
            out[jobs[job]] = job.result()
    return out, time.time() - began


def scene_runs(labels):
    """How often calm/active flips, and how long a stretch lasts in seconds."""
    states = ["active" if "scene-active" in ls else "calm" for ls in labels]
    changes, runs, run = 0, [], 1
    for a, b in zip(states, states[1:]):
        if a != b:
            changes += 1
            runs.append(run * EVERY)
            run = 1
        else:
            run += 1
    runs.append(run * EVERY)
    return changes, runs, states


def effects_of(labels):
    return {l for l in labels if not l.startswith("scene-") and l != "water"}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("film")
    ap.add_argument("--seconds", type=float, default=0)
    ap.add_argument("--context", default="")
    ap.add_argument("--trace", default="")
    args = ap.parse_args()

    old = load("/tmp/vlm-old.py", "vlm_old")
    new = load(os.path.join(os.path.dirname(os.path.abspath(__file__)),
                            "vlm-label.py"), "vlm_new")

    images = frames(args.film, "/tmp/trial", args.seconds)
    print("%d frames, every %.0fs" % (len(images), EVERY))
    print()

    conditions = [("before", old.PROMPT), ("after", new.PROMPT)]
    if args.context:
        conditions.append(("context", new.PROMPT + new.CONTEXT_PROMPT % args.context))

    results = {}
    for name, prompt in conditions:
        replies, took = run(prompt, images)
        labels = [new.parse(r or "") for r in replies]
        changes, runs, states = scene_runs(labels)
        results[name] = {
            "replies": replies, "labels": labels, "states": states,
            "changes": changes, "median_run": statistics.median(runs),
            "active": states.count("active"),
            "seconds": round(took, 1),
        }
        print("%-8s %5.0fs   SCENE: %3d changes, median run %4.0fs, %3d of %d active"
              % (name, took, changes, statistics.median(runs),
                 states.count("active"), len(states)))

    print()
    base = results["before"]
    for name in [n for n, _ in conditions if n != "before"]:
        got = results[name]
        moved = sum(1 for a, b in zip(base["labels"], got["labels"])
                    if effects_of(a) != effects_of(b))
        scene_moved = sum(1 for a, b in zip(base["states"], got["states"]) if a != b)
        print("%s vs before:" % name)
        print("   EFFECTS differ on %d of %d frames (%.1f%%)  <- must be ~0"
              % (moved, len(images), 100 * moved / len(images)))
        print("   SCENE   differs on %d of %d frames (%.1f%%)"
              % (scene_moved, len(images), 100 * scene_moved / len(images)))
        print("   changes %d -> %d, median run %.0fs -> %.0fs"
              % (base["changes"], got["changes"],
                 base["median_run"], got["median_run"]))
        print()

    if "after" in results:
        guesses = [new.inferred(r or "") for r in results["after"]["replies"]]
        offered = [g for g in guesses if g]
        print("LIKELY offered on %d of %d frames" % (len(offered), len(images)))
        for g in offered[:6]:
            print("   %s" % g[:70])

    if args.trace:
        with open(args.trace, "w", encoding="utf-8", newline="\n") as f:
            f.write(json.dumps({
                "kind": "prompt-trial", "model": MODEL, "host": HOST,
                "film": os.path.basename(args.film), "frames": len(images),
                "every_seconds": EVERY, "context": args.context,
                "when": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "box": platform.node(),
                "summary": {n: {k: v for k, v in r.items()
                                if k in ("changes", "median_run", "active", "seconds")}
                            for n, r in results.items()},
            }, ensure_ascii=False) + "\n")
            for i in range(len(images)):
                f.write(json.dumps({
                    "t": round((i + 0.5) * EVERY, 3),
                    **{n: {"labels": results[n]["labels"][i],
                           "seen": new.described(results[n]["replies"][i] or ""),
                           "likely": new.inferred(results[n]["replies"][i] or "")}
                       for n in results},
                }, ensure_ascii=False) + "\n")
        print()
        print("trace written to", args.trace)
    return 0


if __name__ == "__main__":
    sys.exit(main())
