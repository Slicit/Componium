import { describe, it, expect } from 'vitest';
import {
  rgbToHSI, hsiToRGB, fromHex, toHex, hexOf, rgbOf, colourOf, writeColour,
} from './score';

describe('a colour, back into channels', () => {
  it('round trips the primaries', () => {
    for (const [h, s, i] of [
      [0, 1, 1], [1 / 3, 1, 1], [2 / 3, 1, 1],
      [1 / 6, 1, 1], [0.5, 1, 1], [5 / 6, 1, 1],
    ]) {
      const [r, g, b] = hsiToRGB(h, s, i);
      const back = rgbToHSI(r, g, b);
      expect(back[0]).toBeCloseTo(h, 5);
      expect(back[1]).toBeCloseTo(s, 5);
      expect(back[2]).toBeCloseTo(i, 5);
    }
  });

  it('round trips arbitrary colours', () => {
    for (const [h, s, i] of [
      [0.07, 0.42, 0.9], [0.61, 0.15, 0.33], [0.88, 0.77, 0.51],
    ]) {
      const [r, g, b] = hsiToRGB(h, s, i);
      const back = rgbToHSI(r, g, b);
      /* Component by component, and not exactly: a round trip through six
       * divisions lands within about 1e-16, which is arithmetic rather than
       * anything a colour has an opinion about. */
      const again = hsiToRGB(back[0], back[1], back[2]);
      expect(again[0]).toBeCloseTo(r, 9);
      expect(again[1]).toBeCloseTo(g, 9);
      expect(again[2]).toBeCloseTo(b, 9);
    }
  });

  it('says grey has no hue rather than guessing one', () => {
    // Every hue makes grey at zero saturation, so there is nothing to read.
    // The caller keeps the hue it had; inventing one here would swing a point
    // to red the moment its saturation came back up.
    const [h, s, i] = rgbToHSI(0.5, 0.5, 0.5);
    expect(h).toBe(0);
    expect(s).toBe(0);
    expect(i).toBeCloseTo(0.5, 6);
  });

  it('reads black without dividing by it', () => {
    expect(rgbToHSI(0, 0, 0)).toEqual([0, 0, 0]);
  });

  it('keeps hue on the wheel', () => {
    for (let n = 0; n < 24; n++) {
      const [r, g, b] = hsiToRGB(n / 24, 1, 1);
      const h = rgbToHSI(r, g, b)[0];
      expect(h).toBeGreaterThanOrEqual(0);
      expect(h).toBeLessThan(1.000001);
    }
  });
});

describe('hex, which is all a colour input speaks', () => {
  it('goes out and comes back', () => {
    for (const hex of ['#000000', '#ffffff', '#ff0000', '#3b82f6', '#7f7f7f']) {
      const rgb = fromHex(hex)!;
      expect(toHex(rgb[0], rgb[1], rgb[2])).toBe(hex);
    }
  });

  it('takes it with or without the hash', () => {
    expect(fromHex('ff0000')).toEqual(fromHex('#ff0000'));
  });

  it('refuses anything that is not six digits', () => {
    // A colour input only ever produces the six digit form, so anything else
    // is a caller holding something different. Guessing would turn a typo
    // into a colour.
    expect(fromHex('#fff')).toBeNull();
    expect(fromHex('red')).toBeNull();
    expect(fromHex('')).toBeNull();
    expect(fromHex('#gggggg')).toBeNull();
  });
});

describe('what colour a set of channels is', () => {
  it('answers the same way whichever formatter asks', () => {
    // One rule, several formats. The timeline and the editor disagreeing about
    // the colour of a cue is the bug this shape exists to prevent.
    const hsi = { h: 0.6, s: 0.8, i: 0.9 };
    const rgb = rgbOf(hsi)!;
    expect(hexOf(hsi)).toBe(toHex(rgb[0], rgb[1], rgb[2]));
    expect(colourOf(hsi)).toBe(
      `rgb(${Math.round(rgb[0] * 255)}, ${Math.round(rgb[1] * 255)}, ${Math.round(rgb[2] * 255)})`,
    );
  });

  it('reads a track written the older way, in rgb', () => {
    expect(hexOf({ r: 1, g: 0, b: 0 })).toBe('#ff0000');
  });

  it('has nothing to say about params with no colour in them', () => {
    expect(rgbOf({ output: 0.5 })).toBeNull();
    expect(hexOf(undefined)).toBeNull();
  });

  it('survives a round trip through the picker', () => {
    // What the editor actually does: read the colour, hand it to an input,
    // take back what the input gives and store it again.
    const start = { h: 0.33, s: 0.7, i: 0.85 };
    const picked = fromHex(hexOf(start)!)!;
    const [h, s, i] = rgbToHSI(picked[0], picked[1], picked[2]);
    // Eight bits a channel, so the trip is lossy; it has to land close enough
    // that nudging a point does not visibly move it.
    expect(h).toBeCloseTo(start.h, 2);
    expect(s).toBeCloseTo(start.s, 2);
    expect(i).toBeCloseTo(start.i, 2);
  });
});

describe('writing a picked colour back into channels', () => {
  it('sets hue, saturation and intensity on an HSI point', () => {
    const p: Record<string, number> = { h: 0, s: 0, i: 0 };
    writeColour(p, '#00ff00');
    expect(p.h).toBeCloseTo(1 / 3, 2);
    expect(p.s).toBeCloseTo(1, 3);
    expect(p.i).toBeCloseTo(1, 3);
  });

  it('sets r, g and b on a track written the older way', () => {
    const p: Record<string, number> = { r: 0, g: 0, b: 0 };
    writeColour(p, '#3366ff');
    expect(p.r).toBeCloseTo(0.2, 2);
    expect(p.g).toBeCloseTo(0.4, 2);
    expect(p.b).toBeCloseTo(1, 2);
  });

  it('keeps the hue when the colour picked is grey', () => {
    // Grey has no hue - every hue makes it at zero saturation - so the
    // conversion reports 0. Taking that literally would swing a point to red,
    // invisibly, until its saturation came back up and showed it.
    const p: Record<string, number> = { h: 0.72, s: 0.9, i: 0.8 };
    writeColour(p, '#808080');
    expect(p.h).toBeCloseTo(0.72, 6);
    expect(p.s).toBe(0);
    expect(p.i).toBeCloseTo(0.502, 2);
  });

  it('white is the same case, and keeps the hue too', () => {
    const p: Record<string, number> = { h: 0.4, s: 0.5, i: 0.5 };
    writeColour(p, '#ffffff');
    expect(p.h).toBeCloseTo(0.4, 6);
    expect(p.s).toBe(0);
    expect(p.i).toBeCloseTo(1, 3);
  });

  it('touches only the channels already there', () => {
    // A point storing hue and saturation but no intensity is a point about
    // hue. Giving it a third channel would change what it means as well as
    // what it looks like.
    const p: Record<string, number> = { h: 0, s: 0 };
    writeColour(p, '#00ff00');
    expect(Object.keys(p).sort()).toEqual(['h', 's']);
  });

  it('does nothing at all with something that is not a colour', () => {
    const p: Record<string, number> = { h: 0.5, s: 0.5, i: 0.5 };
    writeColour(p, 'not a colour');
    expect(p).toEqual({ h: 0.5, s: 0.5, i: 0.5 });
  });

  it('leaves params with no colour channels alone', () => {
    const p: Record<string, number> = { output: 0.7 };
    writeColour(p, '#ff0000');
    expect(p).toEqual({ output: 0.7 });
  });

  it('round trips through the swatch', () => {
    // What the editor does end to end: show the colour, take back what the
    // input gives, store it, and show it again unchanged.
    const p: Record<string, number> = { h: 0.58, s: 0.64, i: 0.77 };
    const shown = hexOf(p)!;
    writeColour(p, shown);
    expect(hexOf(p)).toBe(shown);
  });
});
