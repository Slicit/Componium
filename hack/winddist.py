"""What is the smoothed forward rate, in the units the constant is now in?

Guessing scales and measuring what falls out is slow and keeps landing in the
wrong decade. The distribution of the thing itself says where to put the line.

Smoothed exactly as wind_series smooths it, at the rate the pipeline samples.
"""

import sys

sys.path.insert(0, "/home/claude/Componium/composer")
import analysis
import motion_est

FPS = 4.0
FILMS = [
    ("rebel moon cut", "/home/claude/componium-media/Rebel.Moon.cut-1h03-1h18.mp4"),
    ("wanted cut", "/home/claude/componium-media/Wanted.2008.cut-0h55-1h10.mp4"),
    ("sintel", "/home/claude/componium-media/sintel.mp4"),
    ("crab rave", "/home/claude/componium-media/noisestorm-crab-rave_138410.mp4"),
]


def pct(s, p):
    if not s:
        return 0.0
    return s[min(len(s) - 1, int(p / 100.0 * len(s)))]


print("smoothed forward rate, per second")
print("%-16s %8s %8s %8s %8s %8s" % ("film", "p50", "p75", "p90", "p97", "max"))
print("-" * 62)
everything = []
for name, path in FILMS:
    d = analysis.decode(path, FPS, want_scenes=False)
    try:
        frames = [analysis.features(f) for f in d.gray()]
    finally:
        d.close()
    ms = motion_est.track(frames, width=analysis.GRAY_W)
    forward = [max(0.0, m.expansion) * FPS for m in ms]
    sm = motion_est.smooth(forward, max(2, int(1.5 * FPS)))
    everything += sm
    s = sorted(sm)
    print("%-16s %8.3f %8.3f %8.3f %8.3f %8.3f"
          % (name, pct(s, 50), pct(s, 75), pct(s, 90), pct(s, 97), max(s)))

s = sorted(everything)
print()
print("all together   p50 %.3f  p75 %.3f  p90 %.3f  p97 %.3f  p99 %.3f  max %.3f"
      % (pct(s, 50), pct(s, 75), pct(s, 90), pct(s, 97), pct(s, 99), max(s)))
print()
print("Full wind wants to sit where a genuine forward move reaches it and a")
print("drifting camera does not — around the 97th percentile, so a few per cent")
print("of a film is windy and a fifth of it is stirring.")
