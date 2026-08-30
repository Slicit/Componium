"""Camera movement, speed, and plunges, estimated from frame projections.

The method is projection matching, which is old and cheap and works. A camera
pan shifts every column of the image by the same amount, so the sum of each
column shifts too, and finding that shift is a search over 64 numbers instead
of 2304 pixels.

What this can and cannot tell you is worth being clear about. It measures
apparent movement of the image, which is camera movement plus whatever large
thing is moving in front of the camera. It cannot distinguish a camera tilting
down from a camera falling, because those look identical through a lens. A
sustained vertical movement is therefore reported as a *plunge candidate*, not
as a fact, and it is the sort of thing a vision model should confirm.
"""

from __future__ import annotations

import math


def centre(projection):
    """Subtract the mean, so matching is not dominated by brightness changes.

    A cut to a brighter shot raises every column equally. Without this, that
    looks like an enormous mismatch at every shift and the estimate is noise.
    """
    if not projection:
        return []
    mean = sum(projection) / float(len(projection))
    return [v - mean for v in projection]


def best_shift(a, b, max_shift: int) -> tuple[int, float]:
    """Find the shift of b relative to a that matches best.

    Returns the shift in samples and a confidence between 0 and 1. Confidence
    is how much better the best shift is than the average shift: a flat search
    surface means the frame had nothing to match on, which happens on a plain
    sky or a hard cut, and the caller should not believe the answer.
    """
    a = centre(a)
    b = centre(b)
    n = len(a)
    if n == 0 or len(b) != n:
        return 0, 0.0

    scores = {}
    for s in range(-max_shift, max_shift + 1):
        lo = max(0, -s)
        hi = min(n, n - s)
        if hi - lo < n // 2:
            continue  # too little overlap to mean anything
        total = 0.0
        for i in range(lo, hi):
            total += abs(a[i] - b[i + s])
        scores[s] = total / float(hi - lo)

    if not scores:
        return 0, 0.0

    # Tie break toward no movement. On a featureless frame, a blank sky or
    # a fade to black, every shift scores identically and picking the first
    # one reports the maximum shift as fact. That is how a static dark shot
    # came to be measured as the fastest movement in the film.
    best = min(scores, key=lambda s: (scores[s], abs(s)))
    best_score = scores[best]
    average = sum(scores.values()) / float(len(scores))
    if average <= 0:
        return 0, 0.0
    confidence = max(0.0, min(1.0, 1.0 - (best_score / average)))
    if confidence <= 0.0:
        return 0, 0.0
    return best, confidence


class Movement:
    """Apparent movement between two sampled frames."""

    __slots__ = ("dx", "dy", "speed", "confidence")

    def __init__(self, dx: int, dy: int, confidence: float, width: int):
        self.dx = dx
        self.dy = dy
        self.confidence = confidence
        # Normalised by frame width so the number means the same thing whatever
        # resolution the analysis ran at.
        self.speed = math.hypot(dx, dy) / float(width)


def track(frames, max_shift: int = 8, width: int = 64,
          min_confidence: float = 0.05):
    """Estimate movement between consecutive frames.

    dy is how far the image content moved *down*. A camera descending makes the
    world rise in frame, so a fall shows as sustained negative dy.
    """
    out = []
    for i in range(1, len(frames)):
        prev, cur = frames[i - 1], frames[i]
        dx, cx = best_shift(prev.cols, cur.cols, max_shift)
        dy, cy = best_shift(prev.rows, cur.rows, max_shift)
        confidence = min(cx, cy)
        if confidence < min_confidence:
            # No evidence of movement is reported as no movement, not as
            # whatever the search happened to land on. Missing a pan across
            # a featureless sky is better than inventing one.
            dx = dy = 0
        out.append(Movement(dx, dy, confidence, width))
    return out


def smooth(values, window: int):
    """Moving average. Camera movement is continuous; single frame spikes are
    matching errors, and a cut produces exactly one."""
    if window < 2 or len(values) < window:
        return list(values)
    out = []
    half = window // 2
    for i in range(len(values)):
        lo = max(0, i - half)
        hi = min(len(values), i + half + 1)
        out.append(sum(values[lo:hi]) / float(hi - lo))
    return out


def find_plunges(movements, fps: float, min_seconds: float = 1.0,
                 threshold: float = 3.0, min_confidence: float = 0.15,
                 min_magnitude: float = 0.06, merge_gap: float = 2.0):
    """Find sustained downward camera movement.

    Returns (start, end, magnitude) triples in seconds. Magnitude is the mean
    normalised speed over the run, so a caller can scale an effect by how
    violent the fall was rather than treating every one the same.

    Two gates, and both are needed. threshold is the per sample vertical
    movement in pixels of a 36 pixel tall frame, so 3.0 is a little over
    eight percent of the height per sample: at 4 Hz that is a third of the
    frame per second, which is a fall rather than a drift. min_magnitude
    then requires the *overall* movement to be substantial, which rejects a
    slow vertical pan that happens to be steady.

    The first version of this had a 1.2 pixel threshold and no magnitude
    gate, and found thirty plunges in a two minute test pattern that was
    merely scrolling.

    Deliberately conservative. A false plunge drops somebody's seat during a
    dialogue scene, which is worse than missing a real one.
    """
    if not movements:
        return []
    dys = smooth([m.dy for m in movements], 3)
    speeds = [m.speed for m in movements]
    min_frames = max(2, int(min_seconds * fps))

    runs = []
    start = None
    for i, dy in enumerate(dys):
        falling = dy <= -threshold and movements[i].confidence >= min_confidence
        if falling and start is None:
            start = i
        elif not falling and start is not None:
            if i - start >= min_frames:
                runs.append((start, i))
            start = None
    if start is not None and len(dys) - start >= min_frames:
        runs.append((start, len(dys)))

    # A continuous fall dips below threshold for a frame or two whenever the
    # matcher loses confidence, which fragments one plunge into several.
    # Firing five seat drops during one fall is worse than firing one.
    merged = []
    gap = int(merge_gap * fps)
    for run in runs:
        if merged and run[0] - merged[-1][1] <= gap:
            merged[-1][1] = run[1]
        else:
            merged.append([run[0], run[1]])
    runs = merged

    out = []
    for lo, hi in runs:
        magnitude = sum(speeds[lo:hi]) / float(hi - lo)
        if magnitude < min_magnitude:
            continue
        # +1 because movements[i] describes the step ending at frame i+1.
        out.append(((lo + 1) / fps, (hi + 1) / fps, magnitude))
    return out


def speed_series(movements, fps: float, window: float = 0.5):
    """Smoothed normalised speed, one value per movement sample."""
    return smooth([m.speed for m in movements], max(2, int(window * fps)))


def washout(values, window: int):
    """High pass a signal by subtracting its own slow average.

    This is the one place washout belongs. A platform driven by raw camera
    movement drifts to its end stops within a minute, because a pan in one
    direction never comes back. Subtracting the slow average means a fast
    movement is felt and a slow one is not, and the platform always returns to
    neutral, which is what a limited travel rig needs.

    ADR 0001 argues washout is unnecessary for *authored* motion, and that
    holds: an author works inside the rig's limits already. Generated motion is
    the exception it named, and this is it.
    """
    if window < 2 or len(values) < window:
        return [0.0] * len(values)
    slow = smooth(values, window)
    return [v - s for v, s in zip(values, slow)]


def pose_series(movements, fps: float, width: int = 64, height: int = 36,
                washout_seconds: float = 4.0, gain: float = 1.0):
    """Turn apparent camera movement into 6DOF pose.

    The mapping is deliberately literal about what can and cannot be known:

      yaw    <- horizontal movement. A pan *is* a yaw.
      pitch  <- vertical movement. A tilt is a pitch.
      heave  <- vertical movement again, because a fall reads the same way and
                a seat dropping is the effect people actually want from it.
      surge  <- overall speed, as a forward push during fast movement.

      sway and roll are left at zero. Nothing in a single projection pair
      distinguishes a lateral track from a pan, or tells you about roll at all,
      and inventing them would be making things up.

    Everything is washed out and scaled to a unit range. The instrument clamps
    to the rig's declared travel afterwards, so these are intentions rather
    than commands.
    """
    if not movements:
        return []

    win = max(2, int(washout_seconds * fps))
    dxs = washout([m.dx for m in movements], win)
    dys = washout([m.dy for m in movements], win)
    speeds = smooth([m.speed for m in movements], max(2, int(fps)))

    peak_speed = max(speeds) if speeds else 0.0
    out = []
    for i in range(len(movements)):
        # Normalised by half the frame, so a movement of half a frame per
        # sample is full deflection. Anything faster is already extreme.
        yaw = clamp(dxs[i] / (width / 2.0) * gain)
        pitch = clamp(dys[i] / (height / 2.0) * gain)
        heave = clamp(-dys[i] / (height / 2.0) * gain)
        surge = clamp((speeds[i] / peak_speed) * gain) if peak_speed > 0 else 0.0
        out.append({
            "surge": round(surge * 0.6, 4),
            "sway": 0.0,
            "heave": round(heave, 4),
            "roll": 0.0,
            "pitch": round(pitch, 4),
            "yaw": round(yaw, 4),
        })
    return out


def clamp(v: float) -> float:
    return max(-1.0, min(1.0, v))


def wind_series(movements, fps: float, smooth_seconds: float = 1.5):
    """Wind from apparent speed.

    A fast moving camera is the closest thing an image has to airflow, and it
    is what a fan can honestly react to. Smoothed hard, because a fan takes
    over a second to change speed and driving it from a twitchy signal just
    wastes the movement.
    """
    if not movements:
        return []
    speeds = smooth([m.speed for m in movements], max(2, int(smooth_seconds * fps)))
    peak = max(speeds) if speeds else 0.0
    if peak <= 0:
        return [0.0] * len(speeds)
    return [round(v / peak, 4) for v in speeds]


# The axes a three actuator platform can actually produce. Three linear
# actuators under a triangle give exactly these and nothing else, which is what
# almost every buildable home platform is.
DOF3_AXES = ("heave", "roll", "pitch")

# How much of a sustained push to render as a backward tilt.
#
# The standard motion cueing trick, and it is not a compromise: below the tilt
# rate threshold the vestibular system cannot distinguish a tilt from a linear
# acceleration, so a seat pitched back *is* how a platform with centimetres of
# travel conveys accelerating forward. A full six axis rig does the same thing
# for sustained acceleration, because it runs out of travel too.
TILT_COORDINATION = 0.55


def to_3dof(pose):
    """Fold a six axis pose onto the three a real platform has.

    Surge becomes a backward pitch and sway becomes a roll, rather than being
    discarded: the information is rendered through the axes that exist instead
    of being thrown away. Yaw is dropped outright — a pan is something the
    camera looked at, not a motion a seated person feels, and a three actuator
    platform cannot yaw at all.

    Worth knowing what this costs, which is less than it sounds: of the six
    axes, this analysis already leaves sway and roll at zero always, because
    nothing in a single projection pair distinguishes a lateral track from a
    pan. Three of the six were extrapolation rather than measurement.
    """
    out = []
    for p in pose:
        pitch = clamp(p.get("pitch", 0.0) - p.get("surge", 0.0) * TILT_COORDINATION)
        roll = clamp(p.get("roll", 0.0) + p.get("sway", 0.0) * TILT_COORDINATION)
        out.append({
            "heave": round(p.get("heave", 0.0), 4),
            "roll": round(roll, 4),
            "pitch": round(pitch, 4),
        })
    return out
