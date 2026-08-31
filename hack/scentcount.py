"""Which scents would actually have fired, on the films in this library?

A bank of reservoirs is a hardware commitment and a shopping list, so it is
worth choosing from what films contain rather than from what sounds evocative.
Every description the model has written is on disk — several thousand sentences
across the library — and each one says what was in front of the camera.

Counted by scene rather than by frame: scent lingers for minutes, so what
matters is how many distinct stretches of a film would have called for one, not
how many frames mention a tree.
"""

import collections
import glob
import json
import os
import re

SCORES = "/home/claude/componium-scores"

# Each candidate scent, and the words that would put it in a scene. Deliberately
# broad on the left and specific on the right: the question is how often a film
# is *in* a place, not how often a word appears.
SCENTS = {
    "smoke / burnt wood": r"smoke|smoky|burning|burnt|ember|ash|charred|fire|flame|blaze",
    "petrichor / rain": r"\brain|raining|downpour|storm|wet ground|puddle|drizzle",
    "sea salt": r"\bsea\b|ocean|beach|shore|waves|surf|coastal|harbou?r|tide",
    "pine / forest": r"forest|woods|woodland|pine|jungle|bamboo|trees|foliage",
    "cut grass": r"grass|meadow|field of|lawn|pasture|hay|farmland|crops|wheat",
    "earth / soil": r"cave|cavern|soil|\bmud|dirt|underground|tunnel|quarry|rubble|dug",
    "gunpowder": r"rifle|gun\b|guns\b|gunfire|shooting|shot at|weapon|soldier|battle|combat|war\b",
    "ozone / hot metal": r"machine|machinery|engine|metal|steel|industrial|factory|circuit|electric|spark|ship's|spacecraft",
    "floral": r"flower|blossom|garden|petal|bouquet|floral",
    "bread / cooking": r"kitchen|cooking|bread|meal|feast|food|dining|banquet|stove|bakery",
    "coffee": r"coffee|cafe|café|espresso|diner",
    "leather": r"leather|saddle|horse|car interior|jacket|boots|holster",
    "incense": r"temple|church|shrine|altar|candle|ritual|monk|cathedral|incense",
    "spirits": r"\bbar\b|tavern|whisk|wine|drink|bottle|pub|saloon|glass of",
    "citrus": r"market|orange|lemon|citrus|fruit|stall",
}
RE = {name: re.compile(pat, re.I) for name, pat in SCENTS.items()}

# How far apart two mentions have to be to count as two scenes.
SCENE_GAP = 120.0


def scenes_for(times):
    """Collapse a list of times into stretches, so lingering is respected."""
    if not times:
        return 0
    times = sorted(times)
    runs, last = 1, times[0]
    for t in times[1:]:
        if t - last > SCENE_GAP:
            runs += 1
        last = t
    return runs


films = sorted(glob.glob(os.path.join(SCORES, "*.seen.jsonl")))
if not films:
    raise SystemExit("no descriptions on disk")

total_scenes = collections.Counter()
total_frames = collections.Counter()
per_film = {}

for path in films:
    name = os.path.basename(path).split(".")[0][:22]
    rows = [json.loads(l) for l in open(path, encoding="utf-8") if l.strip()]
    hits = collections.defaultdict(list)
    for r in rows:
        said = r.get("seen") or ""
        for scent, rx in RE.items():
            if rx.search(said):
                hits[scent].append(float(r["t"]))
    per_film[name] = {s: scenes_for(ts) for s, ts in hits.items()}
    for s, ts in hits.items():
        total_scenes[s] += scenes_for(ts)
        total_frames[s] += len(ts)
    print("%-24s %5d descriptions" % (name, len(rows)))

print()
print("%-22s %8s %8s   %s" % ("scent", "scenes", "frames", "films it appears in"))
print("-" * 78)
for scent, n in total_scenes.most_common():
    where = sum(1 for f in per_film.values() if f.get(scent))
    print("%-22s %8d %8d   %d of %d" % (scent, n, total_frames[scent], where, len(per_film)))

missing = [s for s in SCENTS if s not in total_scenes]
if missing:
    print()
    print("never mentioned:", ", ".join(missing))
