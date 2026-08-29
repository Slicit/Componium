"""Mine effect cues from a film's subtitle track.

This is the most underrated signal available. Subtitles for the deaf and hard
of hearing already contain timestamped, human authored descriptions of exactly
the events a 4D rig wants to react to:

    00:14:22,100 --> 00:14:24,000
    [thunder rumbles]

That is a precise timestamp and a semantic label, written by a person who
watched the film, requiring no inference at all. Nothing else in the composer
comes close for accuracy per unit of effort.

The mapping from words to effects is deliberately data, not code, so that
somebody scoring a film in another language can replace it without touching
Python.
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess

# Only bracketed or parenthesised text is a sound description. Dialogue is not
# an effect, and treating it as one would fire the rig on every line spoken.
DESCRIPTION = re.compile(r"[\[(]([^\])]{2,80})[\])]")

TIMING = re.compile(
    r"(\d{2}):(\d{2}):(\d{2})[,.](\d{3})\s*-->\s*(\d{2}):(\d{2}):(\d{2})[,.](\d{3})"
)

# Word to effect. Keys are matched as whole words, case insensitively, against
# the description text.
#
# Kept conservative on purpose. A false positive fires a physical effect in
# somebody's living room at a moment the film did not call for, which is worse
# than a miss: a miss is merely absent, a false positive is wrong.
DEFAULT_MAPPING = {
    "thunder":   [{"kind": "shake", "action": "rumble", "params": {"intensity": 0.7}, "duration": 2.5},
                  {"kind": "light", "action": "flash", "params": {"r": 1.0, "g": 1.0, "b": 1.0}, "duration": 0.2}],
    "lightning": [{"kind": "light", "action": "flash", "params": {"r": 1.0, "g": 1.0, "b": 1.0}, "duration": 0.15}],
    "explosion": [{"kind": "shake", "action": "hit", "params": {"intensity": 1.0}, "duration": 1.5},
                  {"kind": "light", "action": "flash", "params": {"r": 1.0, "g": 0.6, "b": 0.2}, "duration": 0.3}],
    "explodes":  [{"kind": "shake", "action": "hit", "params": {"intensity": 1.0}, "duration": 1.5}],
    "gunshot":   [{"kind": "shake", "action": "hit", "params": {"intensity": 0.6}, "duration": 0.3}],
    "gunfire":   [{"kind": "shake", "action": "rumble", "params": {"intensity": 0.5}, "duration": 2.0}],
    "rain":      [{"kind": "mist", "action": "spray", "params": {"output": 0.4}, "duration": 4.0}],
    "raining":   [{"kind": "mist", "action": "spray", "params": {"output": 0.4}, "duration": 4.0}],
    "downpour":  [{"kind": "mist", "action": "spray", "params": {"output": 0.7}, "duration": 6.0}],
    "wind":      [{"kind": "wind", "action": "gust", "params": {"intensity": 0.6}, "duration": 5.0}],
    "gale":      [{"kind": "wind", "action": "gust", "params": {"intensity": 0.9}, "duration": 6.0}],
    "howling":   [{"kind": "wind", "action": "gust", "params": {"intensity": 0.7}, "duration": 5.0}],
    "engine":    [{"kind": "shake", "action": "rumble", "params": {"intensity": 0.4}, "duration": 3.0}],
    "rumbles":   [{"kind": "shake", "action": "rumble", "params": {"intensity": 0.5}, "duration": 2.0}],
    "roars":     [{"kind": "shake", "action": "rumble", "params": {"intensity": 0.6}, "duration": 2.0}],
    "crash":     [{"kind": "shake", "action": "hit", "params": {"intensity": 0.8}, "duration": 1.0}],
    # Scent. Conservative by necessity: a smell cannot be taken back, it
    # lingers past the scene that called for it, and some people are
    # asthmatic or allergic. Only the most unambiguous words qualify.
    "burning":   [{"kind": "scent", "action": "puff", "params": {"channel": 1.0}, "duration": 1.0}],
    "smoke":     [{"kind": "scent", "action": "puff", "params": {"channel": 1.0}, "duration": 1.0}],
    "petrichor": [{"kind": "scent", "action": "puff", "params": {"channel": 2.0}, "duration": 1.0}],
}


def load_mapping(path: str | None) -> dict:
    """Load a replacement mapping, or return the default."""
    if not path:
        return DEFAULT_MAPPING
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def extract(path: str, stream: int = 0) -> str:
    """Pull a subtitle track out of the container as SRT.

    Returns an empty string when the file has no subtitles, which is common and
    is not an error.
    """
    exe = shutil.which("ffmpeg")
    if not exe:
        return ""
    out = subprocess.run(
        [exe, "-v", "error", "-i", path, "-map", f"0:s:{stream}", "-f", "srt", "-"],
        capture_output=True, text=True, errors="replace", check=False,
    )
    return out.stdout if out.returncode == 0 else ""


def parse(srt: str):
    """Parse SRT into (start_seconds, end_seconds, text) triples.

    Deliberately forgiving: subtitle files in the wild are full of stray blank
    lines, missing indices and both comma and full stop decimal separators.
    Anything unparseable is skipped rather than fatal.
    """
    entries = []
    block_lines = []

    def flush():
        if not block_lines:
            return
        timing = None
        text_lines = []
        for line in block_lines:
            m = TIMING.search(line)
            if m and timing is None:
                timing = m
            elif timing is not None:
                text_lines.append(line)
        if timing:
            start = _seconds(timing.group(1), timing.group(2), timing.group(3), timing.group(4))
            end = _seconds(timing.group(5), timing.group(6), timing.group(7), timing.group(8))
            entries.append((start, end, " ".join(text_lines).strip()))
        block_lines.clear()

    for raw in srt.splitlines():
        if raw.strip() == "":
            flush()
        else:
            block_lines.append(raw.strip())
    flush()
    return entries


def _seconds(h, m, s, ms) -> float:
    return int(h) * 3600 + int(m) * 60 + int(s) + int(ms) / 1000.0


def descriptions(entries):
    """Return (time, phrase) for every bracketed sound description."""
    out = []
    for start, _end, text in entries:
        for match in DESCRIPTION.finditer(text):
            out.append((start, match.group(1).strip()))
    return out


def cues(entries, mapping=None, kinds=None):
    """Turn subtitle descriptions into cue dictionaries."""
    return cues_from_descriptions(descriptions(entries), mapping, kinds)


def cues_from_descriptions(descs, mapping=None, kinds=None):
    """Turn (time, phrase) pairs into cue dictionaries.

    Shared with the vision seam on purpose. A model that says "explosion"
    should produce exactly the same cues as a subtitle that said
    "[explosion]", so there is one vocabulary and one mapping rather than
    two that drift apart.

    kinds maps an effect kind to the instrument id in the target rig, so
    that a rig calling its fan "wind.left" still works.
    """
    mapping = mapping or DEFAULT_MAPPING
    kinds = kinds or {}
    out = []
    for at, phrase in descs:
        words = set(re.findall(r"[a-z]+", phrase.lower()))
        for word, effects in mapping.items():
            if word not in words:
                continue
            for effect in effects:
                kind = effect["kind"]
                out.append({
                    "t": at,
                    "instrument": kinds.get(kind, f"{kind}.main"),
                    "action": effect["action"],
                    "params": dict(effect.get("params", {})),
                    "duration": effect.get("duration", 1.0),
                    "source": phrase,
                })
    out.sort(key=lambda c: (c["t"], c["instrument"]))
    return dedupe(out)


def dedupe(cues_list, window: float = 0.5):
    """Drop cues for the same instrument within a short window.

    A description like "[thunder rumbles and crashes]" matches several words
    and would otherwise fire the same effect three times in the same instant.
    """
    kept = []
    for cue in cues_list:
        clash = False
        for other in reversed(kept):
            if cue["t"] - other["t"] > window:
                break
            if other["instrument"] == cue["instrument"]:
                clash = True
                break
        if not clash:
            kept.append(cue)
    return kept
