"""What does wind actually respond to?

The claim in feat-two-clocks stage 3 is that wind is driven by the wrong
quantity: a pan across a static room reads as maximal, and a forward dolly —
driving, running, flying, the one case where air genuinely rushes past — reads
as near zero. Projection matching says it should be so, because a pan shifts
every column by the same amount and a dolly stretches the projection about its
centre, which no single shift describes.

Three synthetic clips from one still, where the camera move is known exactly:
a pan, a dolly in, and a static shot as the floor.

Writes nothing.
"""

import os
import sys

sys.path.insert(0, "/home/claude/Componium/composer")
import analysis
import motion_est

CLIPS = [("static", "/tmp/motion/static.mp4"),
         ("pan", "/tmp/motion/pan.mp4"),
         ("dolly in", "/tmp/motion/dolly.mp4")]
FPS = 12.0


def movements_of(path):
    d = analysis.decode(path, FPS, want_scenes=False)
    try:
        frames = [analysis.features(f) for f in d.gray()]
    finally:
        d.close()
    return motion_est.track(frames, width=analysis.GRAY_W)


print("%-10s %7s %8s %9s %9s" % ("clip", "frames", "mean |dx|", "mean speed", "mean wind"))
print("-" * 50)
rows = []
for name, path in CLIPS:
    if not os.path.exists(path):
        print("%-10s missing" % name)
        continue
    ms = movements_of(path)
    if not ms:
        print("%-10s no movements" % name)
        continue
    w = motion_est.wind_series(ms, FPS)
    adx = sum(abs(m.dx) for m in ms) / len(ms)
    sp = sum(m.speed for m in ms) / len(ms)
    rows.append((name, sp))
    print("%-10s %7d %9.2f %10.4f %9.3f"
          % (name, len(ms), adx, sp, sum(w) / max(len(w), 1)))

print()
if len(rows) == 3:
    by = dict(rows)
    print("static %.4f   pan %.4f   dolly %.4f"
          % (by["static"], by["pan"], by["dolly in"]))
    print()
    if by["pan"] > by["dolly in"]:
        print("Confirmed: the pan reads higher than the dolly. Wind is driven by")
        print("translation, which is the one thing a forward move does not produce.")
    else:
        print("Not confirmed: the dolly reads at least as high as the pan.")
    if by["dolly in"] <= by["static"] * 1.5:
        print("And the dolly is within half again of a static shot, which is to say")
        print("moving through air is indistinguishable from not moving at all.")
