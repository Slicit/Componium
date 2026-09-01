"""Optional semantic labelling of keyframes by an external model.

Componium ships no model. What it ships is a seam: `--vlm-command` names a
program that takes an image path and prints labels, one per line. Anything you
can wrap in a shell script works, local or remote, and the composer neither
knows nor cares which.

That is deliberate. Bundling a model would date badly, bloat the install for
everyone who does not want it, and make a choice on the user's behalf about
where their film frames are sent. A seam costs forty lines and ages well.

The pass looks at the film on a uniform grid. It used to look only where the
cheap detectors had already flagged something, which was cheaper and wrong:
the detectors nominate by loudness, and half the vocabulary is quiet.
"""

from __future__ import annotations

import concurrent.futures as futures
import os
import shutil
import subprocess
import tempfile

# A label is only useful if something can act on it, so the vocabulary is the
# same one the subtitle mapping uses. A model that says "explosion" produces
# the same cues as a subtitle that said "[explosion]".


# How often the film is looked at.
#
# A uniform grid, not a shortlist. The signals that used to nominate moments —
# loud low frequency, and scene cuts — are structurally blind to most of the
# vocabulary: dust, mist and smoke are quiet, so a shortlist chosen by loudness
# never contains them. Crab rave throws up sand seven times across three
# minutes, and a shortlist of forty frames caught one of them. Not because the
# model could not see the other six; because it was never shown them. On a two
# second grid it finds all seven.
GRID_SECONDS = 2.0

# How many frames are labelled at once.
#
# The seam runs a process per image, so this is a thread pool around a
# subprocess rather than anything clever. Measured through vLLM on one 12GB
# card: 1.6 frames a second one at a time, 10.8 at eight, 17.8 at sixteen,
# 21.1 at twenty-four and nothing further past that.
#
# Eight rather than twenty-four because the composer does not know what is on
# the other end of the seam. It may be a local model that gains nothing from
# being asked twice at once, or a metered API. Eight is most of the benefit at
# a third of the demand, and the number is a flag for anyone who knows better.
VLM_WORKERS = 8

# How far back the second frame is taken from, in seconds. Zero sends one.
#
# Every question the model gets wrong is a temporal one. Dust is thrown and
# snow is settled; a splash needs water that is moving; activity is motion. A
# still cannot answer any of them, which is why three different attempts to fix
# it by rewording the prompt each made it worse — emphasis produced more
# splashes, room to reason produced dust on snow, and naming the near-misses as
# not-effects produced rain in a bamboo forest.
#
# A second frame is evidence rather than instruction, and it is the one change
# that worked: on sintel it settles SCENE from 114 changes to 40 and leaves
# EFFECTS where it was, for 1.4x the time.
#
# One second because that is what was measured. Shorter risks nothing having
# moved; longer risks the shot having cut.
PAIR_SECONDS = 1.0


def cadence(duration: float, every: float = GRID_SECONDS, limit: int = 0):
    """The spacing actually used, once a budget has had its say.

    Its own function because two callers need the same answer and deriving it
    twice is how they came to disagree: the grid widened for a budget and the
    nomination step went on using the spacing it had asked for, so a cap of
    five frames produced five grid points and ninety-five nominations.
    """
    if duration <= 0.0 or every <= 0.0:
        return 0.0
    if limit > 0 and duration / every > limit:
        return duration / limit
    return every


def grid(duration: float, every: float = GRID_SECONDS, limit: int = 0):
    """Times on a uniform grid across a film of this length.

    A budget widens the grid rather than cutting it short. Looking at the
    first forty minutes of a feature closely and the rest not at all is a
    worse answer than looking at all of it a little less closely — and it is
    the kind of wrong that hides, because the score simply has nothing to say
    after a point and nothing reports that it stopped asking.
    """
    step = cadence(duration, every, limit)
    if step <= 0.0:
        return []
    count = max(1, int(duration / step))
    return [round(i * step + step / 2.0, 3) for i in range(count)]


def spacing_of(times):
    """The step of the evenly spaced run in these times, or 0 if there is none.

    Extraction takes one pass over a run of evenly spaced frames and seeks for
    everything else. It reads the step off the times rather than being told it,
    because being told it is the arrangement that already went wrong once: a
    second argument that has to be kept in step with the first eventually is
    not.
    """
    times = sorted(times)
    if len(times) < 3:
        return 0.0
    gaps = {}
    for a, b in zip(times, times[1:]):
        gap = round(b - a, 6)
        if gap > 0:
            gaps[gap] = gaps.get(gap, 0) + 1
    if not gaps:
        return 0.0
    step, seen = max(gaps.items(), key=lambda kv: (kv[1], -kv[0]))
    # One repeated gap is a coincidence, not a grid.
    return step if seen >= 2 else 0.0


def evenly_spaced(times):
    """The run of grid frames, and everything left over.

    The run has to start at the first time and have no holes, because one pass
    of ffmpeg emits frames at a fixed cadence from where it was told to start
    and the nth output is only the nth grid point if none were skipped.
    """
    times = sorted(times)
    step = spacing_of(times)
    if step <= 0.0:
        return [], times
    first = times[0]
    at_index = {}
    for t in times:
        k = (t - first) / step
        i = int(round(k))
        if i >= 0 and abs(k - i) < 1e-6 and i not in at_index:
            at_index[i] = t
    count = 0
    while count in at_index:
        count += 1
    run = [at_index[i] for i in range(count)]
    kept = set(run)
    return run, [t for t in times if t not in kept]


def candidates(envelope, rate: float, cuts=None, limit: int = 0,
               threshold: float = 0.55, spacing: float = 8.0,
               every: float = GRID_SECONDS):
    """Choose the moments worth showing to a model.

    A uniform grid over everything decoded, plus any moment the cheap signals
    nominated that the grid does not already cover.

    The grid is what finds effects. Nomination survives only for the case
    where the grid is coarse — a budget has widened it, or the caller asked
    to look rarely — and a loud moment would otherwise fall a long way from
    the nearest frame. At the default density the grid covers everything and
    nomination adds nothing at all, which is the density doing its job.

    Times are counted from the start of what was decoded, like everything
    else in the composer. Turning them into film times is the span's job.
    """
    duration = (len(envelope or []) / rate) if rate > 0 else 0.0
    chosen = grid(duration, every, limit)
    # Close enough to count as already looked at. An absolute distance, not a
    # share of the spacing: a nomination is within half a spacing of a grid
    # point by construction, so a share of the spacing is a rule that can
    # never fire. At the default density this covers everything, and
    # nomination contributes nothing — which is the point of the density, not
    # an oversight.
    near = GRID_SECONDS / 2.0

    picks = []
    for i, value in enumerate(envelope or []):
        if value >= threshold:
            picks.append((value, i / rate))
    picks.sort(reverse=True)

    # A nomination the grid already covers is not worth a second frame.
    extra = []
    for _score, at in picks:
        if any(abs(at - other) <= near for other in chosen):
            continue
        if all(abs(at - other) >= spacing for other in extra):
            extra.append(at)

    for at in (cuts or []):
        if any(abs(at - other) <= near for other in chosen):
            continue
        if all(abs(at - other) >= spacing for other in extra):
            extra.append(at)

    # Nominations are extra frames, and extra frames cost what every frame
    # costs. A cap that the grid honours and nominations ignore is not a cap.
    # The weakest go first: cuts before loud moments, since a cut is a change
    # of place and a loud moment is closer to being an effect.
    if limit > 0:
        extra = extra[:max(0, limit - len(chosen))]
    return sorted(chosen + extra)


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


def ffmpeg():
    """Where ffmpeg is, or None.

    One seam rather than two lookups, because a test that stands in for the
    decoder has to stand in for the decision to call it as well. Without that,
    a machine with no ffmpeg makes every extraction test assert against an
    empty list it was handed before any stub was reached — which is green
    where ffmpeg is installed and red where it is not.
    """
    return shutil.which("ffmpeg")


def keyframe(path: str, at: float, out_path: str) -> bool:
    """Extract one frame as a JPEG. Returns False if ffmpeg could not."""
    exe = ffmpeg()
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


# Comment lines a wrapper may send that are not the description.
#
# The sentence has ridden on a bare comment since descriptions existed, so it
# stays bare and anything else is prefixed. A reader written before these
# ignores them the way it has always ignored comments.
MARKED = ("place:", "doing:", "busy:")


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
            if said and not said.lower().startswith(MARKED):
                return said
    return ""


def observe_frame(command: str, images, timeout: float = 60.0):
    """Run the command against one frame, or a frame and the one before it.

    Takes a path or a list of them. The frame being asked about goes first, so
    a wrapper written when this passed a single image reads the right one and
    ignores the context it was also handed.

    Returns (labels, seen). A failure is an empty pair rather than an
    exception: a model that chokes on one JPEG must not cost the analysis
    every frame after it.
    """
    if isinstance(images, str):
        images = [images]
    # Last is the frame in question; the seam is handed it first.
    args = [images[-1]] + list(images[:-1])
    try:
        result = subprocess.run(
            command.split() + args,
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


def extract(path: str, times, into: str, span=None,
            gap: float = PAIR_SECONDS):
    """Pull every wanted frame out of the film. Returns [(t, [images])].

    Each entry carries the frame asked about last, and — when a gap is asked
    for — the frame that many seconds before it first, which is the order the
    model is shown them in.

    Times are counted from the start of what was decoded; the span turns them
    into the film's own clock. Without that, a chunk starting an hour in asks
    the film for the frame one second from its beginning, gets it, and files it
    under the hour mark.

    The evenly spaced run comes out in one pass, sampled at the gap rather than
    at the grid so that each frame's predecessor is already in hand. ffmpeg
    decodes every frame whichever rate is asked for — the fps filter only
    chooses which to keep — so pairing costs a few more JPEGs and not a second
    decode. Everything off the grid is seeked to individually.
    """
    exe = ffmpeg()
    if not exe:
        return []
    start = span.decode_start if span is not None else 0.0
    run, rest = evenly_spaced(float(t) for t in times)
    out = []

    if run:
        step = run[1] - run[0]
        paired = gap > 0.0 and step > gap
        rate = gap if paired else step
        first = (run[0] - gap) if paired else run[0]
        if first < 0.0:
            # No room for a predecessor at the very start of the film. The
            # grid keeps its place and the first frame simply goes alone.
            first = run[0]
        count = int(round((run[-1] - first) / rate)) + 1
        pattern = os.path.join(into, "grid-%05d.jpg")
        subprocess.run(
            [exe, "-v", "error", "-y",
             "-ss", "%.3f" % (start + first), "-i", path,
             "-vf", "fps=%.9f,scale=%d:-2" % (1.0 / rate, KEYFRAME_WIDTH),
             "-q:v", "3", "-frames:v", str(count), pattern],
            capture_output=True, check=False,
        )
        for at in run:
            j = int(round((at - first) / rate))
            here = pattern % (j + 1)
            if not os.path.exists(here):
                continue
            images = [here]
            if paired and j > 0:
                before = pattern % j
                if os.path.exists(before):
                    images = [before, here]
            out.append((at, images))

    for i, at in enumerate(rest):
        film_time = span.to_film_time(at) if span is not None else at
        images = []
        if gap > 0.0 and film_time - gap >= 0.0:
            before = os.path.join(into, "seek-%05d-a.jpg" % i)
            if keyframe(path, film_time - gap, before):
                images.append(before)
        here = os.path.join(into, "seek-%05d-b.jpg" % i)
        if keyframe(path, film_time, here):
            images.append(here)
            out.append((at, images))

    return sorted(out)


def observe(path: str, times, command: str, timeout: float = 60.0,
            workers: int = VLM_WORKERS, span=None,
            gap: float = PAIR_SECONDS):
    """Look at a set of moments. Returns one record per frame seen.

    Each record is {"t", "labels", "seen"}, timed from the start of what was
    decoded. This is the pass that costs a GPU and a decode, and the only one
    that cannot be repeated once the film has been analysed and put away — so
    it keeps everything it was told, and the passes that draw conclusions from
    it read this rather than the film.

    Frames are labelled concurrently. The seam is untouched by that: it is
    still one process per image printing labels on stdout, just several at
    once, so a wrapper written for the serial version needs no changes.

    A model that fails on one frame does not stop the run. A composer that
    aborts three quarters of the way through a feature because one JPEG upset
    something is worse than one that returns slightly less.
    """
    seen = []
    with tempfile.TemporaryDirectory(prefix="componium-vlm-") as tmp:
        frames = extract(path, times, tmp, span, gap)
        if not frames:
            return []

        def look(item):
            at, images = item
            labels, said = observe_frame(command, images, timeout)
            return at, labels, said

        with futures.ThreadPoolExecutor(max_workers=max(1, workers)) as pool:
            for at, labels, said in pool.map(look, frames):
                if labels or said:
                    seen.append({"t": round(at, 3), "labels": labels,
                                 "seen": said})
    return sorted(seen, key=lambda o: o["t"])


def parse_marked(text: str, key: str) -> str:
    """One of the prefixed comment lines a scene pass sends back."""
    want = key.lower() + ":"
    for line in (text or "").splitlines():
        line = line.strip()
        if not line.startswith("#"):
            continue
        said = line.lstrip("#").strip()
        if said.lower().startswith(want):
            return said.partition(":")[2].strip()
    return ""


# What the wrapper is told to ask about. Empty is the frame question, which is
# what everything asked before this existed.
ASK_ENV = "COMPONIUM_VLM_ASK"


def observe_scenes(path: str, times, command: str, timeout: float = 60.0,
                   workers: int = VLM_WORKERS, span=None,
                   gap: float = PAIR_SECONDS):
    """Ask what each moment is a scene of, rather than what is in the frame.

    A fifth of the rate the frame pass runs at, because a place is a property
    of a stretch of film and asking every two seconds buys nothing but cost.

    Returns records of {"t", "place", "doing"}. Activity is deliberately not
    among them: measured against the audio and the camera, this pass judges it
    worse than the frame pass does even at the same resolution, so it does not
    get to have an opinion about it.
    """
    seen = []
    with tempfile.TemporaryDirectory(prefix="componium-scene-") as tmp:
        frames = extract(path, times, tmp, span, gap)
        if not frames:
            return []

        def look(item):
            at, images = item
            reply = _ask_scene(command, images, timeout)
            return at, parse_marked(reply, "place"), parse_marked(reply, "doing")

        with futures.ThreadPoolExecutor(max_workers=max(1, workers)) as pool:
            for at, place, doing in pool.map(look, frames):
                if place or doing:
                    seen.append({"t": round(at, 3), "place": place, "doing": doing})
    return sorted(seen, key=lambda o: o["t"])


def _ask_scene(command: str, images, timeout: float) -> str:
    """Run the seam with the scene question set in its environment."""
    if isinstance(images, str):
        images = [images]
    args = [images[-1]] + list(images[:-1])
    env = dict(os.environ)
    env[ASK_ENV] = "scene"
    try:
        result = subprocess.run(
            command.split() + args, capture_output=True, text=True,
            errors="replace", timeout=timeout, check=False, env=env,
        )
    except (OSError, subprocess.TimeoutExpired):
        return ""
    return result.stdout if result.returncode == 0 else ""


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
