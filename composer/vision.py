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


# How wide a keyframe is sent to the model.
#
# Not the film's own size, which is what this used to send. Measured against
# Qwen2.5-VL on six moments of one film, full resolution and 512 wide disagreed
# on three of them — and disagreed the wrong way round. A frame of crabs
# throwing up sand came back as dust ten times out of ten at 512 wide and as
# nothing at all ten times out of ten at 1080p. Deterministic both ways; the
# size was the whole difference.
#
# It also costs five times more to be wrong: 3091 prompt tokens against 580.
# A large image becomes many patches, and a diffuse low contrast thing like a
# dust cloud is spread thin across them, where downscaled it occupies enough of
# one to be seen.
KEYFRAME_WIDTH = 512


def keyframe(path: str, at: float, out_path: str) -> bool:
    """Extract one frame as a JPEG. Returns False if ffmpeg could not."""
    exe = shutil.which("ffmpeg")
    if not exe:
        return False
    result = subprocess.run(
        [exe, "-v", "error", "-y", "-ss", f"{at:.3f}", "-i", path,
         "-frames:v", "1", "-q:v", "3",
         "-vf", f"scale={KEYFRAME_WIDTH}:-2", out_path],
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


def parse_seen(text: str) -> str:
    """The description a wrapper offered, if it offered one.

    Carried on a comment line, which the seam has always ignored — so a
    wrapper written before descriptions existed still works, and one that
    offers a sentence costs nothing to anything not looking for it.
    """
    for line in (text or "").splitlines():
        line = line.strip()
        if line.startswith("#"):
            said = line.lstrip("#").strip()
            if said:
                return said
    return ""


def observe_frame(command: str, image_path: str, timeout: float = 60.0):
    """Run the command against one image, keeping the labels and the sentence.

    Returns (labels, seen). A failure is an empty pair rather than an
    exception: a model that chokes on one JPEG must not cost the analysis
    every frame after it.
    """
    try:
        result = subprocess.run(
            command.split() + [image_path],
            capture_output=True, text=True, errors="replace",
            timeout=timeout, check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return [], ""
    if result.returncode != 0:
        return [], ""
    return parse_labels(result.stdout), parse_seen(result.stdout)


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


def observe(path: str, times, command: str, timeout: float = 60.0):
    """Look at a set of moments. Returns one record per frame seen.

    Each record is {"t", "labels", "seen"}. This is the pass that costs a GPU
    and a decode, and it is the only one that cannot be repeated once the film
    has been analysed and put away — so it keeps everything it was told, and
    the passes that draw conclusions from it read this rather than the film.

    A model that fails on one frame does not stop the run. A composer that
    aborts three quarters of the way through a feature because one JPEG upset
    something is worse than one that returns slightly less.
    """
    seen = []
    with tempfile.TemporaryDirectory(prefix="componium-vlm-") as tmp:
        for i, at in enumerate(times):
            image = os.path.join(tmp, f"frame-{i:04d}.jpg")
            if not keyframe(path, at, image):
                continue
            labels, said = observe_frame(command, image, timeout)
            if labels or said:
                seen.append({"t": round(at, 3), "labels": labels, "seen": said})
    return seen


def as_pairs(observations):
    """The (time, label) pairs the mapping reads, out of the observations."""
    out = []
    for o in observations or []:
        for label in o.get("labels") or []:
            out.append((o["t"], label))
    return out


def describe(path: str, times, command: str, timeout: float = 60.0):
    """Label a set of moments. Returns (time, label) pairs.

    Kept as it was for anything that only wants the conclusions.
    """
    return as_pairs(observe(path, times, command, timeout))


# Some labels are only worth believing in company.
#
# A model asked whether spray is water or sand is being asked to judge a
# material from a still, and it is not good at it: crabs kicking up sand on a
# beach read as a splash. It is good at whether a scene contains water at all —
# a sea, a lake, a river — which is a much easier question about a much larger
# part of the frame.
#
# So the weak judgement is gated on the strong one. A splash counts only where
# the model also saw water nearby; on a beach that is most of the film, and in
# a dust-blown desert it is none of it, which is exactly the discrimination the
# label could not make on its own.
#
# Tried and rejected: saying it more firmly in the prompt. Emphasising that a
# splash must be water made the model reach for the word more often, not less,
# and two calm shots of an island went from no effects to a splash.
CORROBORATES = {"splash": "water"}


def gate(found, window: float = 20.0):
    """Drop labels needing corroboration that did not get it.

    found is the (time, label) list describe() returns. The window is generous
    because it is asking whether this is a watery part of the film, not whether
    two things happened together.
    """
    if not found:
        return found
    out = []
    for at, label in found:
        need = CORROBORATES.get(label)
        if need is None:
            out.append((at, label))
            continue
        if any(other == need and abs(when - at) <= window for when, other in found):
            out.append((at, label))
    return out
