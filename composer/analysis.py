"""Frame level feature extraction.

Everything the analyser knows about a film comes from two cheap ffmpeg passes:

  A grayscale pass at 64x36, which is where motion comes from. Asking ffmpeg
  for gray rather than computing luma in Python saves two thirds of the data
  and all of the arithmetic.

  A one pixel colour pass, which is the ambient light.

Both are deliberately tiny. Motion estimation from a 64x36 image sounds absurd
until you notice that camera movement is a property of the whole frame, and
that downscaling is itself a very good low pass filter: it removes exactly the
detail that would otherwise make frame matching noisy.
"""

from __future__ import annotations

import os
import re
import shutil
import subprocess
import tempfile

# Where ffmpeg puts a frame time in a showinfo line. The same pattern
# scenes.py uses, kept separately rather than imported: a cycle between the
# two modules would be a poor trade for one regular expression.
PTS = re.compile(r"pts_time:([0-9.]+)")

# 64x36 keeps the 16:9 shape and gives about a degree of angular resolution per
# pixel on a typical shot, which is finer than any effect can act on anyway.
GRAY_W = 64
GRAY_H = 36


def _ffmpeg() -> str:
    exe = shutil.which("ffmpeg")
    if not exe:
        raise SystemExit("ffmpeg not found on PATH")
    return exe


def gray_frames(path: str, fps: float, w: int = GRAY_W, h: int = GRAY_H, span=None):
    """Yield one bytes object of w*h grayscale pixels per sampled frame.

    Streams rather than collecting: a two hour film at 8 Hz is 57,600 frames,
    and holding them all costs 130MB for no reason when every consumer works
    one frame at a time.
    """
    cmd = [
        _ffmpeg(), "-v", "error",
        *(span.input_args() if span else []), "-i", path,
        "-vf", f"fps={fps},scale={w}:{h},format=gray",
        "-f", "rawvideo", "-pix_fmt", "gray", "-",
    ]
    size = w * h
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    try:
        while True:
            buf = proc.stdout.read(size)
            if not buf or len(buf) < size:
                return
            yield buf
    finally:
        proc.stdout.close()
        proc.wait()


class Frame:
    """The features of one sampled frame.

    Projections are the sums of each column and each row. Two 1D signals rather
    than a 2D image, which is what makes matching cheap: comparing 64 numbers
    instead of 2304, with no loss for the thing being measured, because a
    camera pan shifts every column equally.
    """

    __slots__ = ("mean", "peak", "cols", "rows")

    def __init__(self, mean: float, peak: int, cols: list[int], rows: list[int]):
        self.mean = mean
        self.peak = peak
        self.cols = cols
        self.rows = rows


def features(frame: bytes, w: int = GRAY_W, h: int = GRAY_H) -> Frame:
    """Reduce one grayscale frame to its projections and luminance."""
    cols = [0] * w
    rows = [0] * h
    total = 0
    peak = 0
    i = 0
    for y in range(h):
        row_total = 0
        for x in range(w):
            v = frame[i]
            i += 1
            cols[x] += v
            row_total += v
            if v > peak:
                peak = v
        rows[y] = row_total
        total += row_total
    return Frame(total / float(w * h), peak, cols, rows)


def analyse(path: str, fps: float, w: int = GRAY_W, h: int = GRAY_H,
            span=None) -> list[Frame]:
    """Extract per frame features for a film, or for one range of one."""
    return [features(f, w, h) for f in gray_frames(path, fps, w, h, span)]


# 8x8 is enough to know where in the frame a colour is, which is the difference
# between a blue sky and a blue sea, and small enough to stay free.
COLOUR_W = 8
COLOUR_H = 8


def colour_frames(path: str, fps: float, w: int = COLOUR_W, h: int = COLOUR_H,
                  span=None):
    """Yield w*h*3 bytes of RGB per sampled frame."""
    cmd = [
        _ffmpeg(), "-v", "error",
        *(span.input_args() if span else []), "-i", path,
        "-vf", f"fps={fps},scale={w}:{h}",
        "-f", "rawvideo", "-pix_fmt", "rgb24", "-",
    ]
    size = w * h * 3
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    try:
        while True:
            buf = proc.stdout.read(size)
            if not buf or len(buf) < size:
                return
            yield buf
    finally:
        proc.stdout.close()
        proc.wait()


def mean_colour(frame: bytes) -> tuple[float, float, float]:
    """Average colour of a raw RGB frame, as 0..1."""
    n = len(frame) // 3
    if n == 0:
        return (0.0, 0.0, 0.0)
    r = g = b = 0
    for i in range(0, n * 3, 3):
        r += frame[i]
        g += frame[i + 1]
        b += frame[i + 2]
    return (r / (255.0 * n), g / (255.0 * n), b / (255.0 * n))


def luma_series(path: str, fps: float, span=None):
    """Mean luminance per frame, at whatever rate is asked for.

    Scaling to a single pixel makes this one byte per frame: sampling a two
    hour film at 24 Hz costs 173 kilobytes and a few seconds. That is cheap
    enough to run at the content's own frame rate, which matters because
    flashes are short.

    A lightning strike lasts around 150ms. Sampled at 4 Hz, most of them fall
    between samples and are simply not there. This exists so flash detection
    can run fast while everything else stays slow.
    """
    cmd = [
        _ffmpeg(), "-v", "error",
        *(span.input_args() if span else []), "-i", path,
        "-vf", f"fps={fps},scale=1:1,format=gray",
        "-f", "rawvideo", "-pix_fmt", "gray", "-",
    ]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    return list(raw)


class Luma:
    """A frame reduced to nothing but its brightness, for flash detection."""

    __slots__ = ("mean", "peak")

    def __init__(self, value: int):
        self.mean = float(value)
        self.peak = value


# --- one decode, every stream -------------------------------------------- #
#
# Everything above reads the film on its own, and each of those reads costs the
# same: measured on a three minute film, the grayscale pass took 9.9 seconds,
# the colour pass 10.4, the luma pass 10.8 and scene detection 9.8 — for output
# of 64x36, 8x8, 1x1 and nothing at all. The size of what comes out is
# irrelevant. What costs is decoding the film, and the film was being decoded
# five times.
#
# So it is decoded once and fanned out with a split filter. Same frames, same
# bytes, one pass: 41.4 seconds became 13.0, which is where nearly all of an
# analysis went.


class Decoded:
    """Every stream one decode of a film produced.

    Held as files in a temporary directory rather than in memory. A two hour
    film at 4Hz is 68MB of grayscale alone, and the consumers all read once and
    in order, so a file is the right shape and costs nothing a pipe would not.
    """

    def __init__(self, dir_path, gray_size, colour_size, cuts, has_audio=True):
        self.dir = dir_path
        self._gray_size = gray_size
        self._colour_size = colour_size
        self._cuts = cuts
        self._has_audio = has_audio

    def _chunks(self, name, size):
        path = os.path.join(self.dir, name)
        if size <= 0 or not os.path.exists(path):
            return
        with open(path, "rb") as f:
            while True:
                buf = f.read(size)
                if not buf or len(buf) < size:
                    return
                yield buf

    def gray(self):
        """Grayscale frames, one bytes object each."""
        return self._chunks("gray.raw", self._gray_size)

    def colour(self):
        """Colour frames at the analysis rate."""
        return self._chunks("colour.raw", self._colour_size)

    def flash_colour(self):
        """Colour frames at the flash rate, which is much higher."""
        return self._chunks("flash.raw", self._colour_size)

    def luma(self):
        """Mean brightness per frame, at the flash rate."""
        path = os.path.join(self.dir, "luma.raw")
        if not os.path.exists(path):
            return []
        with open(path, "rb") as f:
            return list(f.read())

    def audio(self):
        """Low passed mono audio, as signed 16 bit samples."""
        import array

        path = os.path.join(self.dir, "audio.raw")
        out = array.array("h")
        if not self._has_audio or not os.path.exists(path):
            return out
        with open(path, "rb") as f:
            raw = f.read()
        out.frombytes(raw[: len(raw) - (len(raw) % 2)])
        return out

    def cuts(self):
        """The times of detected scene cuts, in seconds."""
        return list(self._cuts)

    def close(self):
        shutil.rmtree(self.dir, ignore_errors=True)

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
        return False


def has_audio(path: str) -> bool:
    """Whether this file has an audio stream at all.

    Asked before building the decode rather than discovered during it. `-map
    0:a?` makes the mapping optional and the output file mandatory, so a film
    without audio produces an output with no streams — which ffmpeg treats as
    fatal to the whole command, taking the video with it.
    """
    exe = shutil.which("ffprobe")
    if not exe:
        # Without ffprobe, assume there is audio: asking for it and getting
        # nothing is the situation this is avoiding, but guessing the other way
        # would throw away the audio of every film on a box missing a tool.
        return True
    result = subprocess.run(
        [exe, "-v", "error", "-select_streams", "a", "-show_entries",
         "stream=index", "-of", "csv=p=0", path],
        capture_output=True, text=True, check=False,
    )
    return bool(result.stdout.strip())


def decode(path: str, fps: float, flash_fps: float = 0.0, span=None,
           w: int = GRAY_W, h: int = GRAY_H,
           cw: int = COLOUR_W, ch: int = COLOUR_H,
           scene_threshold: float = 0.35, want_scenes: bool = True,
           audio_rate: int = 1000, audio_cutoff: int = 120):
    """Read a film once and produce every stream the analysis needs.

    The streams are exactly what the separate passes produced, because they are
    the same filters on the same frames — only the decode is shared.

    A film with no audio track is normal and not an error: the audio output is
    asked for separately and its absence leaves an empty array rather than
    failing the whole decode.
    """
    tmp = tempfile.mkdtemp(prefix="componium-decode-")
    seek = span.input_args() if span else []

    branches = []
    outputs = []
    labels = []

    branches.append("[g]fps=%s,scale=%d:%d,format=gray[gray]" % (fps, w, h))
    labels.append("g")
    outputs += ["-map", "[gray]", "-f", "rawvideo", "-pix_fmt", "gray",
                os.path.join(tmp, "gray.raw")]

    branches.append("[c]fps=%s,scale=%d:%d[col]" % (fps, cw, ch))
    labels.append("c")
    outputs += ["-map", "[col]", "-f", "rawvideo", "-pix_fmt", "rgb24",
                os.path.join(tmp, "colour.raw")]

    if flash_fps and flash_fps > 0:
        branches.append("[l]fps=%s,scale=1:1,format=gray[luma]" % flash_fps)
        labels.append("l")
        outputs += ["-map", "[luma]", "-f", "rawvideo", "-pix_fmt", "gray",
                    os.path.join(tmp, "luma.raw")]

        branches.append("[f]fps=%s,scale=%d:%d[flash]" % (flash_fps, cw, ch))
        labels.append("f")
        outputs += ["-map", "[flash]", "-f", "rawvideo", "-pix_fmt", "rgb24",
                    os.path.join(tmp, "flash.raw")]

    if want_scenes:
        # Scene detection needs the frames at their own size, and reports
        # through showinfo on stderr rather than as an output — so it produces
        # nothing and is read from the log.
        branches.append("[s]select='gt(scene,%s)',showinfo[scene]" % scene_threshold)
        labels.append("s")
        outputs += ["-map", "[scene]", "-an", "-f", "null", "-"]

    graph = "[0:v]split=%d%s;%s" % (
        len(labels), "".join("[%s]" % x for x in labels), ";".join(branches))

    # Only when there is audio to take. An output file with no streams in it
    # is fatal to the whole ffmpeg command, not just to itself.
    audio_out = []
    if has_audio(path):
        audio_out = ["-map", "0:a?", "-af", "lowpass=f=%d" % audio_cutoff,
                     "-ac", "1", "-ar", str(audio_rate), "-f", "s16le",
                     os.path.join(tmp, "audio.raw")]

    cmd = ([_ffmpeg(), "-v", "info"] + seek + ["-i", path,
            "-filter_complex", graph] + outputs + audio_out)

    result = subprocess.run(cmd, capture_output=True, text=True,
                            errors="replace", check=False)
    grey = os.path.join(tmp, "gray.raw")
    # Empty counts as missing. ffmpeg creates its outputs before it fails, so
    # a decode that produced nothing leaves a file of zero bytes behind and the
    # analysis carries on with no frames and no complaint.
    empty = not os.path.exists(grey) or os.path.getsize(grey) == 0
    if result.returncode != 0 and empty:
        shutil.rmtree(tmp, ignore_errors=True)
        raise RuntimeError("ffmpeg could not read %s: %s"
                           % (path, (result.stderr or "").strip()[-400:]))

    cuts = []
    if want_scenes:
        cuts = sorted(set(float(m) for m in PTS.findall(result.stderr or "")))

    return Decoded(tmp, w * h, cw * ch * 3, cuts,
                   has_audio=os.path.exists(os.path.join(tmp, "audio.raw")))
