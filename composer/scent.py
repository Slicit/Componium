"""Which scent a scene calls for, and how rarely to say so.

Scent is not like the other effects and the difference is not a detail. A light
is instant and a fan decays in seconds; a puff hangs in a room for minutes and
cannot be taken back. Two scents inside a minute are not two events, they are
mud — so the interesting problem is not detection but restraint, and most of
this file is about saying no.

It is driven by the scene rather than the frame for the same reason. "There is
fire in this frame" is the wrong question; "this is a burning village for the
next four minutes" is the right one, and only a pass that looks at scenes can
answer it.

The bank is named, not numbered. A score says `smoke` and a rig says which
reservoir holds it, exactly as a score says `light.ambient` and a rig says what
that is on this hardware. A rig without a scent simply does not fire it, which
is the same thing that happens to an instrument it does not have.
"""

from __future__ import annotations

import re

# The bank, in the order it was argued for.
#
# Chosen against 7,591 descriptions the model wrote about this library rather
# than from what sounds evocative, then grouped by how much of a library each
# tier covers. See LOGBOOK/features/feat-two-clocks.md.
#
# The first five are conditions a film is *in* and that read instantly on
# anyone: burning, weather, water, vegetation, underground. The next five are
# genre: combat, machinery, pastoral, and the domestic register that a library
# of two action films cannot see but most films live in. The last five are
# interiors, and they are last partly because they linger longest — resins and
# leather hang about for half an hour and will contaminate the scene after
# next, where citrus and pine have cleared before the shot changes.
NECESSARY = ("smoke", "petrichor", "sea", "pine", "earth")
GENRE = ("gunpowder", "ozone", "grass", "coffee", "citrus")
INTERIOR = ("leather", "incense", "bread", "floral", "spirits")
BANK = NECESSARY + GENRE + INTERIOR

# What puts a scene in each scent.
#
# The same table that was counted against the library, kept in the order the
# bank is in so that a tie goes to the more broadly useful scent. Deliberately
# broad: the question is whether a film is in a place, not whether a word
# appears.
CUES = (
    ("smoke", r"smoke|smoky|burning|burnt|ember|ash|charred|\bfire\b|flame|"
              r"blaze|bonfire|smoulder|wreckage on fire"),
    ("petrichor", r"\brain|raining|downpour|storm|thunderstorm|wet ground|"
                  r"puddle|drizzle|damp street|after the rain"),
    ("sea", r"\bsea\b|ocean|beach|shore|waves|surf|coastal|harbou?r|\btide\b|"
            r"seaside|cliffs above the"),
    ("pine", r"forest|woods|woodland|\bpine\b|jungle|bamboo|among trees|"
             r"canopy|undergrowth"),
    ("earth", r"cave|cavern|\bsoil\b|\bmud\b|\bdirt\b|underground|tunnel|"
              r"quarry|rubble|freshly dug|grave|mine\b"),
    ("gunpowder", r"gunfire|rifle|firing|gunshot|shoot-?out|battlefield|"
                  r"trench|artillery|musket|shell"),
    ("ozone", r"machinery|engine room|factory|foundry|forge|circuit|"
              r"electrical|generator|welding|sparks fly"),
    ("grass", r"meadow|\blawn\b|pasture|\bhay\b|farmland|wheat|field of|"
              r"cut grass|grassland"),
    ("coffee", r"coffee|caf[eé]|espresso|diner|breakfast table"),
    ("citrus", r"market stall|greengrocer|orchard|orange|lemon|citrus|"
               r"fruit stall"),
    ("leather", r"saddle|stable|tack room|leather|car interior|cockpit"),
    ("incense", r"temple|church|shrine|altar|cathedral|monk|ritual|"
                r"candlelit|incense|chapel"),
    ("bread", r"kitchen|bakery|baking|\bbread\b|cooking|banquet|feast|"
              r"dining hall|stove"),
    ("floral", r"flowers|blossom|garden|petals|bouquet|floral|greenhouse|"
               r"orchard in bloom"),
    ("spirits", r"tavern|saloon|\bpub\b|\bbar\b|whisk|wine cellar|"
                r"drinking|bottle of"),
)
MATCH = tuple((name, re.compile(pat, re.I)) for name, pat in CUES)

# How long a puff owns the room, in seconds.
#
# Not a guess about hardware but about noses. A scent needs a couple of minutes
# to be noticed, enjoyed and forgotten, and a second one inside that window
# does not read as a second event — it reads as the first one having been
# wrong. Four minutes is deliberately long: the failure that matters here is
# too many, and there is no way to undo one.
LINGER_SECONDS = 240.0

# How long a scene has to hold before it earns a scent at all.
#
# A film passes through a kitchen on the way to somewhere else. Ten seconds of
# forest between two rooms is not a forest, and a fogger firing at it is worse
# than one that stayed quiet.
HOLD_SECONDS = 45.0


def choose(text: str, bank=BANK):
    """The scent a description calls for, or None.

    First match in bank order, so a burning village is smoke rather than earth
    — the earlier entries are the ones that carry more of a library, and a
    scene that is two things at once should smell of the stronger.
    """
    if not text:
        return None
    allowed = set(bank)
    for name, rx in MATCH:
        if name in allowed and rx.search(text):
            return name
    return None


# How many samples either side vote on what a moment smells of.
#
# The scene pass flickers the way the frame pass does — a battle has a shot of
# a village in it, and "not enough information" is a fair answer to a frame of
# smoke. Two either side is fifty seconds at the rate the pass runs, which is
# long enough to outvote a stray shot and short enough not to smear one place
# into the next.
VOTE = 2


def dominant(picked, votes: int = VOTE):
    """Each moment replaced by what its neighbourhood mostly is.

    A plurality, and it has to be a real one: a window with no scent in the
    majority stays empty rather than taking the first thing it saw.
    """
    out = []
    for i, (at, _scent) in enumerate(picked):
        lo = max(0, i - votes)
        hi = min(len(picked), i + votes + 1)
        window = [s for _t, s in picked[lo:hi] if s]
        if not window:
            out.append((at, None))
            continue
        best = max(set(window), key=lambda s: (window.count(s), -BANK.index(s)))
        # Half the window, so a place that is merely present does not win.
        out.append((at, best if window.count(best) * 2 >= (hi - lo) else None))
    return out


def scenes(observations, bank=BANK, hold: float = HOLD_SECONDS):
    """Stretches of film that smell of one thing.

    Takes the scene pass's records — each a time and some words about where it
    is — decides each moment by what its neighbours mostly say, and joins the
    agreeing ones. A stretch shorter than `hold` is dropped: passing through
    somewhere is not being somewhere.

    The vote is what makes this work on a real film. Requiring consecutive
    agreement found nothing at all on a battle sequence, because one shot in
    the middle of it is a village.

    Returns (start, end, scent) in time order.
    """
    picked = []
    for o in observations or []:
        at = float(o.get("t", 0.0))
        said = " ".join(str(o.get(k) or "") for k in ("place", "doing", "seen"))
        picked.append((at, choose(said, bank)))
    picked.sort()
    picked = dominant(picked)

    out = []
    run_from = run_scent = None
    last = None
    for at, scent in picked:
        if scent != run_scent:
            if run_scent is not None and last is not None:
                out.append((run_from, last, run_scent))
            run_from, run_scent = at, scent
        last = at
    if run_scent is not None and last is not None:
        out.append((run_from, last, run_scent))

    # A single sample is a stretch of zero length, which no amount of holding
    # will save; give it the gap to its neighbour so a real but brief scene is
    # judged on its own terms rather than on a rounding.
    step = 0.0
    if len(picked) > 1:
        gaps = [b[0] - a[0] for a, b in zip(picked, picked[1:]) if b[0] > a[0]]
        step = min(gaps) if gaps else 0.0
    return [(a, b + step, s) for a, b, s in out if (b + step) - a >= hold]


def ration(stretches, linger: float = LINGER_SECONDS):
    """One puff per stretch, and never two inside `linger`.

    Returns (time, scent). The puff lands at the start of the stretch, because
    a scent takes time to arrive and a scene that has already ended does not
    want to start smelling of itself.

    A stretch that is refused is refused entirely rather than delayed. Delaying
    it would put the smell of one scene into the next, which is the exact
    failure the wait exists to prevent.
    """
    out = []
    when = None
    for start, _end, scent in sorted(stretches):
        if when is not None and start - when < linger:
            continue
        out.append((start, scent))
        when = start
    return out


def cues(observations, instrument: str, bank=BANK,
         linger: float = LINGER_SECONDS, hold: float = HOLD_SECONDS,
         output: float = 0.6):
    """Scent cues for a film, from what the scene pass saw.

    The whole point of the file in one call: find the stretches, throw away the
    ones too brief or too close together, and put one puff at the start of each
    survivor.
    """
    out = []
    for at, scent in ration(scenes(observations, bank, hold), linger):
        out.append({
            "instrument": instrument,
            "t": round(at, 3),
            "action": "puff",
            "params": {"output": output},
            "scent": scent,
            "duration": 1.0,
            "source": "scene: " + scent,
        })
    return out
