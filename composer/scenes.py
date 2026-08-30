"""Detect scene cuts, so that effects do not bleed across them.

An automatically generated curve that ramps smoothly through a hard cut is the
most obvious tell of a machine generated score: the room keeps behaving as if
it were still in the previous shot. Snapping the curve at the cut costs one
extra point and fixes it.

ffmpeg's scene detection filter does the work. It scores each frame by how
different it is from the previous one, and anything above the threshold is a
cut.
"""

from __future__ import annotations

import re
import shutil
import subprocess

PTS = re.compile(r"pts_time:([0-9.]+)")


def detect(path: str, threshold: float = 0.35, span=None) -> list[float]:
    """Return the times of detected scene cuts, in seconds.

    A higher threshold means fewer, more confident cuts. 0.35 is conservative:
    missing a cut costs a little smoothness, while a false cut snaps the room
    for no reason, which is more noticeable.
    """
    exe = shutil.which("ffmpeg")
    if not exe:
        return []
    out = subprocess.run(
        [exe, "-v", "info", *(span.input_args() if span else []), "-i", path,
         "-vf", f"select='gt(scene,{threshold})',showinfo",
         "-an", "-f", "null", "-"],
        capture_output=True, text=True, errors="replace", check=False,
    )
    # showinfo writes to stderr, which is where ffmpeg puts all diagnostics.
    times = [float(m) for m in PTS.findall(out.stderr)]
    return sorted(set(times))


def snap(points, cuts, epsilon: float = 0.04):
    """Insert holding points just before each cut so curves step rather than ramp.

    For every cut that falls strictly inside the curve, the value immediately
    before the cut is repeated at cut minus epsilon. Interpolation then holds
    flat up to the cut and jumps at it, instead of sliding through it.

    epsilon defaults to roughly one frame at 24fps: close enough to the cut to
    be invisible, far enough not to collide with a point already there.
    """
    if not points or not cuts:
        return list(points)

    out = list(points)
    for cut in cuts:
        if cut <= out[0][0] or cut >= out[-1][0]:
            continue
        before = None
        for point in out:
            if point[0] < cut - epsilon:
                before = point
            else:
                break
        if before is None:
            continue
        if any(abs(p[0] - (cut - epsilon)) < 1e-6 for p in out):
            continue
        out.append((cut - epsilon, before[1]))
    out.sort(key=lambda p: p[0])
    return out
