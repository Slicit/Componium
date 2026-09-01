import { describe, it, expect } from 'vitest';
import { followFrames, type Frames } from './frameClock';

/** A video that presents frames when told to, and counts what it was asked. */
function filmed(times: number[]) {
  let cb: ((now: number, meta: { mediaTime: number }) => void) | null = null;
  let cancelled: number[] = [];
  return {
    video: {
      currentTime: 0,
      requestVideoFrameCallback(fn: (now: number, meta: { mediaTime: number }) => void) {
        cb = fn;
        return times.length;
      },
      cancelVideoFrameCallback(h: number) { cancelled.push(h); },
    },
    present(t: number) { cb?.(0, { mediaTime: t }); },
    get pending() { return cb !== null; },
    get cancelled() { return cancelled; },
  };
}

/** A frame scheduler under test control. */
function ticking(): Frames & { run(n: number): void; cancelled: number[] } {
  let next: (() => void) | null = null;
  const cancelled: number[] = [];
  let n = 0;
  return {
    request(cb) { next = cb; return ++n; },
    cancel(h) { cancelled.push(h); },
    cancelled,
    run(count: number) {
      for (let i = 0; i < count; i++) {
        const go = next;
        next = null;
        go?.();
      }
    },
  };
}

describe('following the film frame by frame', () => {
  it('reports the presented frame time, not a sample of the clock', () => {
    /* mediaTime is the timestamp of the frame on screen. currentTime is where
     * the element has got to, which is a different and slightly later number,
     * and mixing the two makes a playhead that steps backwards. */
    const f = filmed([]);
    const seen: number[] = [];
    followFrames(f.video, (t) => seen.push(t));
    f.present(1.25);
    f.present(1.2917);
    expect(seen).toEqual([1.25, 1.2917]);
  });

  it('asks for the next frame after every one', () => {
    const f = filmed([]);
    followFrames(f.video, () => {});
    f.present(0.1);
    f.present(0.2);
    f.present(0.3);
    expect(f.pending).toBe(true);
  });

  it('stops, and stays stopped', () => {
    const f = filmed([]);
    const seen: number[] = [];
    const stop = followFrames(f.video, (t) => seen.push(t));
    f.present(0.1);
    stop();
    // A callback already scheduled still arrives; it must not be acted on and
    // must not schedule another.
    f.present(0.2);
    f.present(0.3);
    expect(seen).toEqual([0.1]);
    expect(f.cancelled.length).toBe(1);
  });

  it('falls back to animation frames where the video callback is missing', () => {
    const frames = ticking();
    const video = { currentTime: 0 };
    const seen: number[] = [];
    followFrames(video, (t) => seen.push(t), frames);
    video.currentTime = 0.5;
    frames.run(1);
    video.currentTime = 1.5;
    frames.run(1);
    expect(seen).toEqual([0.5, 1.5]);
  });

  it('stops the fallback too', () => {
    const frames = ticking();
    const video = { currentTime: 0 };
    const seen: number[] = [];
    const stop = followFrames(video, (t) => seen.push(t), frames);
    frames.run(1);
    stop();
    frames.run(3);
    expect(seen).toHaveLength(1);
    expect(frames.cancelled).toHaveLength(1);
  });
});
