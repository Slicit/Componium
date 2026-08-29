/* The editor's copy of the colour conversion.
 *
 * There are two implementations of this — one in Go for the player, one here
 * for the preview — and they have to agree. A disagreement is not a rendering
 * quirk, it is a preview that lies about what the fixture will do. These
 * assertions are deliberately the same ones as internal/colour/colour_test.go.
 */

import { describe, it, expect } from 'vitest';
import { hsiToRGB, lerpHSI, colourOf, channelsOf, isHSI, valueAt } from './score';
import type { Track } from './score';

describe('hue, saturation and intensity', () => {
  it('gives the primaries', () => {
    expect(hsiToRGB(0, 1, 1)).toEqual([1, 0, 0]);
    expect(hsiToRGB(1 / 3, 1, 1).map((v) => Math.round(v))).toEqual([0, 1, 0]);
    expect(hsiToRGB(2 / 3, 1, 1).map((v) => Math.round(v))).toEqual([0, 0, 1]);
    expect(hsiToRGB(0, 0, 1)).toEqual([1, 1, 1]);
  });

  it('treats intensity as a dimmer', () => {
    const [r, g, b] = hsiToRGB(0, 1, 0.5);
    expect([r, g, b]).toEqual([0.5, 0, 0]);
  });

  it('wraps hue rather than clamping it', () => {
    expect(hsiToRGB(1.25, 1, 1)).toEqual(hsiToRGB(0.25, 1, 1));
    expect(hsiToRGB(-0.25, 1, 1)).toEqual(hsiToRGB(0.75, 1, 1));
  });
});

describe('interpolating a colour', () => {
  /* The seam is red, which is not an obscure corner of a lighting score. */
  it('takes the short way round the wheel', () => {
    const mid = lerpHSI({ h: 0.97, s: 1, i: 1 }, { h: 0.03, s: 1, i: 1 }, 0.5);
    expect(mid.h).toBeCloseTo(0, 6);
    for (let i = 0; i <= 10; i++) {
      const h = lerpHSI({ h: 0.97, s: 1, i: 1 }, { h: 0.03, s: 1, i: 1 }, i / 10).h;
      expect(h > 0.06 && h < 0.94).toBe(false);
    }
  });

  /* White has no hue, so a fade to red must grow into red rather than sweep
   * through whatever number was stored beside the white. */
  it('carries a hue across a point that has none', () => {
    const white = { h: 0.35, s: 0, i: 1 };
    const red = { h: 0, s: 1, i: 1 };
    for (let i = 1; i < 10; i++) {
      expect(lerpHSI(white, red, i / 10).h).toBeCloseTo(0, 6);
    }
  });
});

describe('a track written in hsi', () => {
  const track: Track = {
    instrument: 'light.ambient', type: 'curve', space: 'hsi',
    points: [
      { t: 0, value: { h: 0, s: 1, i: 0 } },
      { t: 10, value: { h: 0, s: 1, i: 1 } },
    ],
  };

  it('is recognised, and names its lanes in the order they are thought about', () => {
    expect(isHSI(track)).toBe(true);
    expect(channelsOf(track)).toEqual(['h', 's', 'i']);
  });

  it('is recognised even when the space was not declared', () => {
    expect(isHSI({ ...track, space: undefined })).toBe(true);
  });

  it('shows the colour a fixture will be sent', () => {
    expect(colourOf({ h: 0, s: 1, i: 1 })).toBe('rgb(255, 0, 0)');
    expect(colourOf({ h: 0, s: 1, i: 0.5 })).toBe('rgb(128, 0, 0)');
    expect(colourOf({ h: 0, s: 0, i: 1 })).toBe('rgb(255, 255, 255)');
  });

  it('interpolates through the colour space, not channel by channel', () => {
    const seam: Track = {
      instrument: 'l', type: 'curve', space: 'hsi',
      points: [
        { t: 0, value: { h: 0.97, s: 1, i: 1 } },
        { t: 10, value: { h: 0.03, s: 1, i: 1 } },
      ],
    };
    const mid = valueAt(seam.points!, 5, ['h', 's', 'i'], true);
    expect(mid.h).toBeCloseTo(0, 6);

    // Without the flag it is the naive average, which is the bug.
    const naive = valueAt(seam.points!, 5, ['h', 's', 'i'], false);
    expect(naive.h).toBeCloseTo(0.5, 3);
  });
});

describe('rgb tracks are untouched', () => {
  const track: Track = {
    instrument: 'light.old', type: 'curve',
    points: [
      { t: 0, value: { r: 0, g: 0, b: 0 } },
      { t: 10, value: { r: 1, g: 0.5, b: 0 } },
    ],
  };

  it('is not mistaken for hsi', () => {
    expect(isHSI(track)).toBe(false);
    expect(channelsOf(track)).toEqual(['r', 'g', 'b']);
  });

  it('still interpolates and still shows its colour', () => {
    const mid = valueAt(track.points!, 5, ['r', 'g', 'b']);
    expect(mid.r).toBeCloseTo(0.5, 3);
    expect(colourOf({ r: 1, g: 0, b: 0 })).toBe('rgb(255, 0, 0)');
  });
});
