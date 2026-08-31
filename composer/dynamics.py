"""Calm, contrast, and the rest budget.

The most common failure of an automatically scored film is not a missed effect.
It is that everything is scored, all the time, and after twenty minutes the
audience stops noticing any of it. Being shaken continuously for two hours is
not immersive, it is tiring.

So this module does something a detector cannot: it decides what *not* to play.

Two mechanisms, and they are different:

  **Calm regions are protected.** Stretches the film itself makes quiet stay
  quiet. Silence is a thing the score contains, not an absence of score.

  **A rest budget caps density everywhere else.** Even in a busy sequence, only
  so much of any window may be active. What survives the cap is the peaks,
  which is precisely what makes a peak feel like one.

Both drop cues. That is the point. A generator that only adds is a generator
that produces mush.
"""

from __future__ import annotations

# Effects worth interrupting calm for. A thunderclap in a silent scene is the
# whole reason the scene was silent; suppressing it would be exactly backwards.
LOUD_ACTIONS = frozenset({"hit", "flash", "explosion", "thunder"})

# Kinds a calm stretch has no opinion about.
#
# Calm exists so a quiet scene is not shaken or flashed through. A smell is not
# intrusive in that way, and the scenes worth a scent are very often the calm
# ones — a forest, a church, a kitchen. Dropping those was the pass working as
# designed and the design being wrong about one kind.
UNGATED_KINDS = frozenset({"scent"})


def normalise(values):
    """Scale to 0..1 by the maximum, which keeps a quiet film usable.

    Absolute calibration is the rig's business, not the composer's: the author
    sets overall intensity once, and everything here is relative to the film.
    """
    if not values:
        return []
    peak = max(values)
    if peak <= 0:
        return [0.0] * len(values)
    return [v / peak for v in values]


def activity(audio=None, speed=None, cuts=None, fps: float = 4.0,
             duration: float = 0.0, weights=(0.5, 0.35, 0.15)):
    """Combine signals into one activity level per sample.

    Three sources, weighted: how loud the low end is, how fast the image is
    moving, and how often the film is cutting. Cutting rate matters because a
    rapidly cut sequence is busy even when it is quiet, and a long take is calm
    even when it is loud.
    """
    # Audio arrives already peak normalised over the whole film, so
    # normalising again would only destroy scale. Speed is in arbitrary
    # units and does need it.
    #
    # This matters more than it looks: normalise divides by the peak, so a
    # uniformly quiet signal comes back uniformly *maximum*. Doing it twice
    # turned a silent stretch into the busiest thing in the film.
    audio = list(audio or [])
    speed = normalise(speed or [])
    n = max(len(audio), len(speed))
    if n == 0 and duration > 0:
        n = int(duration * fps)
    if n == 0:
        return []

    cut_density = [0.0] * n
    if cuts:
        # A cut contributes to the second either side of it, so the measure is
        # a rate rather than a set of spikes.
        for at in cuts:
            centre = int(at * fps)
            for i in range(max(0, centre - int(fps)), min(n, centre + int(fps) + 1)):
                cut_density[i] += 1.0
        cut_density = normalise(cut_density)

    wa, ws, wc = weights
    out = []
    for i in range(n):
        a = audio[i] if i < len(audio) else 0.0
        s = speed[i] if i < len(speed) else 0.0
        c = cut_density[i]
        out.append(wa * a + ws * s + wc * c)
    return out


def calm_regions(levels, fps: float, threshold: float = 0.18,
                 min_seconds: float = 12.0, merge_gap: float = 4.0):
    """Find stretches the film itself keeps quiet.

    A region has to last a while to count. Two seconds of quiet between two
    explosions is a breath, not a calm scene, and protecting it would only
    stop the second explosion landing.
    """
    if not levels:
        return []
    min_len = max(1, int(min_seconds * fps))

    runs = []
    start = None
    for i, v in enumerate(levels):
        if v <= threshold and start is None:
            start = i
        elif v > threshold and start is not None:
            if i - start >= min_len:
                runs.append([start, i])
            start = None
    if start is not None and len(levels) - start >= min_len:
        runs.append([start, len(levels)])

    # Merge regions separated by a brief flurry, which is usually one loud
    # moment inside a scene that is otherwise quiet.
    merged = []
    gap = int(merge_gap * fps)
    for run in runs:
        if merged and run[0] - merged[-1][1] <= gap:
            merged[-1][1] = run[1]
        else:
            merged.append(run)
    return [(lo / fps, hi / fps) for lo, hi in merged]


def in_region(at: float, regions) -> bool:
    for lo, hi in regions:
        if lo <= at < hi:
            return True
    return False


def intensity_of(cue) -> float:
    """How loud a cue is, for deciding what to drop first."""
    params = cue.get("params") or {}
    if not params:
        return 0.5
    return max(abs(v) for v in params.values())


def kind_of(cue) -> str:
    return str(cue.get("instrument") or "").split(".")[0]


def protect_calm(cues, regions, keep_above: float = 0.75):
    """Drop cues inside calm regions, except the ones worth interrupting for.

    Returns (kept, dropped). A thunderclap in a silent scene survives; a gentle
    ambient rumble does not, because the rumble is what turns a quiet scene into
    an averagely busy one.

    A scent survives regardless. See UNGATED_KINDS: calm is about not shaking
    and not flashing, and the scenes most worth a smell are the quiet ones.
    """
    kept, dropped = [], []
    for cue in cues:
        if kind_of(cue) in UNGATED_KINDS:
            kept.append(cue)
            continue
        if not in_region(cue["t"], regions):
            kept.append(cue)
            continue
        if cue.get("action") in LOUD_ACTIONS and intensity_of(cue) >= keep_above:
            kept.append(cue)
        else:
            dropped.append(cue)
    return kept, dropped


def enforce_budget(cues, window: float = 120.0, max_active: float = 0.25):
    """Cap how much of any window may be spent doing something.

    Walks the film keeping a running total of effect duration inside a sliding
    window. When a cue would push the window over budget, the quietest cue in
    that window is dropped rather than the newest: what should survive a cap is
    the peaks, not whatever happened to come first.

    Returns (kept, dropped).
    """
    ordered = sorted(cues, key=lambda c: c["t"])
    kept, dropped = [], []
    for cue in ordered:
        kept.append(cue)
        lo = cue["t"] - window
        inside = [c for c in kept if c["t"] >= lo]
        busy = sum(c.get("duration", 1.0) for c in inside)
        while busy > window * max_active and len(inside) > 1:
            weakest = min(inside, key=intensity_of)
            kept.remove(weakest)
            inside.remove(weakest)
            dropped.append(weakest)
            busy = sum(c.get("duration", 1.0) for c in inside)
    return kept, dropped
