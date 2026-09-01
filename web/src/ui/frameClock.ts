/* The playhead, at the film's own frame rate.
 *
 * `timeupdate` is the obvious event and it is the wrong one. Every browser
 * throttles it to roughly four a second, which is fine for a clock readout and
 * useless for anything that has to line up with what is on screen: a strobe of
 * twelve pulses over two seconds is a 6Hz square wave, and sampling that four
 * times a second cannot reconstruct it at all. What you see is a handful of
 * flashes at arbitrary moments, landing differently on every pass. Nothing was
 * dropping frames; the room was being handed four light values a second and
 * drawing each one perfectly.
 *
 * `requestVideoFrameCallback` fires once per frame the browser actually
 * presents, and hands over that frame's own presentation time. That is the
 * film's clock rather than a sampling of it, which matters twice: the score
 * steps at the rate the film does, and it steps at the same instants the
 * picture does. The television and the room were already on that clock, since
 * three.js registers the same callback for its video texture. Only the score
 * was not.
 *
 * Animation frames are the fallback, which is the display's rate rather than
 * the film's — more often than needed for a 24p film and never less.
 */

/** What this needs from a video element, so a test need not build one. */
export interface Framed {
  currentTime: number;
  requestVideoFrameCallback?: (
    cb: (now: number, meta: { mediaTime: number }) => void,
  ) => number;
  cancelVideoFrameCallback?: (handle: number) => void;
}

/** Somewhere to schedule the fallback, injectable for the same reason. */
export interface Frames {
  request: (cb: () => void) => number;
  cancel: (handle: number) => void;
}

const ANIMATION: Frames = {
  request: (cb) => globalThis.requestAnimationFrame(cb),
  cancel: (h) => globalThis.cancelAnimationFrame(h),
};

/**
 * Report the film's time every frame until the returned function is called.
 *
 * The stop function is the whole contract: this is started from an effect and
 * a loop that outlives its effect keeps writing a dead component's state, and
 * keeps a video element alive by holding it.
 */
export function followFrames(
  video: Framed,
  onTime: (t: number) => void,
  frames: Frames = ANIMATION,
): () => void {
  let stopped = false;
  let handle = 0;

  if (typeof video.requestVideoFrameCallback === 'function') {
    const step = (_now: number, meta: { mediaTime: number }) => {
      if (stopped) return;
      onTime(meta.mediaTime);
      handle = video.requestVideoFrameCallback!(step);
    };
    handle = video.requestVideoFrameCallback(step);
    return () => {
      stopped = true;
      video.cancelVideoFrameCallback?.(handle);
    };
  }

  const step = () => {
    if (stopped) return;
    onTime(video.currentTime);
    handle = frames.request(step);
  };
  handle = frames.request(step);
  return () => {
    stopped = true;
    frames.cancel(handle);
  };
}
