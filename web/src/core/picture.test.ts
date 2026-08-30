import { describe, it, expect } from 'vitest';
import { containScale, aspectOf, SCREEN_ASPECT } from './picture';

describe('fitting a film onto the screen in the room', () => {
  it('leaves a film the shape of the screen alone', () => {
    expect(containScale(SCREEN_ASPECT, SCREEN_ASPECT)).toEqual({ x: 1, y: 1 });
  });

  it('insets a scope film top and bottom', () => {
    // 2.39:1 on a 16:9 screen. Full width, and the panel shows above and
    // below, which is what a television does with a scope film.
    const fit = containScale(SCREEN_ASPECT, 2.39);
    expect(fit.x).toBe(1);
    expect(fit.y).toBeLessThan(1);
    expect(fit.y).toBeCloseTo(SCREEN_ASPECT / 2.39, 5);
  });

  it('insets an academy film left and right', () => {
    const fit = containScale(SCREEN_ASPECT, 4 / 3);
    expect(fit.y).toBe(1);
    expect(fit.x).toBeLessThan(1);
  });

  it('never scales past the screen', () => {
    // Whatever the film, the picture stays inside the panel. A fit above 1
    // would poke the picture through the bezel.
    for (const aspect of [0.5, 1, 1.33, 1.78, 1.85, 2.39, 4]) {
      const fit = containScale(SCREEN_ASPECT, aspect);
      expect(fit.x).toBeLessThanOrEqual(1);
      expect(fit.y).toBeLessThanOrEqual(1);
      expect(Math.max(fit.x, fit.y)).toBe(1);
    }
  });

  it('holds the film shape, which is the whole point', () => {
    for (const aspect of [1.33, 1.78, 2.39]) {
      const fit = containScale(SCREEN_ASPECT, aspect);
      // The drawn rectangle is the screen scaled by the fit; its aspect must
      // be the film's.
      expect((SCREEN_ASPECT * fit.x) / fit.y).toBeCloseTo(aspect, 5);
    }
  });

  it('does not guess before the metadata arrives', () => {
    // The normal state for the first frames after a film is chosen. A guess
    // here shows as a visible snap once the real size turns up.
    expect(containScale(SCREEN_ASPECT, 0)).toEqual({ x: 1, y: 1 });
    expect(containScale(SCREEN_ASPECT, NaN)).toEqual({ x: 1, y: 1 });
    expect(containScale(0, 1.78)).toEqual({ x: 1, y: 1 });
  });
});

describe('reading the shape off a video element', () => {
  it('measures a loaded video', () => {
    expect(aspectOf({ videoWidth: 1920, videoHeight: 1080 })).toBeCloseTo(16 / 9, 5);
  });

  it('says nothing about one that has not loaded', () => {
    expect(aspectOf({ videoWidth: 0, videoHeight: 0 })).toBe(0);
    expect(aspectOf(null)).toBe(0);
  });
});
