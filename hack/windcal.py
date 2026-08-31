"""What does expansion look like on real film, so the scale means something?

An absolute scale only works if it is set from the world rather than from one
synthetic clip. Measured across real footage: how often the camera is moving
forward at all, and how hard when it is.

The number wanted is the one where a genuine forward move — driving, running,
charging — reads as full wind, and a gentle push-in reads as a breath.
"""

import sys

sys.path.insert(0, "/home/claude/Componium/composer")
import analysis
import motion_est

FPS = 12.0
FILMS = [
    ("rebel moon cut", "/home/claude/componium-media/Rebel.Moon.cut-1h03-1h18.mp4"),
    ("wanted cut", "/home/claude/componium-media/Wanted.2008.cut-0h55-1h10.mp4"),
    ("sintel", "/home/claude/componium-media/sintel.mp4"),
    ("crab rave", "/home/claude/componium-media/noisestorm-crab-rave_138410.mp4"),
]


def pct(sorted_values, p):
    if not sorted_values:
        return 0.0
    i = min(len(sorted_values) - 1, int(p / 100.0 * len(sorted_values)))
    return sorted_values[i]


print("%-16s %7s %8s %8s %8s %8s %8s"
      % ("film", "frames", "forward%", "p50", "p90", "p99", "max"))
print("-" * 70)
allf = []
for name, path in FILMS:
    d = analysis.decode(path, FPS, want_scenes=False)
    try:
        frames = [analysis.features(f) for f in d.gray()]
    finally:
        d.close()
    ms = motion_est.track(frames, width=analysis.GRAY_W)
    fwd = [max(0.0, m.expansion) for m in ms]
    allf += fwd
    moving = [v for v in fwd if v > 0]
    s = sorted(fwd)
    print("%-16s %7d %7.1f%% %8.4f %8.4f %8.4f %8.4f"
          % (name, len(ms), 100.0 * len(moving) / max(len(fwd), 1),
             pct(s, 50), pct(s, 90), pct(s, 99), max(s) if s else 0))

s = sorted(allf)
print()
print("across everything: p90 %.4f  p99 %.4f  p99.9 %.4f  max %.4f"
      % (pct(s, 90), pct(s, 99), pct(s, 99.9), max(s) if s else 0))
print()
print("A scale set at the 99th percentile makes the top one per cent of forward")
print("movement full wind, and everything gentler a fraction of it. Set it at the")
print("maximum and nothing ever reaches full; set it at the median and half the")
print("film is a gale, which is the fault being fixed.")
