"""How much of the running time is the fan actually doing something?

Counting stored points answers a different question. The curve is compressed
before it is written, and compression keeps points where the value is changing
— so a quiet half hour collapses to a handful of points and a busy minute keeps
dozens. Counting points therefore over-reports the busy parts, which is exactly
the thing being measured.

Weighted by the interval each point covers instead.
"""

import re
import sys

TIME = re.compile(r'\bt = "(\d+):(\d+):([\d.]+)"')
VAL = re.compile(r"intensity = ([0-9.]+)")


def seconds(m):
    return int(m[0]) * 3600 + int(m[1]) * 60 + float(m[2])


def wind_points(path):
    body = open(path, encoding="utf-8").read()
    # The wind track, up to the next track header.
    start = body.find('instrument = "wind')
    if start < 0:
        return []
    end = body.find("[[track]]", start)
    block = body[start:end if end > 0 else len(body)]
    out = []
    for line in block.splitlines():
        t = TIME.search(line)
        v = VAL.search(line)
        if t and v:
            out.append((seconds(t.groups()), float(v.group(1))))
    return out


for path in sys.argv[1:]:
    pts = wind_points(path)
    if len(pts) < 2:
        print("%-34s no wind curve" % path.split("/")[-1][:34])
        continue
    span = pts[-1][0] - pts[0][0]
    above = {0.15: 0.0, 0.35: 0.7 and 0.35, 0.7: 0.0}
    held = {0.15: 0.0, 0.35: 0.0, 0.7: 0.0}
    for (t0, v0), (t1, _v1) in zip(pts, pts[1:]):
        dt = t1 - t0
        for level in held:
            if v0 > level:
                held[level] += dt
    name = path.split("/")[-1].replace("wind-", "").replace(".componium", "")
    print("%-30s %6.0fs   above 0.15 %5.1f%%   above 0.35 %5.1f%%   above 0.7 %5.1f%%"
          % (name[:30], span,
             100 * held[0.15] / span, 100 * held[0.35] / span,
             100 * held[0.7] / span))
