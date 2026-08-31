"""How much of a film would the fan actually be blowing on?

The complaint was that wind was everywhere. The per-frame numbers say forward
movement happens on a fifth of frames at most, but wind is smoothed over a
second and a half before it reaches a fan, so what matters is how much of the
running time comes out above a level you would notice.

A few candidate scales, judged by that.
"""

import sys

sys.path.insert(0, "/home/claude/Componium/composer")
import analysis
import motion_est

FPS = 4.0  # what the pipeline actually samples at
FILMS = [
    ("rebel moon cut", "/home/claude/componium-media/Rebel.Moon.cut-1h03-1h18.mp4"),
    ("wanted cut", "/home/claude/componium-media/Wanted.2008.cut-0h55-1h10.mp4"),
    ("sintel", "/home/claude/componium-media/sintel.mp4"),
]
SCALES = (0.7, 1.4, 2.1, 3.0)

cache = {}
for name, path in FILMS:
    d = analysis.decode(path, FPS, want_scenes=False)
    try:
        frames = [analysis.features(f) for f in d.gray()]
    finally:
        d.close()
    cache[name] = motion_est.track(frames, width=analysis.GRAY_W)

print("share of running time above each level, per scale")
print()
print("%-16s %7s %8s %8s %8s" % ("film", "rate", ">0.15", ">0.35", ">0.7"))
print("-" * 54)
for name, _p in FILMS:
    ms = cache[name]
    for full in SCALES:
        w = motion_est.wind_series(ms, FPS, full=full)
        n = max(len(w), 1)
        print("%-16s %7.2f %7.1f%% %7.1f%% %7.1f%%"
              % (name if full == SCALES[0] else "", full,
                 100.0 * sum(1 for v in w if v > 0.15) / n,
                 100.0 * sum(1 for v in w if v > 0.35) / n,
                 100.0 * sum(1 for v in w if v > 0.7) / n))
    print()

print("A fan that is doing something a fifth of the time is a fan you notice.")
print("One doing something most of the time is the complaint being fixed.")
