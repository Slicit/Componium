#!/usr/bin/env python3
"""Generate a Componium score from a film.

Composer v0 extracts the two cheapest signals that carry most of the value:

  LFE energy -> shake.  Sub-bass maps almost directly onto rumble, and it is
  nearly free to compute.  Explosions, engines and thunder all live here.

  Average frame colour -> ambient light.  This is what Ambilight does.  It is
  one ffmpeg filter and it demonstrates the whole pipeline end to end.

Both are deliberately dumb.  The expensive signals (scene detection, optical
flow, subtitle mining, a vision model over keyframes) come later, and they are
worth having only once the cheap ones are proven.

The output is a proposal for a human to refine, never something to play
unreviewed.  See LOGBOOK/features/feat-composer.md.
"""

from __future__ import annotations

import argparse
import array
import hashlib
import math
import os
import shutil
import subprocess
import sys

SCORE_VERSION = "0.1"


# --------------------------------------------------------------------------
# extraction
# --------------------------------------------------------------------------

def ffmpeg_path() -> str:
    exe = shutil.which("ffmpeg")
    if not exe:
        sys.exit("ffmpeg not found on PATH; the composer cannot decode anything without it")
    return exe


def ffprobe_duration(path: str) -> float:
    exe = shutil.which("ffprobe")
    if not exe:
        return 0.0
    out = subprocess.run(
        [exe, "-v", "error", "-show_entries", "format=duration",
         "-of", "default=nw=1:nk=1", path],
        capture_output=True, text=True, check=False,
    )
    try:
        return float(out.stdout.strip())
    except ValueError:
        return 0.0


def average_colours(path: str, fps: float) -> list[tuple[float, float, float]]:
    """Return the average colour of each sampled frame, as 0..1 triples.

    Scaling to a single pixel makes ffmpeg do the averaging, which is far
    faster than reading frames into Python and much less code.
    """
    cmd = [
        ffmpeg_path(), "-v", "error", "-i", path,
        "-vf", f"fps={fps},scale=1:1",
        "-f", "rawvideo", "-pix_fmt", "rgb24", "-",
    ]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    out = []
    for i in range(0, len(raw) - 2, 3):
        out.append((raw[i] / 255.0, raw[i + 1] / 255.0, raw[i + 2] / 255.0))
    return out


def lfe_envelope(path: str, rate: float, cutoff_hz: int = 120) -> list[float]:
    """Return a low-frequency energy envelope, one value per 1/rate second.

    The audio is low-passed and downsampled to 1kHz mono, then reduced to RMS
    per window.  Working at 1kHz rather than 48kHz makes this cheap enough to
    run over a feature film without anyone noticing.
    """
    sample_rate = 1000
    cmd = [
        ffmpeg_path(), "-v", "error", "-i", path,
        "-af", f"lowpass=f={cutoff_hz}",
        "-ac", "1", "-ar", str(sample_rate),
        "-f", "s16le", "-",
    ]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    samples = array.array("h")
    samples.frombytes(raw[: len(raw) - (len(raw) % 2)])
    return rms_windows(samples, int(sample_rate / rate))


def rms_windows(samples, window: int) -> list[float]:
    """Root mean square per window, normalised to 0..1 by the loudest window.

    Normalising by the peak rather than by full scale means a quiet film still
    produces a usable range, which matters more than absolute calibration: the
    author sets the rig's overall intensity, not the composer.
    """
    if window < 1:
        window = 1
    out = []
    for start in range(0, len(samples), window):
        chunk = samples[start:start + window]
        if not chunk:
            break
        total = 0.0
        for s in chunk:
            total += float(s) * float(s)
        out.append(math.sqrt(total / len(chunk)))
    peak = max(out) if out else 0.0
    if peak <= 0:
        return [0.0] * len(out)
    return [v / peak for v in out]


# --------------------------------------------------------------------------
# turning signals into a score
# --------------------------------------------------------------------------

def compress(points, threshold: float):
    """Drop points that are within threshold of the last kept one.

    A two hour film sampled four times a second is 28,800 points per track.
    Most of them say the same thing as their neighbour.  Keeping only
    meaningful changes takes a typical film down by an order of magnitude and
    makes the score something a human can actually open and edit.

    The first and last points are always kept, so the curve still spans the
    whole film.
    """
    if len(points) <= 2:
        return list(points)
    kept = [points[0]]
    for p in points[1:-1]:
        last = kept[-1][1]
        if max(abs(a - b) for a, b in zip(p[1], last)) >= threshold:
            kept.append(p)
    kept.append(points[-1])
    return kept


def timecode(seconds: float) -> str:
    """Format seconds as HH:MM:SS.mmm.

    Rounds to whole milliseconds first and decomposes afterwards. Doing it the
    other way round means handling a carry when .9995 rounds up to a full
    second, and getting that wrong produces timecodes like 00:00:60.000.
    """
    if seconds < 0:
        seconds = 0.0
    total_ms = int(round(seconds * 1000))
    h = total_ms // 3_600_000
    m = (total_ms % 3_600_000) // 60_000
    s = (total_ms % 60_000) // 1000
    ms = total_ms % 1000
    return f"{h:02d}:{m:02d}:{s:02d}.{ms:03d}"


def file_hash(path: str, limit_mb: int) -> str:
    """Hash the file so a score binds to content rather than a filename.

    With limit_mb set, only the first N megabytes are hashed along with the
    file size.  That is far faster on a ten gigabyte remux and still
    distinguishes different films, which is all the binding needs to do.
    """
    h = hashlib.sha256()
    size = os.path.getsize(path)
    h.update(str(size).encode())
    remaining = limit_mb * 1024 * 1024 if limit_mb > 0 else None
    with open(path, "rb") as f:
        while True:
            n = 1024 * 1024 if remaining is None else min(1024 * 1024, remaining)
            if n <= 0:
                break
            chunk = f.read(n)
            if not chunk:
                break
            h.update(chunk)
            if remaining is not None:
                remaining -= len(chunk)
    prefix = "sha256" if limit_mb <= 0 else f"sha256-first{limit_mb}mb"
    return f"{prefix}:{h.hexdigest()}"


def render(meta, tracks) -> str:
    """Render a score as TOML.

    Written by hand rather than with a TOML library so that the composer has no
    dependencies at all: it needs to run wherever ffmpeg does.
    """
    lines = [
        "# Generated by the Componium composer.",
        "# This is a proposal. Review it before playing it: nothing here has",
        "# been checked against what your rig can safely do.",
        "",
        "[score]",
        f'componium = "{SCORE_VERSION}"',
        f'title = "{meta["title"]}"',
        "",
        "[score.media]",
        f'duration = "{timecode(meta["duration"])}"',
    ]
    if meta.get("hash"):
        lines.append(f'hash = "{meta["hash"]}"')
    if meta.get("fps"):
        lines.append(f'fps = {meta["fps"]:.3f}')

    for tr in tracks:
        lines += ["", "[[track]]", f'instrument = "{tr["instrument"]}"',
                  'type = "curve"', 'interpolation = "linear"', "points = ["]
        for at, values in tr["points"]:
            body = ", ".join(f"{k} = {v:.4f}" for k, v in values.items())
            lines.append(f'  {{ t = "{timecode(at)}", value = {{ {body} }} }},')
        lines.append("]")
    return "\n".join(lines) + "\n"


def build(args) -> str:
    duration = ffprobe_duration(args.input)

    colours = average_colours(args.input, args.fps)
    light = [(i / args.fps,
              (r * args.light_gain, g * args.light_gain, b * args.light_gain))
             for i, (r, g, b) in enumerate(colours)]
    light = compress(light, args.threshold)
    light_points = [(t, {"r": v[0], "g": v[1], "b": v[2]}) for t, v in light]

    env = lfe_envelope(args.input, args.fps)
    shake = [(i / args.fps, (v * args.shake_gain,)) for i, v in enumerate(env)]
    shake = compress(shake, args.threshold)
    shake_points = [(t, {"intensity": v[0]}) for t, v in shake]

    tracks = []
    if len(light_points) >= 2:
        tracks.append({"instrument": args.light_id, "points": light_points})
    if len(shake_points) >= 2:
        tracks.append({"instrument": args.shake_id, "points": shake_points})
    if not tracks:
        sys.exit("nothing extracted; is the input a playable file?")

    meta = {
        "title": args.title or os.path.splitext(os.path.basename(args.input))[0],
        "duration": duration or (len(colours) / args.fps),
        "fps": args.media_fps,
        "hash": "" if args.no_hash else file_hash(args.input, args.hash_mb),
    }
    return render(meta, tracks)


def main(argv=None):
    p = argparse.ArgumentParser(description="Generate a Componium score from a film.")
    p.add_argument("input", help="video file")
    p.add_argument("-o", "--out", help="score file to write (default: stdout)")
    p.add_argument("--title", help="score title (default: the filename)")
    p.add_argument("--fps", type=float, default=4.0,
                   help="how often to sample signals, per second (default 4)")
    p.add_argument("--media-fps", type=float, default=0.0,
                   help="the film's own frame rate, recorded in the score")
    p.add_argument("--threshold", type=float, default=0.02,
                   help="drop curve points within this of the previous (default 0.02)")
    p.add_argument("--light-id", default="light.ambient")
    p.add_argument("--shake-id", default="shake.seat")
    p.add_argument("--light-gain", type=float, default=1.0)
    p.add_argument("--shake-gain", type=float, default=1.0)
    p.add_argument("--hash-mb", type=int, default=64,
                   help="megabytes to hash, 0 for the whole file (default 64)")
    p.add_argument("--no-hash", action="store_true")
    args = p.parse_args(argv)

    out = build(args)
    if args.out:
        with open(args.out, "w", encoding="utf-8", newline="\n") as f:
            f.write(out)
        sys.stderr.write("wrote " + args.out + "\n")
    else:
        sys.stdout.write(out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
