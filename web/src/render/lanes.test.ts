/* What the timeline actually draws.
 *
 * These are the tests that could not exist before. The renderer emits a list
 * of primitives rather than painting, so "a 1.0 gust is drawn taller than a
 * 0.2 gust" is a thing node can check — and it is exactly the class of claim
 * that a passing unit suite used to leave completely unexamined while the page
 * drew something wrong.
 */

import { describe, it, expect } from 'vitest';
import { TimeView } from '../core/view';
import { DrawList } from './drawlist';
import { drawCues, drawCurve, drawRibbon, visibleRange } from './lanes';
import type { Track } from '../core/score';

const theme = {
  ink: '#fff', muted: '#888', line: '#333', grid: '#222',
  event: '#d8a24a', eventSoft: '#333', playhead: '#fff', warn: '#f00',
  channel: { r: '#e37', g: '#7c7', b: '#69e', intensity: '#d8a24a' },
};

const box = { x: 0, y: 0, w: 900, h: 46 };

const gusts: Track = {
  instrument: 'wind.main',
  type: 'cue',
  cues: [
    { t: 10, action: 'gust', params: { intensity: 0.2 }, duration: 4 },
    { t: 30, action: 'gust', params: { intensity: 1.0 }, duration: 4 },
    { t: 50, action: 'pop', params: { intensity: 0.6 } },
  ],
};

function heights(list: DrawList) {
  return list.of('rect').map((r) => r.h);
}

describe('amplitude is visible — the defect this fixes', () => {
  it('draws a harder cue taller than a weaker one of the same length', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCues(list, gusts, view, box, theme);
    const h = heights(list);
    expect(h.length).toBe(2); // the two spans; the momentary cue is not a rect
    expect(h[1]).toBeGreaterThan(h[0] * 1.8);
  });

  it('draws duration as width, independently of amplitude', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCues(list, {
      instrument: 'w', type: 'cue', cues: [
        { t: 10, action: 'a', params: { intensity: 0.5 }, duration: 2 },
        { t: 30, action: 'b', params: { intensity: 0.5 }, duration: 8 },
      ],
    }, view, box, theme);
    const [short, long] = list.of('rect');
    expect(long.w).toBeCloseTo(short.w * 4, 0);
    expect(long.h).toBeCloseTo(short.h, 6);
  });

  /* A momentary cue and a very short span must never be the same shape: the
   * difference between an instant and a held effect is the whole reason spans
   * exist in this format. */
  it('draws a momentary cue as a marker, not as a thin bar', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCues(list, gusts, view, box, theme);
    expect(list.of('dot').length).toBe(1);
    expect(list.of('rect').length).toBe(2);
  });

  it('gives an amplitude-less cue full height rather than none', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCues(list, {
      instrument: 'w', type: 'cue',
      cues: [{ t: 10, action: 'stop', duration: 2 }],
    }, view, box, theme);
    const [r] = list.of('rect');
    expect(r.h).toBeGreaterThan(box.h * 0.6);
  });

  it('takes the brightest colour channel, not the average', () => {
    const view = new TimeView(120, 24).fit();
    const red = new DrawList();
    drawCues(red, {
      instrument: 'l', type: 'cue',
      cues: [{ t: 10, action: 'flash', params: { r: 1, g: 0, b: 0 }, duration: 2 }],
    }, view, box, theme);
    const white = new DrawList();
    drawCues(white, {
      instrument: 'l', type: 'cue',
      cues: [{ t: 10, action: 'flash', params: { r: 1, g: 1, b: 1 }, duration: 2 }],
    }, view, box, theme);
    expect(red.of('rect')[0].h).toBeCloseTo(white.of('rect')[0].h, 6);
  });

  it('outlines a nominated event instead of filling it', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCues(list, {
      instrument: 'w', type: 'cue',
      cues: [{ t: 10, action: 'splash', params: { intensity: 1 }, duration: 2, source: 'water:nominated' }],
    }, view, box, theme);
    const [r] = list.of('rect');
    expect(r.fill).toBeUndefined();
    expect(r.stroke).toBeTruthy();
  });
});

describe('nothing off screen is drawn', () => {
  it('skips cues outside the window and says how many', () => {
    const view = new TimeView(120, 24).set(0, 12);
    const list = new DrawList();
    drawCues(list, gusts, view, box, theme);
    expect(list.of('rect').length).toBe(1);
    expect(list.culled).toBe(2);
  });

  it('finds the visible slice of a large point array without scanning it', () => {
    const points = Array.from({ length: 40000 }, (_, i) => ({ t: i * 0.16, value: { r: 0.5 } }));
    const view = new TimeView(6400, 24).set(3000, 60);
    const { from, to } = visibleRange(points, view);
    expect(points[from].t).toBeLessThanOrEqual(3000);
    expect(points[to].t).toBeGreaterThanOrEqual(3060);
    expect(to - from).toBeLessThan(500);
  });
});

describe('curves', () => {
  const ramp: Track = {
    instrument: 'light.ambient', type: 'curve',
    points: [
      { t: 0, value: { r: 0, g: 0, b: 0 } },
      { t: 20, value: { r: 1, g: 0.6, b: 0.2 } },
      { t: 40, value: { r: 0.2, g: 0.2, b: 0.5 } },
    ],
  };

  it('draws handles when there is room for them', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCurve(list, ramp, 'r', view, box, theme);
    expect(list.of('dot').length).toBe(3);
    expect(list.of('path').length).toBe(1);
  });

  it('puts a higher value higher up the lane', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCurve(list, ramp, 'r', view, box, theme);
    const dots = list.of('dot');
    expect(dots[1].y).toBeLessThan(dots[0].y); // r goes 0 -> 1
  });

  /* The whole point of the envelope: a feature film's worth of points must
   * cost the width of the lane, not the length of the score. */
  it('switches to an envelope when points outnumber pixels', () => {
    const dense: Track = {
      instrument: 'x', type: 'curve',
      points: Array.from({ length: 45000 }, (_, i) => ({
        t: i * 0.16, value: { r: Math.abs(Math.sin(i / 40)) },
      })),
    };
    const view = new TimeView(7200, 24).fit();
    const list = new DrawList();
    drawCurve(list, dense, 'r', view, box, theme);

    expect(list.of('dot').length).toBe(0);
    const paths = list.of('path');
    expect(paths.length).toBe(1);
    // Two points per column at most, however many points went in.
    expect(paths[0].pts.length).toBeLessThanOrEqual(box.w * 4 + 8);
  });

  it('says so, rather than drawing nothing, when a curve is empty', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawCurve(list, { instrument: 'x', type: 'curve', points: [] }, 'r', view, box, theme);
    expect(list.of('text').map((t) => t.s).join(' ')).toContain('double click');
  });
});

describe('the collapsed colour ribbon', () => {
  const ramp: Track = {
    instrument: 'light.ambient', type: 'curve',
    points: [
      { t: 0, value: { r: 0, g: 0, b: 0 } },
      { t: 60, value: { r: 1, g: 0, b: 0 } },
    ],
  };

  it('samples the colour across the window', () => {
    const view = new TimeView(120, 24).fit();
    const list = new DrawList();
    drawRibbon(list, ramp, ['r', 'g', 'b'], view, box, theme);
    const [rib] = list.of('ribbon');
    expect(rib.stops.length).toBeGreaterThan(20);
    expect(rib.stops[0].colour).toBe('rgb(0, 0, 0)');
    /* Half way through a 120s film the 60s ramp has arrived at full red. The
     * sample nearest the midpoint is not exactly at it — there are 96 of them
     * — so this asks for "essentially full red" rather than an exact string,
     * which would only be testing the sample count. */
    const mid = rib.stops.reduce((best, s) =>
      Math.abs(s.at - 0.5) < Math.abs(best.at - 0.5) ? s : best);
    const [r, g, b] = mid.colour.match(/\d+/g)!.map(Number);
    expect(r).toBeGreaterThan(240);
    expect(g).toBe(0);
    expect(b).toBe(0);
  });

  it('follows the window when it is zoomed, not the whole film', () => {
    const view = new TimeView(120, 24).set(0, 30);
    const list = new DrawList();
    drawRibbon(list, ramp, ['r', 'g', 'b'], view, box, theme);
    const [rib] = list.of('ribbon');
    // At 30s the ramp is half way, so the right edge is mid-red, not full.
    const last = rib.stops[rib.stops.length - 1].colour;
    expect(last).not.toBe('rgb(255, 0, 0)');
    expect(last).not.toBe('rgb(0, 0, 0)');
  });
});

describe('the performance budget', () => {
  /* The number that decides whether this scales: a two hour score, eight
   * tracks, drawn at a realistic width. The list is the work, so counting it
   * is a fair proxy for the frame — and it fails loudly if a future change
   * reintroduces per-point drawing. */
  it('draws a two hour score in a bounded number of primitives', () => {
    const points = Array.from({ length: 45000 }, (_, i) => ({
      t: i * 0.16, value: { r: Math.abs(Math.sin(i / 30)), g: 0.4, b: 0.2 },
    }));
    const view = new TimeView(7200, 24);
    const list = new DrawList();
    const started = Date.now();
    for (let lane = 0; lane < 8; lane++) {
      drawCurve(list, { instrument: 'x', type: 'curve', points }, 'r', view, box, theme);
    }
    expect(list.length).toBeLessThan(200);
    expect(Date.now() - started).toBeLessThan(400);
  });
});
