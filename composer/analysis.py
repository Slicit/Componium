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

import shutil
import subprocess

# 64x36 keeps the 16:9 shape and gives about a degree of angular resolution per
# pixel on a typical shot, which is finer than any effect can act on anyway.
GRAY_W = 64
GRAY_H = 36


def _ffmpeg() -> str:
    exe = shutil.which("ffmpeg")
    if not exe:
        raise SystemExit("ffmpeg not found on PATH")
    return exe


def gray_frames(path: str, fps: float, w: int = GRAY_W, h: int = GRAY_H):
    """Yield one bytes object of w*h grayscale pixels per sampled frame.

    Streams rather than collecting: a two hour film at 8 Hz is 57,600 frames,
    and holding them all costs 130MB for no reason when every consumer works
    one frame at a time.
    """
    cmd = [
        _ffmpeg(), "-v", "error", "-i", path,
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


def analyse(path: str, fps: float, w: int = GRAY_W, h: int = GRAY_H) -> list[Frame]:
    """Extract per frame features for a whole film."""
    return [features(f, w, h) for f in gray_frames(path, fps, w, h)]


# 8x8 is enough to know where in the frame a colour is, which is the difference
# between a blue sky and a blue sea, and small enough to stay free.
COLOUR_W = 8
COLOUR_H = 8


def colour_frames(path: str, fps: float, w: int = COLOUR_W, h: int = COLOUR_H):
    """Yield w*h*3 bytes of RGB per sampled frame."""
    cmd = [
        _ffmpeg(), "-v", "error", "-i", path,
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


def luma_series(path: str, fps: float):
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
        _ffmpeg(), "-v", "error", "-i", path,
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
