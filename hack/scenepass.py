"""Is a scene pass better at judging activity than the frame pass, and cheaper?

Stage one moved the baseline: two frames took SCENE from 114 changes across
sintel to 40. So "steadier" is no longer the test, and at a fifth of the rate
fewer changes would be true whatever the answers were.

The honest measure is agreement with evidence the model never sees. calm.py
already blends its judgement with two independent signals — the low frequency
audio and the camera's own speed — at half, a quarter and a quarter. If the
scene pass tracks those better than the frame pass does, it is reading the film
rather than the frame in front of it. If it tracks them worse, it is guessing
more comfortably.

    frame pass   every 2s, a pair, the shipped prompt
    scene pass   every 10s, a pair, asked about the scene rather than the frame

Held across its window so both are compared at the same resolution.
"""

import base64
import concurrent.futures as futures
import glob
import importlib.util
import json
import os
import subprocess
import sys
import time
import urllib.request

sys.path.insert(0, "/home/claude/Componium/composer")
import analysis
import motion_est

here = os.path.dirname(os.path.abspath(__file__))
HOST = "http://192.168.1.110:8123"
MODEL = "Qwen/Qwen2.5-VL-7B-Instruct-AWQ"
WORKERS = 16
FRAME_EVERY = 2.0
SCENE_EVERY = 10.0
GAP = 1.0
FPS = 4.0

spec = importlib.util.spec_from_file_location("vlm", os.path.join(here, "vlm-label.py"))
vlm = importlib.util.module_from_spec(spec)
spec.loader.exec_module(vlm)

SCENE_PROMPT = """You are shown two frames from a film, one second apart, to
judge the scene they belong to rather than the frames themselves.

Reply with exactly three lines and nothing else. No explanation.

PLACE: <a few words for where this is — a battlefield, a forest, a kitchen, a
        ship's corridor, a city street at night>
DOING: <a few words for what is happening — a fight, a conversation, a chase,
        a funeral, someone cooking>
BUSY: <calm or active>

BUSY is about how much is happening to an audience watching, not how pretty the
frame is, and it is about the scene rather than the instant. A lull inside a
battle is still a battle. Conversation, stillness, scenery and walking are
calm; impact, combat, chaos, speed and destruction are active.

Most of most films is calm."""


def line_of(reply, head):
    for line in (reply or "").splitlines():
        a, _, b = line.strip().lstrip("-*# ").partition(":")
        if a.strip().strip("*").lower() == head:
            return " ".join(b.split())
    return ""


def frames_at(film, into, every, gap):
    """Pairs on a grid: the frame, and the one `gap` before it."""
    subprocess.run(["rm", "-rf", into], check=False)
    os.makedirs(into)
    subprocess.run(
        ["ffmpeg", "-v", "error", "-ss", "%.3f" % max(0.0, every / 2 - gap),
         "-i", film, "-vf", "fps=%.6f,scale=512:-2" % (1.0 / gap),
         "-q:v", "3", os.path.join(into, "f%05d.jpg")], check=True)
    shots = sorted(glob.glob(os.path.join(into, "*.jpg")))
    per = int(round(every / gap))
    out = []
    for k in range(len(shots) // per):
        j = k * per + 1
        if j < len(shots):
            out.append(((k + 0.5) * every, [shots[j - 1], shots[j]]))
    return out


def ask(prompt, images):
    parts = [{"type": "text", "text": prompt}]
    for p in images:
        with open(p, "rb") as f:
            parts.append({"type": "image_url", "image_url": {
                "url": "data:image/jpeg;base64," + base64.b64encode(f.read()).decode()}})
    body = json.dumps({"model": MODEL, "temperature": 0, "max_tokens": 220,
                       "messages": [{"role": "user", "content": parts}]}).encode()
    for attempt in range(3):
        try:
            req = urllib.request.Request(HOST + "/v1/chat/completions", data=body,
                                         headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=240) as r:
                return json.loads(r.read())["choices"][0]["message"]["content"]
        except Exception:
            if attempt == 2:
                return ""
            time.sleep(1)
    return ""


def run(prompt, items):
    out = [None] * len(items)
    began = time.time()
    with futures.ThreadPoolExecutor(max_workers=WORKERS) as pool:
        jobs = {pool.submit(ask, prompt, imgs): i for i, (_t, imgs) in enumerate(items)}
        for job in futures.as_completed(jobs):
            out[jobs[job]] = job.result()
    return out, time.time() - began


def evidence(path):
    """Audio loudness and camera speed, which the model never sees."""
    d = analysis.decode(path, FPS, want_scenes=False)
    try:
        frames = [analysis.features(f) for f in d.gray()]
        audio = list(d.audio())
    finally:
        d.close()
    ms = motion_est.track(frames, width=analysis.GRAY_W)
    speed = motion_est.speed_series(ms, FPS)
    peak = max(speed) if speed else 0.0
    speed = [v / peak for v in speed] if peak > 0 else speed

    # Loudness per analysis frame, normalised.
    window = max(1, len(audio) // max(len(speed), 1))
    loud = []
    for i in range(len(speed)):
        chunk = audio[i * window:(i + 1) * window]
        loud.append(max((abs(v) for v in chunk), default=0))
    top = max(loud) if loud else 0
    loud = [v / top for v in loud] if top else loud
    return [(i / FPS, 0.5 * a + 0.5 * b) for i, (a, b) in enumerate(zip(loud, speed))]


def agreement(states, ev):
    """How much louder and faster the film is where the model says active.

    A number above one means the model's active moments really are the busier
    ones. At one it is saying nothing the evidence agrees with.
    """
    act = [v for t, v in ev if lookup(states, t) == "active"]
    calm = [v for t, v in ev if lookup(states, t) == "calm"]
    if not act or not calm:
        return 0.0
    return (sum(act) / len(act)) / max(sum(calm) / len(calm), 1e-6)


def lookup(states, t):
    best = "calm"
    for at, s in states:
        if at <= t:
            best = s
        else:
            break
    return best


FILMS = [("sintel", "/home/claude/componium-media/sintel.mp4"),
         ("rebel moon cut",
          "/home/claude/componium-media/Rebel.Moon.cut-1h03-1h18.mp4")]

for name, path in FILMS:
    print("=" * 66)
    print(name)
    ev = evidence(path)

    fitems = frames_at(path, "/tmp/sp-frame", FRAME_EVERY, GAP)
    freplies, ftook = run(vlm.prompt(True), fitems)
    fstates = [(t, "active" if "scene-active" in vlm.parse(r or "") else "calm")
               for (t, _i), r in zip(fitems, freplies)]

    sitems = frames_at(path, "/tmp/sp-scene", SCENE_EVERY, GAP)
    sreplies, stook = run(SCENE_PROMPT, sitems)
    sstates = [(t, "active" if line_of(r, "busy").lower().startswith("active") else "calm")
               for (t, _i), r in zip(sitems, sreplies)]

    # The frame pass, held over the scene pass's windows by majority. Same
    # answers, same resolution as the thing it is being compared with.
    coarse = []
    for k in range(int(len(fstates) * FRAME_EVERY / SCENE_EVERY) + 1):
        lo, hi = k * SCENE_EVERY, (k + 1) * SCENE_EVERY
        inside = [st for t, st in fstates if lo <= t < hi]
        if inside:
            active = sum(1 for st in inside if st == "active")
            coarse.append((lo + SCENE_EVERY / 2,
                           "active" if active * 2 > len(inside) else "calm"))

    for label, items, states, took in (
            ("frame pass", fitems, fstates, ftook),
            ("frame, held", fitems, coarse, ftook),
            ("scene pass", sitems, sstates, stook)):
        changes = sum(1 for a, b in zip(states, states[1:]) if a[1] != b[1])
        act = sum(1 for _t, s in states if s == "active")
        print("  %-11s %4d calls %5.0fs   %3d changes   %3d%% active   agreement %.2f"
              % (label, len(items), took, changes,
                 round(100 * act / max(len(states), 1)), agreement(states, ev)))

    print()
    print("  what the scene pass says it is looking at:")
    for (t, _i), r in list(zip(sitems, sreplies))[:6]:
        print("    %6.1fs  %-26s %s" % (t, line_of(r, "place")[:26], line_of(r, "doing")[:34]))
    print()
