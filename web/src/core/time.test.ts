import { describe, it, expect } from 'vitest';
import {
  frameAt, frameStart, snapToFrame, stepFrames, timecode, clockMs,
  parseTime, durationLabel, fpsOf, DEFAULT_FPS,
} from './time';

const FPS = 24;

describe('frames', () => {
  it('a moment belongs to the frame it falls inside', () => {
    expect(frameAt(0, FPS)).toBe(0);
    expect(frameAt(1 / 24 - 0.0001, FPS)).toBe(0);
    expect(frameAt(1 / 24, FPS)).toBe(1);
    expect(frameAt(1, FPS)).toBe(24);
  });

  /* Floating point puts 10 * (1/24) * 24 at 9.999999999999998, which floors to
   * the frame before the one you are on. The playhead then reports the wrong
   * frame for exactly the times a person is most likely to be sitting on,
   * because they got there by stepping. */
  it('does not fall a frame short on exact boundaries', () => {
    for (let n = 0; n < 500; n++) {
      expect(frameAt(frameStart(n, FPS), FPS)).toBe(n);
    }
  });

  it('steps without running off the front or the end', () => {
    expect(stepFrames(0, -1, FPS, 100)).toBe(0);
    expect(stepFrames(100, 5, FPS, 100)).toBe(100);
    expect(stepFrames(1, 1, FPS, 100)).toBeCloseTo(25 / 24, 9);
  });

  it('snaps to the nearest boundary in both directions', () => {
    expect(snapToFrame(1 / 24 + 0.001, FPS)).toBeCloseTo(1 / 24, 9);
    expect(snapToFrame(2 / 24 - 0.001, FPS)).toBeCloseTo(2 / 24, 9);
  });
});

describe('timecode', () => {
  it('reads as HH:MM:SS:FF', () => {
    expect(timecode(0, FPS)).toBe('00:00:00');
    expect(timecode(1, FPS)).toBe('00:01:00');
    expect(timecode(61.5, FPS)).toBe('01:01:12');
    expect(timecode(3661, FPS)).toBe('01:01:01:00');
  });

  it('drops the hours below an hour, and keeps them when asked', () => {
    expect(timecode(65, FPS)).toBe('01:05:00');
    expect(timecode(65, FPS, { hours: true })).toBe('00:01:05:00');
  });

  it('never shows a frame number equal to the frame rate', () => {
    for (let n = 0; n < 200; n++) {
      const ff = Number(timecode(n / FPS, FPS).split(':').pop());
      expect(ff).toBeLessThan(FPS);
    }
  });

  it('still offers the score file"s own millisecond form', () => {
    expect(clockMs(0)).toBe('00:00.000');
    expect(clockMs(61.5)).toBe('01:01.500');
  });
});

describe('parsing what someone typed', () => {
  it('takes the short forms an editor actually types', () => {
    expect(parseTime('90', FPS)).toBe(90);
    expect(parseTime('1:30', FPS)).toBe(90);
    expect(parseTime('1.5s', FPS)).toBe(1.5);
    expect(parseTime('240f', FPS)).toBe(10);
    expect(parseTime('01:30:12', FPS)).toBeCloseTo(90.5, 6);
    expect(parseTime('1:02:03:04', FPS)).toBeCloseTo(3723 + 4 / 24, 6);
  });

  it('reads the score"s own mm:ss.mmm', () => {
    expect(parseTime('01:01.500', FPS)).toBeCloseTo(61.5, 6);
  });

  /* Returning null rather than a guess matters: the caller leaves the field
   * alone instead of moving a cue to somewhere arbitrary. */
  it('refuses rather than guessing', () => {
    for (const bad of ['', '   ', 'abc', '1:2:3:4:5', '::', '1:x', '-5']) {
      expect(parseTime(bad, FPS)).toBeNull();
    }
  });
});

describe('duration labels', () => {
  it('gives frames to short things and minutes to long ones', () => {
    expect(durationLabel(0, FPS)).toBe('instant');
    expect(durationLabel(0.25, FPS)).toBe('6f');
    expect(durationLabel(4, FPS)).toBe('4s');
    expect(durationLabel(185, FPS)).toBe('3m05');
  });
});

describe('frame rate', () => {
  it('takes it from the score when it is there', () => {
    expect(fpsOf({ meta: { media: { fps: 25 } } })).toBe(25);
  });

  it('falls back rather than dividing by zero or NaN', () => {
    expect(fpsOf(null)).toBe(DEFAULT_FPS);
    expect(fpsOf({})).toBe(DEFAULT_FPS);
    expect(fpsOf({ meta: { media: { fps: 0 } } })).toBe(DEFAULT_FPS);
    expect(fpsOf({ meta: { media: { fps: NaN } } })).toBe(DEFAULT_FPS);
  });
});
