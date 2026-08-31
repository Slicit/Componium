"""Deciding which half of a film to leave alone.

The third pass. It reads a score and the description beside it, works out how
busy each moment is, and quiets the ones that are not — so it costs no decode
and can be run again with a different budget in the time it takes to read a
file.

Three things it is built around, each of them measured rather than assumed.

Nothing is asked to invent activity. The budget is a floor on calm, never a
target: a film that is already quieter than the floor is left exactly as it is.
A rig that adds movement to a still film is the fault this whole pass exists to
correct, and it would be a poor joke to introduce it here.

No single signal can be trusted. The audio discriminates well on a film and
badly on a music video, where the music never stops; the camera does the
reverse. Measured across sintel and crab rave the two disagree about which is
the reliable one, so the score is a blend of three opinions — the audio, the
camera, and a model that was asked outright — and the ranking that follows only
needs the blend to be monotonic, not calibrated.

Calm means still, not silent. Asked what calm meant, the answer was "no shake,
no vibration" — so this quiets the shake hard and leaves a slow tilt alone. A
quiet scene with a gentle drift is fine; a quiet scene that buzzes is not.
"""

from __future__ import annotations

import compose

# How long a stretch has to be before it is worth calling calm.
#
# The model's verdict flickers: measured on sintel at one reading every two
# seconds, it changed its mind 113 times with a median run of four seconds.
# Acting on that would switch the platform off and on again every few seconds,
# which is worse than either answer.
MIN_CALM_SECONDS = 8.0


# What a busy stretch is allowed to have more of.
#
# Shake and wind, because those are what "this scene is bigger" feels like from
# a seat. Not light: a flash is already at the level it was measured to be and
# blowing one out is unpleasant rather than exciting. Not fog, mist or scent —
# those are dosed rather than dimmed, and a fogger asked for forty per cent more
# does not produce a slightly bigger scene, it produces a room somebody has to
# leave.
BOOSTED = ("shake", "wind")

# How much more, at the height of it.
#
# Modest, and clamped to one afterwards. Everything here is normalised to a rig
# that has declared what it can survive, so the arithmetic is asking for more of
# what was already allowed rather than for something new — but a proposal that
# asks for more than one is a proposal that gets clipped somewhere less careful.
BOOST = 1.35

# How much of a film may be lifted.
#
# The same shape as the calm budget and for the same reason: a threshold on a
# signal nobody has calibrated moves with the film, and a share does not. A
# sixth is enough for the two or three sequences a feature is built around.
BOOST_SHARE = 0.15

# A dip shorter than this does not end a sequence.
#
# A fight drops below the cut for a beat when the camera cuts to a reaction,
# which fragments the top of the ranking into pieces that are each too short to
# keep. Joining across the dip finds the sequence; raising the share until the
# fragments are long enough finds the whole film.
BUSY_GAP = 8.0

# A stretch shorter than this is not a sequence.
#
# Longer than the calm minimum, because lifting for eight seconds and dropping
# again is a lurch, where calming for eight seconds is merely a rest.
MIN_BUSY_SECONDS = 20.0

# How long the gate takes to open and close, in seconds.
#
# A curve snapped to zero at a boundary is its own event — the absence of a
# jolt announced with a jolt. Long enough not to be felt as a step, short
# enough not to eat the quiet stretch it is protecting.
RAMP_SECONDS = 0.8

# What the blend weighs. The model gets the largest share because it is the
# only one of the three that was asked the actual question; the other two are
# measurements of things that correlate with it.
WEIGHT_MODEL = 0.5
WEIGHT_AUDIO = 0.25
WEIGHT_CAMERA = 0.25

# The floor on calm, and the range it may move in.
#
# Half a film is the guideline. An action film may keep more of itself busy and
# a story film less, so the floor slides with what the film turns out to be —
# but only between these, because a film that never rests is the complaint and
# a film that never moves is not worth wiring a rig for.
FLOOR_MIN = 0.40
FLOOR_MAX = 0.60
# The active fractions those floors correspond to. Below the first a film is
# story; above the second it is action.
STORY_ACTIVE = 0.15
ACTION_ACTIVE = 0.45


def clamp01(v: float) -> float:
    return max(0.0, min(1.0, v))


def level_at(points, at: float) -> float:
    """The strongest channel of a curve at a moment, held at the ends."""
    if not points:
        return 0.0
    if at <= points[0][0]:
        values = points[0][1]
    elif at >= points[-1][0]:
        values = points[-1][1]
    else:
        values = points[-1][1]
        for i in range(1, len(points)):
            if points[i][0] >= at:
                t0, v0 = points[i - 1]
                t1, v1 = points[i]
                f = 0.0 if t1 == t0 else (at - t0) / (t1 - t0)
                values = {k: v0.get(k, 0.0) + (v1.get(k, 0.0) - v0.get(k, 0.0)) * f
                          for k in v0}
                break
    return max((abs(v) for v in values.values()), default=0.0)


def activity(observations, audio=None, camera=None):
    """How busy the film is, one reading per observation.

    Returns (time, score) pairs with the score in 0..1. The absolute value
    means little — what the budget uses is the order.
    """
    out = []
    for o in observations:
        at = float(o["t"])
        said = 1.0 if "scene-active" in (o.get("labels") or []) else 0.0
        a = clamp01(level_at(audio or [], at))
        c = clamp01(level_at(camera or [], at))
        out.append((at, clamp01(WEIGHT_MODEL * said
                                + WEIGHT_AUDIO * a
                                + WEIGHT_CAMERA * c)))
    return out


# How long a window the activity is averaged over before anything is decided.
#
# The model changes its mind constantly. On sintel, read every two seconds, it
# changed 113 times across fifteen minutes with a median run of four seconds,
# and most of that is not the film changing but one frame of a shot looking
# unlike the next. Ranking the raw readings put half the film below the
# threshold and still produced stretches too short to act on: fifty per cent of
# the readings came out as twenty-nine per cent of the time. Averaging first is
# what turns a flicker into a scene.
SMOOTH_SECONDS = 14.0


def smooth_scores(scores, seconds: float = SMOOTH_SECONDS):
    """Average the activity over a window, so a scene reads as a scene.

    A centred rolling mean, shortened at the ends rather than padded. Padding
    with zeros would invent calm at the start and end of every film, which is
    where a title card and a credit roll already are — the one place a wrong
    answer would be least noticed and most wrong.
    """
    rows = list(scores)
    if len(rows) < 2 or seconds <= 0:
        return rows
    step = rows[1][0] - rows[0][0]
    if step <= 0:
        return rows
    half = max(1, int(seconds / step / 2))
    out = []
    for i, (at, _) in enumerate(rows):
        lo = max(0, i - half)
        hi = min(len(rows), i + half + 1)
        window = [v for _, v in rows[lo:hi]]
        out.append((at, sum(window) / len(window)))
    return out


def active_fraction(observations) -> float:
    """How much of the film the model called active."""
    rows = list(observations)
    if not rows:
        return 0.0
    said = sum(1 for o in rows if "scene-active" in (o.get("labels") or []))
    return said / len(rows)


def floor_for(observations) -> float:
    """How much of this film must end up calm.

    Slides with what the film turns out to be, between the two bounds. An
    action film is allowed to keep more of itself busy; a story film less.
    """
    active = active_fraction(observations)
    if active <= STORY_ACTIVE:
        return FLOOR_MAX
    if active >= ACTION_ACTIVE:
        return FLOOR_MIN
    span = (active - STORY_ACTIVE) / (ACTION_ACTIVE - STORY_ACTIVE)
    return FLOOR_MAX - span * (FLOOR_MAX - FLOOR_MIN)


def threshold_for(scores, floor: float) -> float:
    """The activity below which a moment is a candidate for being quieted.

    Taken by ranking rather than by an absolute level, which is what makes it
    survive a signal being miscalibrated: it only has to put the moments in the
    right order, not agree with anything about what 0.4 means.
    """
    values = sorted(v for _, v in scores)
    if not values:
        return 0.0
    index = min(len(values) - 1, max(0, int(len(values) * floor)))
    return values[index]


def regions(scores, threshold: float, min_seconds: float = MIN_CALM_SECONDS):
    """The stretches quiet enough, and long enough, to be worth calming.

    A stretch shorter than the minimum is discarded rather than kept, because
    switching a platform off for four seconds and on again is more noticeable
    than leaving it running.
    """
    out = []
    start = None
    last = None
    for at, value in scores:
        if value <= threshold:
            if start is None:
                start = at
        else:
            if start is not None and last is not None and last - start >= min_seconds:
                out.append((start, last))
            start = None
        last = at
    if start is not None and last is not None and last - start >= min_seconds:
        out.append((start, last))
    return out


def covered(calm) -> float:
    """How many seconds a set of stretches covers."""
    return sum(hi - lo for lo, hi in calm)


def threshold_for_time(scores, floor: float,
                       min_seconds: float = MIN_CALM_SECONDS) -> float:
    """The threshold that leaves the wanted fraction of the film calm.

    Ranking the readings is not enough, and the difference is not small: half
    the readings of sintel fall below the median, but they are scattered, and
    only the ones that form a long enough run become a stretch worth acting on
    — so half the readings bought twenty-nine per cent of the film. The rule is
    about time, so the search is over time.

    Walks up the thresholds the readings actually take, and stops at the first
    that pays for the floor. Returns the highest tried if none does, because a
    film too fragmented to reach its floor should still be quieted as much as
    it can be rather than not at all.
    """
    if not scores:
        return 0.0
    span = scores[-1][0] - scores[0][0]
    if span <= 0:
        return 0.0
    want = span * floor

    candidates = sorted({v for _, v in scores})
    best = candidates[0]
    for level in candidates:
        best = level
        if covered(regions(scores, level, min_seconds)) >= want:
            break
    return best


def gate_at(at: float, calm, ramp: float = RAMP_SECONDS) -> float:
    """How much of the original signal survives at a moment: 1 outside, 0 in.

    Ramped at the edges, because a curve snapped to zero at a boundary is its
    own event.
    """
    keep = 1.0
    for lo, hi in calm:
        if at <= lo - ramp or at >= hi + ramp:
            continue
        if lo <= at <= hi:
            return 0.0
        if at < lo:
            keep = min(keep, (lo - at) / ramp)
        else:
            keep = min(keep, (at - hi) / ramp)
    return clamp01(keep)


def quiet(points, calm, ramp: float = RAMP_SECONDS):
    """Bring a curve to rest inside the calm stretches.

    Points are added at the edges of every ramp so the shape is the gate's
    rather than whatever the curve happened to be doing nearby — without them
    a stretch with no points near its boundary fades over minutes instead of
    over the ramp.
    """
    if not points or not calm:
        return list(points)

    edges = set()
    for lo, hi in calm:
        for at in (lo - ramp, lo, hi, hi + ramp):
            if points[0][0] <= at <= points[-1][0]:
                edges.add(round(at, 3))

    have = {round(t, 3) for t, _ in points}
    out = []
    for at in sorted(have | edges):
        if at in have:
            value = next(v for t, v in points if round(t, 3) == at)
        else:
            # Sampled from the curve as it stands, so inserting an edge does
            # not itself change the shape before the gate is applied.
            value = _sample(points, at)
        g = gate_at(at, calm, ramp)
        out.append((at, {k: round(v * g, 4) for k, v in value.items()}))
    return out


def loud_at(scores, cut: float, min_seconds: float = MIN_BUSY_SECONDS,
            gap: float = BUSY_GAP):
    """The stretches at or above a level, joined across dips and long enough.

    A dip is not an ending: a fight drops below any cut for a beat when the
    camera cuts to a reaction, and judging the pieces separately throws away
    the sequence they belong to.
    """
    raw = []
    start = last = None
    for at, value in scores:
        if value >= cut:
            if start is None:
                start = at
        else:
            if start is not None and last is not None:
                raw.append((start, last))
            start = None
        last = at
    if start is not None and last is not None:
        raw.append((start, last))

    merged = []
    for lo, hi in raw:
        if merged and lo - merged[-1][1] <= gap:
            merged[-1] = (merged[-1][0], hi)
        else:
            merged.append((lo, hi))
    return [(lo, hi) for lo, hi in merged if hi - lo >= min_seconds]


def busy(scores, share: float = BOOST_SHARE,
         min_seconds: float = MIN_BUSY_SECONDS, gap: float = BUSY_GAP):
    """The stretches worth giving more to, and long enough to be worth it.

    The mirror of threshold_for_time(): the same search over time, from the
    other end. A share of the film rather than a threshold, because a threshold
    on a signal nobody has calibrated moves with the film and a share does not
    — which is the property that made the calm pass survive a signal that
    turned out to be measuring the wrong thing.
    """
    rows = [r for r in scores if r]
    if not rows or share <= 0:
        return []
    span = rows[-1][0] - rows[0][0]
    if span <= 0:
        return []
    want = span * share

    # Down through the levels the readings actually take, stopping at the first
    # that buys the wanted time. The share is of the film, not of the readings:
    # readings above a cut are scattered and only the runs long enough to be a
    # sequence survive, so a sixth of the readings bought nothing at all.
    out = []
    for level in sorted({v for _t, v in rows}, reverse=True):
        if level <= 0:
            break
        found = loud_at(rows, level, min_seconds, gap)
        out = found
        if sum(hi - lo for lo, hi in found) >= want:
            break
    return out




def boost_at(at: float, loud, gain: float = BOOST, ramp: float = RAMP_SECONDS) -> float:
    """How much to multiply by at a moment: 1 outside, `gain` inside.

    Ramped at the edges like the gate, because a curve stepped up at a boundary
    is its own event — and a step in a platform is one a body notices more than
    a step in a light.
    """
    lift = 1.0
    for lo, hi in loud:
        if at <= lo - ramp or at >= hi + ramp:
            continue
        if lo <= at <= hi:
            return gain
        near = (lo - at) / ramp if at < lo else (at - hi) / ramp
        lift = max(lift, 1.0 + (gain - 1.0) * (1.0 - clamp01(near)))
    return lift


def lift(points, loud, gain: float = BOOST, ramp: float = RAMP_SECONDS):
    """Give a curve more of itself inside the loud stretches.

    Points at the ramp edges for the same reason quiet() adds them: without
    one, a stretch with no points near its boundary ramps over minutes instead
    of over the ramp.

    Clamped to one. A curve is normalised to what a rig said it can do, and
    asking for more than that is asking somebody else's clamp to decide.
    """
    if not points or not loud:
        return list(points)

    edges = set()
    for lo, hi in loud:
        for at in (lo - ramp, lo, hi, hi + ramp):
            if points[0][0] <= at <= points[-1][0]:
                edges.add(round(at, 3))

    have = {round(t, 3) for t, _ in points}
    out = []
    for at in sorted(have | edges):
        if at in have:
            value = next(v for t, v in points if round(t, 3) == at)
        else:
            value = _sample(points, at)
        g = boost_at(at, loud, gain, ramp)
        out.append((at, {k: round(min(1.0, v * g), 4) for k, v in value.items()}))
    return out


def _sample(points, at: float) -> dict:
    if at <= points[0][0]:
        return dict(points[0][1])
    if at >= points[-1][0]:
        return dict(points[-1][1])
    for i in range(1, len(points)):
        if points[i][0] >= at:
            t0, v0 = points[i - 1]
            t1, v1 = points[i]
            f = 0.0 if t1 == t0 else (at - t0) / (t1 - t0)
            return {k: v0.get(k, 0.0) + (v1.get(k, 0.0) - v0.get(k, 0.0)) * f for k in v0}
    return dict(points[-1][1])


def inside(at: float, calm) -> bool:
    return any(lo <= at <= hi for lo, hi in calm)


# Which kinds go quiet, and which are left alone.
#
# Asked what calm meant, the answer was "no shake, no vibration". So the shake
# is silenced and a slow tilt is not: a quiet scene with a gentle drift is
# fine, a quiet scene that buzzes is not. The motion platform is left running
# because a sustained tilt costs the audience nothing and is often the reason
# the scene reads as calm rather than dead.
QUIETED = ("shake",)
# A cue loud enough to be worth interrupting a quiet stretch for. A thunderclap
# in a silent scene survives; an ambient rumble does not, because the rumble is
# what turns a quiet scene into an averagely busy one.
KEEP_ABOVE = 0.75


def kind_of(instrument: str) -> str:
    return str(instrument or "").split(".")[0]


def loudness(cue: dict) -> float:
    values = [abs(float(v)) for v in (cue.get("params") or {}).values()]
    return max(values) if values else 0.0


def apply_to(score: dict, calm, ramp: float = RAMP_SECONDS, loud=()) -> dict:
    """Quiet a score inside the calm stretches, lift it inside the loud ones.

    Returns a new score. Both sets of regions are written into it as well as
    acted on, because a reviewer looking at a quiet stretch wants to know
    whether the rig is quiet because the film is or because nothing was found —
    and the same question is worth asking of a stretch that is louder than the
    one before it.
    """
    import remap

    out = dict(score)
    tracks = []
    for track in score.get("track") or []:
        fresh = dict(track)
        kind = kind_of(track.get("instrument"))

        if track.get("type") == "curve" and (kind in QUIETED or kind in BOOSTED):
            points = [(remap.seconds_of(p["t"]), dict(p.get("value") or {}))
                      for p in track.get("points") or []]
            if kind in QUIETED:
                points = quiet(points, calm, ramp)
            if loud and kind in BOOSTED:
                points = lift(points, loud, ramp=ramp)
            fresh["points"] = [
                {"t": compose.timecode(at), "value": value}
                for at, value in points
            ]
        elif track.get("type") == "cue" and kind in QUIETED:
            kept = []
            for cue in track.get("cues") or []:
                at = remap.seconds_of(cue["t"])
                if inside(at, calm) and loudness(cue) < KEEP_ABOVE:
                    continue
                kept.append(cue)
            fresh["cues"] = kept
        tracks.append(fresh)

    out["track"] = [t for t in tracks
                    if t.get("type") != "cue" or t.get("cues")]
    out["calm"] = [{"from": compose.timecode(lo), "to": compose.timecode(hi)}
                   for lo, hi in calm]
    if loud:
        out["loud"] = [{"from": compose.timecode(lo), "to": compose.timecode(hi)}
                       for lo, hi in loud]
    return out


def main(argv=None):
    import argparse
    import os
    import sys

    import remap

    p = argparse.ArgumentParser(
        description="Quiet the parts of a film that are not doing anything.")
    p.add_argument("score", help="a .componium file")
    p.add_argument("-o", "--out", help="where to write (default: stdout)")
    p.add_argument("--seen", help="the description (default: <score>.seen.jsonl)")
    p.add_argument("--floor", type=float, default=0.0,
                   help="how much of the film must end up calm, 0 to 1. "
                        "Left alone, it is chosen from how active the film is.")
    p.add_argument("--smooth", type=float, default=SMOOTH_SECONDS,
                   help="how long a window the activity is averaged over")
    p.add_argument("--boost-share", type=float, default=BOOST_SHARE,
                   help="how much of a film may be lifted, 0 to 1; 0 turns "
                        "the lift off and leaves the pass only taking away "
                        "(default %(default)s)")
    p.add_argument("--min-seconds", type=float, default=MIN_CALM_SECONDS,
                   help="the shortest stretch worth calming")
    p.add_argument("--ramp", type=float, default=RAMP_SECONDS,
                   help="how long the gate takes to open and close")
    p.add_argument("--shake-id", default="shake.seat")
    p.add_argument("--motion-id", default="motion.platform")
    args = p.parse_args(argv)

    seen_path = args.seen or (args.score + ".seen.jsonl")
    if not os.path.exists(seen_path):
        sys.exit("no description beside that score: " + seen_path)

    score = remap.load_score(args.score)
    observations = remap.load_observations(seen_path)
    if not observations:
        sys.exit("that description is empty")

    def curve_of(instrument):
        for t in score.get("track") or []:
            if t.get("instrument") == instrument and t.get("type") == "curve":
                return [(remap.seconds_of(p["t"]), p.get("value") or {})
                        for p in t.get("points") or []]
        return []

    scores = smooth_scores(
        activity(observations,
                 audio=curve_of(args.shake_id),
                 camera=curve_of(args.motion_id)),
        args.smooth)
    floor = args.floor if args.floor > 0 else floor_for(observations)
    threshold = threshold_for_time(scores, floor, args.min_seconds)
    calm = regions(scores, threshold, args.min_seconds)
    # The same ranking read from the other end. A film has a shape, and a pass
    # that only ever holds back leaves every sequence at the level of the scene
    # before it.
    loud = busy(scores, args.boost_share) if args.boost_share > 0 else []

    span = observations[-1]["t"] - observations[0]["t"]
    quieted = sum(hi - lo for lo, hi in calm)
    sys.stderr.write(
        "%.0f%% of the readings are active, so the floor is %.0f%% calm\n"
        % (100 * active_fraction(observations), 100 * floor))
    sys.stderr.write(
        "%d stretches, %.0fs of %.0fs (%.0f%%), longest %.0fs\n"
        % (len(calm), quieted, span, 100 * quieted / span if span else 0,
           max((hi - lo for lo, hi in calm), default=0)))
    if loud:
        lifted = sum(hi - lo for lo, hi in loud)
        sys.stderr.write(
            "%d lifted, %.0fs of %.0fs (%.0f%%), longest %.0fs\n"
            % (len(loud), lifted, span, 100 * lifted / span if span else 0,
               max((hi - lo for lo, hi in loud), default=0)))

    out = apply_to(score, calm, args.ramp, loud)

    text = remap.dump(out)
    if args.out:
        with open(args.out, "w", encoding="utf-8", newline="\n") as f:
            f.write(text)
        sys.stderr.write("wrote " + args.out + "\n")
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
