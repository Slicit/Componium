"""Rebuild the cues a vision pass proposed, without looking at the film again.

The second of three passes. The first costs a GPU and a decode and writes down
what it saw; this one draws conclusions from that, and drawing them again is
cheap. Changing what smoke should drive, or whether a splash needs
corroborating, used to mean half an hour of decoding a feature before you could
see the result. It now takes about as long as reading a file.

What it touches and what it leaves alone is the whole design. It rebuilds
exactly the cues whose source says a model proposed them, and nothing else: the
curves the signals produced, the flashes the luminance pass found, the cues the
subtitles gave, all stay as they are. That is only possible because a cue
records what proposed it — which is why source had to stop being a comment.
"""

from __future__ import annotations

import json
import tomllib

import compose
import subtitles

# What a remapped cue says about itself. Anything carrying this prefix is
# rebuilt; anything else is left where it is.
VISION = "vision"


def load_score(path: str) -> dict:
    with open(path, "rb") as f:
        return tomllib.load(f)


def load_observations(path: str) -> list[dict]:
    """Read the description, skipping anything unreadable.

    A line that will not parse is skipped rather than fatal: the file is
    appended to a chunk at a time by something that can be interrupted, and one
    torn line at the end is not a reason to refuse the other two thousand.
    """
    out = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                row = json.loads(line)
            except ValueError:
                continue
            if isinstance(row, dict) and "t" in row:
                out.append(row)
    return out


def as_pairs(observations) -> list[tuple[float, str]]:
    """The (time, label) pairs the mapping reads."""
    pairs = []
    for o in observations:
        for label in o.get("labels") or []:
            pairs.append((float(o["t"]), label))
    return pairs


def seconds_of(value) -> float:
    """A timecode as seconds, from either of the shapes a score uses."""
    if isinstance(value, (int, float)):
        return float(value)
    text = str(value).strip()
    if text.endswith("s") and ":" not in text:
        try:
            return float(text[:-1])
        except ValueError:
            return 0.0
    parts = text.split(":")
    try:
        parts = [float(p) for p in parts]
    except ValueError:
        return 0.0
    total = 0.0
    for p in parts:
        total = total * 60 + p
    return total


def keep(cue: dict) -> bool:
    """Is this a cue the remap leaves alone."""
    return not str(cue.get("source", "")).startswith(VISION)


def as_written(cue: dict) -> dict:
    """A freshly proposed cue in the shape a loaded one has.

    The mapping works in seconds and the file is written in timecodes and
    durations with units. Converting here rather than at the point of writing
    keeps every cue in the score the same shape, whether it was loaded or just
    proposed — so sorting, filtering and dumping need not ask which.

    Not converting was worth a parse error: a duration of "6.0" came back as
    "missing unit in duration".
    """
    return {
        "t": compose.timecode(float(cue["t"])),
        "action": cue["action"],
        "params": cue["params"],
        "duration": "%.3fs" % float(cue.get("duration") or 0),
        "source": cue["source"],
    }


def remap(score: dict, observations, mapping=None, kinds=None, gate=None) -> dict:
    """Return the score with its vision cues rebuilt from the observations.

    The score is not modified; a new one is returned, because the caller may
    well want to compare them.
    """
    pairs = as_pairs(observations)
    if gate is not None:
        pairs = gate(pairs)

    proposed = subtitles.cues_from_descriptions(
        pairs, mapping, kinds or {}, source=VISION)

    # Group what the mapping proposed by the instrument it is addressed to.
    by_instrument: dict[str, list[dict]] = {}
    for cue in proposed:
        by_instrument.setdefault(cue["instrument"], []).append(cue)

    out = dict(score)
    tracks = []
    seen_instruments = set()

    for track in score.get("track") or []:
        fresh = dict(track)
        if track.get("type") == "cue":
            instrument = track.get("instrument", "")
            seen_instruments.add(instrument)
            kept = [c for c in (track.get("cues") or []) if keep(c)]
            for cue in by_instrument.get(instrument, []):
                kept.append(as_written(cue))
            kept.sort(key=lambda c: seconds_of(c["t"]))
            fresh["cues"] = kept
        tracks.append(fresh)

    # An instrument the mapping now addresses that had no track before. A new
    # vocabulary word, or a rig that gained a device.
    for instrument in sorted(by_instrument):
        if instrument in seen_instruments:
            continue
        tracks.append({
            "instrument": instrument,
            "type": "cue",
            "cues": [as_written(c)
                     for c in sorted(by_instrument[instrument], key=lambda c: c["t"])],
        })

    out["track"] = [t for t in tracks
                    if t.get("type") != "cue" or t.get("cues")]
    return out


def counts(score: dict) -> dict:
    """Cues per instrument, for saying what a remap changed."""
    out = {}
    for track in score.get("track") or []:
        if track.get("type") == "cue":
            out[track.get("instrument", "?")] = len(track.get("cues") or [])
    return out


def _num(v) -> str:
    """A number as the score writes them, without a trailing .0 on integers."""
    f = float(v)
    return ("%.4f" % f).rstrip("0").rstrip(".") or "0"


def _quote(v) -> str:
    return '"' + str(v).replace("\\", " ").replace('"', "'") + '"'


def dump(score: dict) -> str:
    """Render a score loaded by tomllib back to the format it came from.

    Written by hand for the same reason the composer renders by hand: no
    dependencies, so this runs wherever ffmpeg does. Timecodes come back as the
    strings they were loaded as, so a remap never re-rounds a time it did not
    change.
    """
    lines = [
        "# Generated by the Componium composer.",
        "# Cues proposed by a vision model have been rebuilt from the",
        "# description beside this file. Everything else is as it was.",
        "",
        "[score]",
    ]
    meta = score.get("score") or {}
    for key in ("componium", "title"):
        if meta.get(key):
            lines.append('%s = %s' % (key, _quote(meta[key])))

    media = meta.get("media") or {}
    if media:
        lines.append("")
        lines.append("[score.media]")
        if media.get("duration"):
            lines.append('duration = %s' % _quote(media["duration"]))
        if media.get("hash"):
            lines.append('hash = %s' % _quote(media["hash"]))
        if media.get("fps"):
            lines.append("fps = %s" % _num(media["fps"]))

    for region in score.get("calm") or []:
        lines += ["", "[[calm]]",
                  'from = %s' % _quote(region.get("from", "00:00:00.000")),
                  'to = %s' % _quote(region.get("to", "00:00:00.000"))]

    # Where the film was given more, recorded for the same reason as where it
    # was given less: so a reviewer can tell a film that is bigger here from a
    # signal that decided it was.
    for region in score.get("loud") or []:
        lines += ["", "[[loud]]",
                  'from = %s' % _quote(region.get("from", "00:00:00.000")),
                  'to = %s' % _quote(region.get("to", "00:00:00.000"))]

    for track in score.get("track") or []:
        lines += ["", "[[track]]", 'instrument = %s' % _quote(track.get("instrument", ""))]
        kind = track.get("type", "cue")
        lines.append('type = %s' % _quote(kind))

        if kind == "cue":
            lines.append("cues = [")
            for cue in track.get("cues") or []:
                params = ", ".join("%s = %s" % (k, _num(v))
                                   for k, v in sorted((cue.get("params") or {}).items()))
                row = '  { t = %s, action = %s' % (_quote(cue.get("t", "00:00:00.000")),
                                                   _quote(cue.get("action", "")))
                if params:
                    row += ", params = { %s }" % params
                if cue.get("duration"):
                    row += ", duration = %s" % _quote(cue["duration"])
                if cue.get("source"):
                    row += ", source = %s" % _quote(cue["source"])
                lines.append(row + " },")
            lines.append("]")
        else:
            lines.append('interpolation = %s' % _quote(track.get("interpolation") or "linear"))
            if track.get("space"):
                lines.append('space = %s' % _quote(track["space"]))
            lines.append("points = [")
            for point in track.get("points") or []:
                body = ", ".join("%s = %s" % (k, _num(v))
                                 for k, v in sorted((point.get("value") or {}).items()))
                lines.append('  { t = %s, value = { %s } },'
                             % (_quote(point.get("t", "00:00:00.000")), body))
            lines.append("]")

    return "\n".join(lines) + "\n"


def main(argv=None):
    import argparse
    import os
    import sys

    p = argparse.ArgumentParser(
        description="Rebuild a score's vision cues from the description beside it.")
    p.add_argument("score", help="a .componium file")
    p.add_argument("-o", "--out", help="where to write (default: stdout)")
    p.add_argument("--seen", help="the description (default: <score>.seen.jsonl)")
    p.add_argument("--mapping", help="JSON file replacing the word to effect mapping")
    p.add_argument("--light-id", default="light.ambient")
    p.add_argument("--light-event-id", default="light.event")
    p.add_argument("--shake-id", default="shake.seat")
    p.add_argument("--wind-id", default="wind.main")
    p.add_argument("--mist-id", default="mist.main")
    p.add_argument("--fog-id", default="fog.main")
    p.add_argument("--scent-id", default="scent.main")
    p.add_argument("--no-gate", action="store_true",
                   help="keep labels that would be dropped for want of corroboration")
    args = p.parse_args(argv)

    seen_path = args.seen or (args.score + ".seen.jsonl")
    if not os.path.exists(seen_path):
        sys.exit("no description beside that score: " + seen_path
                 + "\nAnalyse it with a vision command first.")

    score = load_score(args.score)
    observations = load_observations(seen_path)
    before = counts(score)

    import vision
    kinds = {
        "light": args.light_event_id,
        "shake": args.shake_id,
        "wind": args.wind_id,
        "mist": args.mist_id,
        "fog": args.fog_id,
        "scent": args.scent_id,
    }
    out = remap(score, observations,
                mapping=subtitles.load_mapping(args.mapping),
                kinds=kinds,
                gate=None if args.no_gate else vision.gate)

    after = counts(out)
    for instrument in sorted(set(before) | set(after)):
        was, now = before.get(instrument, 0), after.get(instrument, 0)
        if was != now:
            sys.stderr.write("%-18s %d -> %d\n" % (instrument, was, now))
    sys.stderr.write("%d observations, %d instruments\n"
                     % (len(observations), len(after)))

    text = dump(out)
    if args.out:
        with open(args.out, "w", encoding="utf-8", newline="\n") as f:
            f.write(text)
        sys.stderr.write("wrote " + args.out + "\n")
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
