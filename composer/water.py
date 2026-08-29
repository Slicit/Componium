"""Water in the scene, nominated rather than detected.

Be clear about what this is. Reliably recognising water needs a model. What is
here is a colour and position heuristic: it finds frames where blue and cyan
dominate, and where they sit low in the picture or fill it entirely, which is
what a sea, a pool, rain over a dark street and an underwater shot have in
common.

That is a *nominator*. It produces candidate windows cheaply, and something
that actually knows what it is looking at confirms them: a subtitle saying
[rain], or the vision model reached through `--vlm-command`. Used alone it will
also nominate a blue sky, a night interior and a swimming pool advert.

Driving a mister from an unconfirmed nomination is how somebody's sofa gets
wet during a scene set in a blue-lit office, so nothing here emits a cue by
itself.
"""

from __future__ import annotations

W = 8
H = 8


def blueness(frame: bytes, w: int = W, h: int = H) -> tuple[float, float]:
    """Return (overall, lower) blue-cyan dominance, each 0..1.

    Overall is the whole frame. Lower is the bottom third, because a sea, a
    river and a puddle are all below the horizon, and separating the two is
    what stops every daylight exterior scoring as water.
    """
    if len(frame) < w * h * 3:
        return 0.0, 0.0

    def score(px_lo: int, px_hi: int) -> float:
        total = 0.0
        count = 0
        for i in range(px_lo * 3, px_hi * 3, 3):
            r, g, b = frame[i], frame[i + 1], frame[i + 2]
            level = (r + g + b) / 3.0
            if level < 18:
                continue  # near black tells us nothing about hue
            cyan = (g + b) / 2.0
            # How far the pixel leans blue-cyan, normalised so a dim blue and a
            # bright blue score alike.
            lean = (cyan - r) / max(level, 1.0)
            if lean > 0:
                total += min(1.0, lean * 2.0)
            count += 1
        return total / count if count else 0.0

    lower_start = (h * 2 // 3) * w
    return score(0, w * h), score(lower_start, w * h)


def candidates(frames, fps: float, threshold: float = 0.30,
               min_seconds: float = 3.0, merge_gap: float = 2.0):
    """Find windows worth asking a model about.

    Returns (start, end, confidence) triples. Confidence is the mean score over
    the run and should be treated as "how blue", not "how likely to be water".
    """
    scores = []
    for frame in frames:
        overall, lower = blueness(frame)
        # Weighted toward the lower frame, where water usually is, but not
        # exclusively: an underwater shot is blue everywhere.
        scores.append(max(overall, 0.4 * overall + 0.6 * lower))

    if not scores:
        return []

    min_len = max(1, int(min_seconds * fps))
    runs = []
    start = None
    for i, v in enumerate(scores):
        if v >= threshold and start is None:
            start = i
        elif v < threshold and start is not None:
            if i - start >= min_len:
                runs.append([start, i])
            start = None
    if start is not None and len(scores) - start >= min_len:
        runs.append([start, len(scores)])

    merged = []
    gap = int(merge_gap * fps)
    for run in runs:
        if merged and run[0] - merged[-1][1] <= gap:
            merged[-1][1] = run[1]
        else:
            merged.append(run)

    return [
        (lo / fps, hi / fps, sum(scores[lo:hi]) / float(hi - lo))
        for lo, hi in merged
    ]


def confirmed(candidates_list, confirmations, slack: float = 6.0):
    """Keep only candidates a second source agrees with.

    confirmations is a list of (time, label) from subtitles or a vision model.
    A nomination survives if something that actually understands the scene said
    a water word near it.
    """
    words = ("water", "rain", "sea", "ocean", "wave", "waves", "river",
             "underwater", "swim", "swimming", "storm", "downpour", "flood")
    out = []
    for lo, hi, score in candidates_list:
        for at, label in confirmations:
            if lo - slack <= at <= hi + slack and any(w in label.lower() for w in words):
                out.append((lo, hi, score, label))
                break
    return out
