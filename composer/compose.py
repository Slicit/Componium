#!/usr/bin/env python3
"""Generate a Componium score from a film.

Composer v1 extracts four signals:

  LFE energy -> shake.  Sub-bass maps almost directly onto rumble, and it is
  nearly free to compute.  Explosions, engines and thunder all live here.

  Average frame colour -> ambient light.  This is what Ambilight does.  It is
  one ffmpeg filter and it demonstrates the whole pipeline end to end.

  Subtitle descriptions -> cues.  SDH subtitles already carry timestamped,
  human authored labels for exactly the events a rig wants: [thunder rumbles].

  Scene cuts -> curve snapping, so effects do not bleed across a hard cut.

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

import analysis
import dynamics
import span as span_mod
import light
import motion_est
import scenes
import subtitles
import vision
import water

SCORE_VERSION = "0.1"

# The axis order used for pose curves. Fixed, because a curve point is a tuple
# during compression and the names have to go back on in the same order.
POSE_AXES = ("surge", "sway", "heave", "roll", "pitch", "yaw")

# Three axes by default. A platform with three actuators under a triangle can
# produce exactly heave, roll and pitch, which is what almost every buildable
# home rig is; six needs a Stewart platform. Surge and sway are folded in as
# tilt rather than dropped, and this analysis already leaves sway and roll at
# zero always, so the honest loss is smaller than the axis count suggests.
# Six remains available for a rig that has them.


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


def average_colours(path: str, fps: float, span=None) -> list[tuple[float, float, float]]:
    """Return the average colour of each sampled frame, as 0..1 triples.

    Scaling to a single pixel makes ffmpeg do the averaging, which is far
    faster than reading frames into Python and much less code.
    """
    cmd = [
        ffmpeg_path(), "-v", "error",
        *(span.input_args() if span else []), "-i", path,
        "-vf", f"fps={fps},scale=1:1",
        "-f", "rawvideo", "-pix_fmt", "rgb24", "-",
    ]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    out = []
    for i in range(0, len(raw) - 2, 3):
        out.append((raw[i] / 255.0, raw[i + 1] / 255.0, raw[i + 2] / 255.0))
    return out


LFE_RATE = 1000


def lfe_samples(path: str, cutoff_hz: int = 120, span=None):
    """Low-passed mono audio at 1kHz, as signed 16 bit samples.

    Working at 1kHz rather than 48kHz makes this cheap enough to run over a
    feature film without anyone noticing: measured at 21 seconds for a two
    hour film, which is 345 times realtime.
    """
    cmd = [
        ffmpeg_path(), "-v", "error",
        *(span.input_args() if span else []), "-i", path,
        "-af", f"lowpass=f={cutoff_hz}",
        "-ac", "1", "-ar", str(LFE_RATE),
        "-f", "s16le", "-",
    ]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    samples = array.array("h")
    samples.frombytes(raw[: len(raw) - (len(raw) % 2)])
    return samples


def rms_series(samples, window: int) -> list[float]:
    """Root mean square per window, in the units the samples came in.

    Unnormalised, which is the point: this is the only form in which two
    different parts of a film can be compared with each other.
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
    return out


def audio_peak(path: str, rate: float, cutoff_hz: int = 120) -> float:
    """The loudest window in a whole film, for chunks to normalise against.

    Analysing a film in pieces silently redefines what "the loudest window"
    means: each piece would be scaled against its own peak, so a quiet chunk
    would be amplified until it matched an action chunk and the shake track
    would change character at every boundary. Nothing fails, the score is just
    wrong. So the peak is measured once over the whole film and handed to every
    piece.
    """
    samples = lfe_samples(path, cutoff_hz)
    series = rms_series(samples, int(LFE_RATE / rate))
    return max(series) if series else 0.0


def lfe_envelope(path: str, rate: float, cutoff_hz: int = 120, span=None,
                 peak: float = 0.0) -> list[float]:
    """Return a low-frequency energy envelope, one value per 1/rate second.

    With no peak given this normalises by the loudest window it can see, which
    is right for a whole film and wrong for a piece of one.
    """
    samples = lfe_samples(path, cutoff_hz, span)
    return rms_windows(samples, int(LFE_RATE / rate), peak)


def rms_windows(samples, window: int, peak: float = 0.0) -> list[float]:
    """Root mean square per window, normalised to 0..1.

    Normalising by the peak rather than by full scale means a quiet film still
    produces a usable range, which matters more than absolute calibration: the
    author sets the rig's overall intensity, not the composer.

    A peak may be supplied, and must be when this is looking at part of a film
    rather than all of it — see audio_peak. The clamp is for that case: a
    supplied peak is measured over material this call cannot see, and floating
    point makes "the loudest window" and "the loudest window, again" differ in
    the last place.
    """
    out = rms_series(samples, window)
    scale = peak if peak > 0 else (max(out) if out else 0.0)
    if scale <= 0:
        return [0.0] * len(out)
    return [min(1.0, v / scale) for v in out]


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


def render(meta, tracks, calm=()) -> str:
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

    lines += _calm_sections(calm)

    for tr in tracks:
        lines += ["", "[[track]]", f'instrument = "{tr["instrument"]}"']
        if tr.get("type") == "cue":
            lines += ['type = "cue"', "cues = ["]
            for cue in tr["cues"]:
                params = ", ".join(f"{k} = {v:.4f}" for k, v in cue["params"].items())
                row = f'  {{ t = "{timecode(cue["t"])}", action = "{cue["action"]}"'
                if params:
                    row += f", params = {{ {params} }}"
                if cue.get("duration"):
                    row += f', duration = "{cue["duration"]:.1f}s"'
                row += " },"
                if cue.get("source"):
                    # The source is whatever nominated this cue: a subtitle, a
                    # luminance rise, a camera movement. It goes in the file so
                    # a reviewer can judge the cue without rerunning anything.
                    lines.append(f'  # {cue["source"]}')
                lines.append(row)
            lines.append("]")
        else:
            lines += ['type = "curve"', 'interpolation = "linear"']
            if tr.get("space"):
                lines.append(f'space = "{tr["space"]}"')
            lines.append("points = [")
            for at, values in tr["points"]:
                body = ", ".join(f"{k} = {v:.4f}" for k, v in values.items())
                lines.append(f'  {{ t = "{timecode(at)}", value = {{ {body} }} }},')
            lines.append("]")
    return "\n".join(lines) + "\n"


def _calm_sections(calm):
    """Write down where the analysis decided to leave the film alone.

    Advisory — the player never reads it. It is recorded because it is the
    answer to the only question a sparse stretch of timeline provokes, and
    because these regions were computed anyway, used to decide what not to
    play, and then discarded.
    """
    lines = []
    for lo, hi in calm:
        lines += ["", "[[calm]]",
                  f'from = "{timecode(lo)}"',
                  f'to = "{timecode(hi)}"']
    return lines


def _cue_track(instrument, cues):
    return {"instrument": instrument, "type": "cue", "cues": cues}


def _curve_track(instrument, points, space=None):
    track = {"instrument": instrument, "type": "curve", "points": points}
    if space:
        track["space"] = space
    return track


def progress(fraction: float, label: str):
    """Emit machine readable progress on stderr.

    The studio runs this as a background job and parses these lines to draw
    a bar. Printed rather than returned because the work is a subprocess,
    and stderr because stdout may be carrying the score itself.
    """
    sys.stderr.write("PROGRESS %.3f %s\n" % (fraction, label))
    sys.stderr.flush()


def build(args) -> str:
    report = sys.stderr.write
    film_duration = ffprobe_duration(args.input)
    span = span_mod.Span(getattr(args, "start", 0.0), getattr(args, "end", 0.0),
                         getattr(args, "warmup", 0.0))

    # How long the part being analysed is, which is what everything below is
    # counting frames against. For a whole film that is the film.
    if span.whole:
        duration = film_duration
    else:
        end = span.end if span.end > 0 else film_duration
        duration = max(0.0, end - span.decode_start)
        report(f"analysing {timecode(span.start)} to "
               f"{timecode(span.end) if span.end else 'the end'}"
               f" ({span.lead:.0f}s lead in)\n")

    # One grayscale pass and one colour pass. Everything below is derived from
    # those two, rather than decoding the film once per feature.
    progress(0.05, "decoding frames")
    frames = analysis.analyse(args.input, args.fps, span=span)
    progress(0.45, "decoding colour")
    colour_raw = list(analysis.colour_frames(args.input, args.fps, span=span))
    colours = [analysis.mean_colour(f) for f in colour_raw]
    report(f"{len(frames)} frames analysed at {args.fps} Hz\n")

    progress(0.55, "detecting scene cuts")
    cuts = [] if args.no_scenes else scenes.detect(
        args.input, args.scene_threshold, span=span)
    progress(0.62, "estimating camera movement")
    movements = motion_est.track(frames, width=analysis.GRAY_W)
    speed = motion_est.speed_series(movements, args.fps)
    progress(0.72, "reading low frequency audio")
    env = lfe_envelope(args.input, args.fps, span=span,
                       peak=getattr(args, "audio_peak", 0.0) or 0.0)

    # --- what the film is doing, before deciding what to play ----------------
    progress(0.80, "finding calm")
    levels = dynamics.activity(audio=env, speed=speed, cuts=cuts, fps=args.fps,
                               duration=duration)
    calm = [] if args.no_dynamics else dynamics.calm_regions(
        levels, args.fps, args.calm_threshold, args.calm_min)
    quiet_seconds = sum(hi - lo for lo, hi in calm)
    report(f"{len(calm)} calm regions, {quiet_seconds:.0f}s of the film left alone\n")

    tracks = []
    cue_groups = {}

    def add_cues(instrument, cues):
        if instrument and cues:
            cue_groups.setdefault(instrument, []).extend(cues)

    # --- light, in two layers ------------------------------------------------
    soft = light.soft_curve(colours, gain=args.light_gain)
    soft = compress([(i / args.fps, rgb) for i, rgb in enumerate(soft)], args.threshold)
    soft = scenes.snap(soft, cuts)
    if len(soft) >= 2:
        # Written as hue, saturation and intensity rather than as three
        # colour channels. The wash is edited far more often than any other
        # track, and almost every edit to it is "dim this stretch" — one
        # number here, three that must move together in RGB.
        tracks.append(_curve_track(
            args.light_id,
            [(t, dict(zip(("h", "s", "i"), light.to_hsi(v)))) for t, v in soft],
            space="hsi",
        ))

    # Flashes get their own fast pass. At the analysis rate most of them
    # fall between samples: a lightning strike lasts about 150ms, and 4 Hz
    # misses four out of five. One byte per frame makes 24 Hz free.
    progress(0.86, "finding flashes")
    flash_fps = args.flash_fps or (args.media_fps or 24.0)
    lumas = [analysis.Luma(v)
             for v in analysis.luma_series(args.input, flash_fps, span=span)]
    flash_colours = [analysis.mean_colour(f)
                     for f in analysis.colour_frames(args.input, flash_fps, span=span)]
    add_cues(args.light_event_id, light.flashes(lumas, flash_colours, flash_fps))

    # --- shake ---------------------------------------------------------------
    shake = compress([(i / args.fps, (v * args.shake_gain,)) for i, v in enumerate(env)],
                     args.threshold)
    shake = scenes.snap(shake, cuts)
    if len(shake) >= 2:
        tracks.append(_curve_track(args.shake_id,
                                   [(t, {"intensity": v[0]}) for t, v in shake]))

    # --- motion, as continuous 6DOF ------------------------------------------
    #
    # A curve rather than cues. Plunges are still detected and reported, but
    # they are already present in this curve as heave, and emitting both would
    # put a span and a curve driver in a fight over one instrument.
    plunges = motion_est.find_plunges(movements, args.fps, merge_gap=3.0)
    if plunges:
        report(str(len(plunges)) + " plunges, already carried by the pose curve\n")

    if args.motion_id:
        pose = motion_est.pose_series(movements, args.fps, gain=args.motion_gain)
        axes = POSE_AXES if args.dof == 6 else motion_est.DOF3_AXES
        if args.dof != 6:
            pose = motion_est.to_3dof(pose)
        points = [(i / args.fps, tuple(p[a] for a in axes))
                  for i, p in enumerate(pose)]
        points = compress(points, args.threshold)
        points = scenes.snap(points, cuts)
        if len(points) >= 2:
            tracks.append(_curve_track(args.motion_id, [
                (t, dict(zip(axes, v))) for t, v in points]))

    # --- wind, from apparent speed -------------------------------------------
    if args.wind_id:
        wind = motion_est.wind_series(movements, args.fps)
        wpts = compress([(i / args.fps, (v * args.wind_gain,))
                         for i, v in enumerate(wind)], args.threshold)
        wpts = scenes.snap(wpts, cuts)
        if len(wpts) >= 2:
            tracks.append(_curve_track(args.wind_id,
                                       [(t, {"intensity": v[0]}) for t, v in wpts]))

    # --- subtitles and the vision model --------------------------------------
    confirmations = []
    semantic = []
    if not args.no_subtitles:
        progress(0.92, "mining subtitles")
        srt = subtitles.extract(args.input, args.subtitle_stream, span=span)
        if srt:
            entries = subtitles.parse(srt)
            confirmations += subtitles.descriptions(entries)
            kinds = {"light": args.light_id, "shake": args.shake_id}
            semantic += subtitles.cues_from_descriptions(
                entries and subtitles.descriptions(entries),
                subtitles.load_mapping(args.mapping), kinds)

    if args.vlm_command:
        times = vision.candidates(env, args.fps, cuts, args.vlm_frames)
        labels = vision.describe(args.input, times, args.vlm_command)
        confirmations += labels
        kinds = {"light": args.light_id, "shake": args.shake_id}
        semantic += subtitles.cues_from_descriptions(
            labels, subtitles.load_mapping(args.mapping), kinds)
        report(f"{len(times)} keyframes labelled, {len(semantic)} semantic cues\n")

    # --- water, nominated then confirmed -------------------------------------
    progress(0.95, "nominating water")
    nominated = water.candidates(colour_raw, args.fps)
    wet = water.confirmed(nominated, confirmations)
    report(f"{len(nominated)} water nominations, {len(wet)} confirmed\n")
    if args.mist_id:
        add_cues(args.mist_id, [{
            "t": lo,
            "action": "spray",
            "params": {"output": round(min(0.8, 0.3 + score), 3)},
            "duration": min(8.0, hi - lo),
            "source": f"blue scene confirmed by: {label}",
        } for lo, hi, score, label in wet])

    for cue in semantic:
        add_cues(cue["instrument"], [cue])

    # --- dynamics: decide what not to play -----------------------------------
    dropped_calm = dropped_budget = 0
    for instrument, cues in list(cue_groups.items()):
        if not args.no_dynamics:
            cues, dropped = dynamics.protect_calm(cues, calm)
            dropped_calm += len(dropped)
            cues, dropped = dynamics.enforce_budget(
                cues, args.budget_window, args.budget_max)
            dropped_budget += len(dropped)
        cue_groups[instrument] = sorted(cues, key=lambda c: c["t"])

    if not args.no_dynamics:
        report(f"{dropped_calm} cues dropped to protect calm, "
               f"{dropped_budget} to stay inside the rest budget\n")

    for instrument in sorted(cue_groups):
        if cue_groups[instrument]:
            tracks.append(_cue_track(instrument, cue_groups[instrument]))

    # Everything above counted from the start of what was decoded. Move it
    # into the film's own clock and drop the lead in, so a partial score is a
    # short score rather than a score that needs correcting.
    tracks = span_mod.place(tracks, span, film_duration)
    calm = span_mod.place_regions(calm, span)
    if not tracks:
        sys.exit("nothing extracted; is the input a playable file?")

    meta = {
        "title": args.title or os.path.splitext(os.path.basename(args.input))[0],
        # The duration is the film's, not the range's: a partial score is a
        # window onto a film of a known length, and a merge that had to add up
        # its pieces to discover how long the film was would be trusting the
        # least reliable number it has.
        "duration": film_duration or (len(frames) / args.fps),
        "fps": args.media_fps,
        # Hashed from the film, which is not always the file being read.
        # A prepared copy decodes five times faster and is what the studio
        # plays, so it is what gets analysed — but the score binds to the film
        # the viewer actually has, or regenerating a preview would silently
        # unbind every score made from it.
        "hash": "" if args.no_hash else file_hash(
            getattr(args, "hash_file", "") or args.input, args.hash_mb),
    }
    progress(1.0, "writing the score")
    return render(meta, tracks, calm)


def main(argv=None):
    p = argparse.ArgumentParser(description="Generate a Componium score from a film.")
    p.add_argument("input", help="video file")
    p.add_argument("--from", dest="start", type=float, default=0.0,
                   metavar="SECONDS",
                   help="analyse from this point in the film (default: the start)")
    p.add_argument("--to", dest="end", type=float, default=0.0,
                   metavar="SECONDS",
                   help="analyse up to this point (default: the end)")
    p.add_argument("--warmup", type=float, default=span_mod.DEFAULT_WARMUP,
                   metavar="SECONDS",
                   help="decode this much before --from and discard it, so motion "
                        "has something to compare the first frame against")
    p.add_argument("--audio-peak", type=float, default=0.0,
                   help="the loudest audio window in the whole film, from "
                        "--probe-audio-peak. Required for a range to be scaled "
                        "the same way the rest of the film is")
    p.add_argument("--probe-audio-peak", action="store_true",
                   help="print the whole film's loudest audio window and exit")
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
    p.add_argument("--hash-file", default="",
                   help="hash this file instead of the input, for when the input "
                        "is a prepared copy and the score should bind to the film")
    p.add_argument("--no-subtitles", action="store_true",
                   help="do not mine the subtitle track for effect cues")
    p.add_argument("--subtitle-stream", type=int, default=0,
                   help="which subtitle stream to read (default 0)")
    p.add_argument("--mapping", help="JSON file replacing the word to effect mapping")
    p.add_argument("--no-scenes", action="store_true",
                   help="do not detect scene cuts")
    p.add_argument("--vlm-command",
                   help="program that takes an image path and prints labels, "
                        "one per line; Componium ships no model")
    p.add_argument("--flash-fps", type=float, default=0.0,
                   help="rate for flash detection; 0 uses the film's own")
    p.add_argument("--light-event-id", default="light.event",
                   help="instrument for bright spikes, separate from the soft wash")
    p.add_argument("--motion-id", default="",
                   help="instrument for plunges; empty means do not emit them")
    p.add_argument("--mist-id", default="mist.main",
                   help="instrument for confirmed water scenes")
    p.add_argument("--motion-gain", type=float, default=1.0,
                   help="scale on the generated pose (default 1.0)")
    p.add_argument("--dof", type=int, choices=(3, 6), default=3,
                   help="motion axes to write: 3 is heave, roll and pitch, "
                        "which is what a three actuator platform can produce "
                        "and what almost every buildable home rig is. 6 adds "
                        "surge, sway and yaw for a Stewart platform "
                        "(default 3)")
    p.add_argument("--wind-id", default="wind.main",
                   help="instrument for wind from camera speed; empty to skip")
    p.add_argument("--wind-gain", type=float, default=1.0)
    p.add_argument("--no-dynamics", action="store_true",
                   help="do not protect calm scenes or enforce a rest budget")
    p.add_argument("--calm-threshold", type=float, default=0.18,
                   help="activity level below which a stretch counts as calm")
    p.add_argument("--calm-min", type=float, default=12.0,
                   help="shortest calm stretch worth protecting, in seconds")
    p.add_argument("--budget-window", type=float, default=120.0,
                   help="window the rest budget is measured over, in seconds")
    p.add_argument("--budget-max", type=float, default=0.25,
                   help="fraction of any window that may be spent doing something")
    p.add_argument("--vlm-frames", type=int, default=40,
                   help="how many keyframes to label at most (default 40)")
    p.add_argument("--scene-threshold", type=float, default=0.35,
                   help="scene change sensitivity, higher is fewer cuts (default 0.35)")
    args = p.parse_args(argv)

    if args.probe_audio_peak:
        sys.stdout.write("%.6f\n" % audio_peak(args.input, args.fps))
        return 0

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
