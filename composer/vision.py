"""Optional semantic labelling of keyframes by an external model.

Componium ships no model. What it ships is a seam: `--vlm-command` names a
program that takes an image path and prints labels, one per line. Anything you
can wrap in a shell script works, local or remote, and the composer neither
knows nor cares which.

That is deliberate. Bundling a model would date badly, bloat the install for
everyone who does not want it, and make a choice on the user's behalf about
where their film frames are sent. A seam costs forty lines and ages well.

The expensive pass runs only on windows the cheap detectors already flagged.
Sending every frame of a two hour film to a model would cost a fortune in time
or money to learn what the audio already said.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile

# A label is only useful if something can act on it, so the vocabulary is the
# same one the subtitle mapping uses. A model that says "explosion" produces
# the same cues as a subtitle that said "[explosion]".


def candidates(envelope, rate: float, cuts=None, limit: int = 40,
               threshold: float = 0.55, spacing: float = 8.0):
    """Choose the moments worth showing to a model.

    Two cheap signals nominate: loud low frequency moments, and scene cuts.
    Both are things the composer already computed, so nomination is free.

    Results are spaced out and capped, because forty keyframes across a feature
    is enough to characterise it and four thousand is a way to spend an
    afternoon.
    """
    picks = []

    for i, value in enumerate(envelope or []):
        if value >= threshold:
            picks.append((value, i / rate))
    picks.sort(reverse=True)

    chosen = []
    for _score, at in picks:
        if all(abs(at - other) >= spacing for other in chosen):
            chosen.append(at)
        if len(chosen) >= limit:
            break

    # Scene cuts fill any remaining budget: a cut is a change of place, which
    # is exactly what a model can describe and audio cannot.
    for at in (cuts or []):
        if len(chosen) >= limit:
            break
        if all(abs(at - other) >= spacing for other in chosen):
            chosen.append(at)

    return sorted(chosen)


def keyframe(path: str, at: float, out_path: str) -> bool:
    """Extract one frame as a JPEG. Returns False if ffmpeg could not."""
    exe = shutil.which("ffmpeg")
    if not exe:
        return False
    result = subprocess.run(
        [exe, "-v", "error", "-y", "-ss", f"{at:.3f}", "-i", path,
         "-frames:v", "1", "-q:v", "3", out_path],
        capture_output=True, check=False,
    )
    return result.returncode == 0 and os.path.exists(out_path)


def parse_labels(text: str) -> list[str]:
    """Read a model's output: one label per line, blanks and comments ignored.

    Deliberately the dullest format available. Anyone wrapping a model should
    be able to produce it with an echo, and should not have to read a schema.
    """
    out = []
    for line in (text or "").splitlines():
        line = line.strip().lower()
        if not line or line.startswith("#"):
            continue
        # Tolerate "0.92 explosion" and "explosion: 0.92" alike, because
        # models emit confidences and nobody should have to strip them.
        for token in line.replace(":", " ").replace(",", " ").split():
            if token.replace(".", "", 1).isdigit():
                continue
            out.append(token)
    return out


def label_frame(command: str, image_path: str, timeout: float = 60.0) -> list[str]:
    """Run the labelling command against one image."""
    try:
        result = subprocess.run(
            command.split() + [image_path],
            capture_output=True, text=True, errors="replace",
            timeout=timeout, check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return []
    if result.returncode != 0:
        return []
    return parse_labels(result.stdout)


def describe(path: str, times, command: str, timeout: float = 60.0):
    """Label a set of moments. Returns (time, label) pairs.

    A model that fails on one frame does not stop the run. A composer that
    aborts three quarters of the way through a feature because one JPEG upset
    something is worse than one that returns slightly less.
    """
    found = []
    with tempfile.TemporaryDirectory(prefix="componium-vlm-") as tmp:
        for i, at in enumerate(times):
            image = os.path.join(tmp, f"frame-{i:04d}.jpg")
            if not keyframe(path, at, image):
                continue
            for label in label_frame(command, image, timeout):
                found.append((at, label))
    return found
