"""Analysing part of a film instead of all of it.

A feature takes tens of minutes to analyse and the run is worth nothing until
it finishes, so the studio cuts one into pieces it can record as they land. A
piece is a time range rather than a slice of file: the prepared copy of a
feature runs to gigabytes, and writing a second copy of it to disk in order to
read it in pieces costs more than the problem is worth.

Two things make a range more than an ffmpeg seek.

The first is warmup. Motion is measured by comparing each frame to the one
before it, so the first frame of a range has nothing to compare against and
reports stillness that is not there. A range therefore decodes from a little
before it starts and throws away what it produced before its own beginning.

The second is that the result is stated in the film's own time, not the
range's. A partial score whose cues are at 0s when they belong at 47 minutes
is a thing you have to remember to correct; one that says 47 minutes is just a
score that happens to be short. Everything downstream — merging, reading it by
hand, opening it in the studio — gets simpler for it.
"""

from __future__ import annotations

# Enough to settle the frame-to-frame comparisons that motion is built from,
# and short enough that the cost is noise against a chunk of several minutes.
DEFAULT_WARMUP = 2.0


class Span:
    """The part of a film to analyse, in seconds of the film's own time."""

    __slots__ = ("start", "end", "warmup")

    def __init__(self, start: float = 0.0, end: float = 0.0, warmup: float = 0.0):
        self.start = max(0.0, float(start or 0.0))
        # Zero end means "to the end of the film". A range that has to know the
        # duration in order to say "all of it" makes every caller find the
        # duration first, and the last chunk of a film is exactly the case
        # where the duration is least worth trusting to the nearest frame.
        self.end = max(0.0, float(end or 0.0))
        self.warmup = max(0.0, float(warmup or 0.0))

    @property
    def whole(self) -> bool:
        """Is this the whole film, so nothing need be seeked or trimmed."""
        return self.start <= 0.0 and self.end <= 0.0

    @property
    def decode_start(self) -> float:
        """Where decoding begins, which is before the range when warming up."""
        return max(0.0, self.start - self.warmup)

    @property
    def lead(self) -> float:
        """How much of what is decoded comes before the range and is dropped.

        Not simply the warmup: a range starting at 1s with a 2s warmup can only
        be given 1s of lead, because the film does not start earlier than it
        starts.
        """
        return self.start - self.decode_start

    @property
    def decode_duration(self) -> float:
        """How much to decode, or 0 for everything from decode_start on."""
        if self.end <= 0.0:
            return 0.0
        return max(0.0, self.end - self.decode_start)

    def input_args(self) -> list[str]:
        """ffmpeg arguments placing the range, to go before -i.

        Before -i on purpose: that is the fast seek, which jumps by index
        rather than decoding and discarding everything up to the point. On a
        two hour film the difference is the whole feature.
        """
        args: list[str] = []
        if self.decode_start > 0.0:
            args += ["-ss", "%.3f" % self.decode_start]
        if self.decode_duration > 0.0:
            args += ["-t", "%.3f" % self.decode_duration]
        return args

    def to_film_time(self, t: float) -> float:
        """A time within what was decoded, as a time in the film."""
        return self.decode_start + t

    def contains(self, t: float) -> bool:
        """Is this film time inside the range proper, ignoring the lead."""
        if t < self.start - 1e-9:
            return False
        if self.end > 0.0 and t > self.end + 1e-9:
            return False
        return True


def _time_of(item):
    """The time of a cue or of a curve point.

    The two are not the same shape and deliberately stay that way: a cue is a
    dict because it carries an action, parameters and a duration, while a
    point is a bare (time, values) pair because a curve is millions of them and
    a dict per point is a cost paid on every film. This is the one place that
    has to know both.
    """
    if isinstance(item, dict):
        return float(item.get("t", 0.0))
    return float(item[0])


def _with_time(item, t: float):
    """The same cue or point, at a different time."""
    if isinstance(item, dict):
        moved = dict(item)
        moved["t"] = t
        return moved
    return (t,) + tuple(item[1:])


def place(tracks, span: Span):
    """Move a chunk's tracks into the film's own time, dropping the lead in.

    Returns new tracks; the input is left alone. Cues and points are handled
    the same way because they are the same problem — a time and a payload —
    and the only difference is how each stores it.

    A track that is emptied by the trim is dropped rather than kept empty. An
    empty track and no track say the same thing about what the film does, and
    merging is simpler when nothing has to distinguish them.

    Curves get one thing cues do not: a point at the exact moment the range
    begins, carrying whatever value the curve already held. Compression runs
    before the trim and keeps a point only where the signal changed, so a curve
    that was steady across the boundary can easily have no point between the
    range's start and some moment well inside it — measured at seventeen
    seconds on the first real chunk. Without the holding point the merged curve
    ramps from the previous chunk's last value to that one, inventing a slow
    slide through a stretch the film spent perfectly still. A cue gets no such
    treatment, because moving an event to the boundary would be reporting
    something that did not happen there.
    """
    if span.whole and span.lead <= 0.0:
        return tracks

    out = []
    for track in tracks:
        moved = dict(track)
        hold = track.get("type") == "curve"
        for field in ("cues", "points"):
            if field not in track:
                continue
            kept = []
            before = None
            for item in track[field]:
                t = span.to_film_time(_time_of(item))
                if not span.contains(t):
                    # Remember the last one dropped for being early; it is what
                    # the curve was holding when the range began.
                    if t < span.start:
                        before = item
                    continue
                if hold and not kept and before is not None and t > span.start:
                    kept.append(_with_time(before, round(span.start, 4)))
                kept.append(_with_time(item, round(t, 4)))
            if hold and not kept and before is not None:
                # Nothing at all inside the range, but the curve still has a
                # value there. One point is what says so.
                kept.append(_with_time(before, round(span.start, 4)))
            moved[field] = kept
        if any(moved.get(f) for f in ("cues", "points")):
            out.append(moved)
    return out


def place_regions(regions, span: Span):
    """The same move for calm regions, which are pairs rather than points.

    A region is clipped to the range rather than dropped when it straddles the
    boundary: a calm stretch that runs across a chunk edge is one calm stretch,
    and the merge rejoins the halves. Dropping it would report the film as
    busier at every boundary than it is.
    """
    if span.whole and span.lead <= 0.0:
        return regions

    out = []
    lo_limit = span.start
    hi_limit = span.end if span.end > 0.0 else float("inf")
    for lo, hi in regions:
        a = max(span.to_film_time(lo), lo_limit)
        b = min(span.to_film_time(hi), hi_limit)
        if b > a:
            out.append((round(a, 4), round(b, 4)))
    return out
