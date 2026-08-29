/* The view model: zoom, pan, and the arithmetic the whole timeline reads.
 *
 * All of it runs in node with no DOM, which is the point of putting it here
 * rather than in a component. The bugs this file exists to prevent are the
 * ones that are invisible until you are dragging: a window that drifts off the
 * end of the film, a zoom that does not keep the cursor still, a ruler that
 * asks for a million ticks.
 */

import { describe, it, expect } from 'vitest';
import { TimeView, ticks, DEFAULT_VIEW_FRACTION } from './view';

const FILM = 888.064; // sintel, the score we actually have
const FPS = 24;

const view = () => new TimeView(FILM, FPS);

describe('the default window', () => {
  it('shows a tenth of the film, not all of it', () => {
    const v = view();
    expect(v.fraction).toBeCloseTo(DEFAULT_VIEW_FRACTION, 6);
    expect(v.start).toBe(0);
  });

  it('shows the whole of something shorter than one window', () => {
    const v = new TimeView(3, FPS);
    expect(v.fitted).toBe(true);
  });
});

describe('panning', () => {
  it('cannot scroll off the front', () => {
    const v = view().pan(-500);
    expect(v.start).toBe(0);
  });

  it('cannot scroll off the end', () => {
    const v = view().pan(FILM * 2);
    expect(v.end).toBeCloseTo(FILM, 3);
  });

  it('moves by a fraction of what is on screen, not of the film', () => {
    const v = view();
    const before = v.start;
    v.panByFraction(0.5);
    expect(v.start - before).toBeCloseTo(v.span * 0.5, 6);
  });
});

describe('zooming', () => {
  it('keeps the moment under the cursor under the cursor', () => {
    const v = view().set(100, 60);
    const anchor = 0.25;
    const under = v.start + v.span * anchor;
    v.zoomAt(anchor, 0.5);
    expect(v.start + v.span * anchor).toBeCloseTo(under, 6);
  });

  it('still keeps it still when zooming out', () => {
    const v = view().set(100, 60);
    const anchor = 0.8;
    const under = v.start + v.span * anchor;
    v.zoomAt(anchor, 2);
    expect(v.start + v.span * anchor).toBeCloseTo(under, 6);
  });

  it('stops zooming in at a readable frame width', () => {
    const v = view();
    for (let i = 0; i < 80; i++) v.zoom(0.5);
    expect(v.span).toBeCloseTo(v.minSpan(), 6);
    expect(v.span).toBeGreaterThan(0);
  });

  it('never zooms out past the whole film', () => {
    const v = view();
    for (let i = 0; i < 80; i++) v.zoom(2);
    expect(v.span).toBeCloseTo(FILM, 3);
    expect(v.start).toBe(0);
  });

  it('frames a range with a little air around it', () => {
    const v = view().zoomTo(400, 460);
    expect(v.start).toBeLessThan(400);
    expect(v.end).toBeGreaterThan(460);
    expect(v.span).toBeLessThan(80);
  });

  /* An anchored zoom near the end used to be able to leave start beyond the
   * last legal position, which shows as the timeline drifting into blank space
   * that cannot be scrolled back out of. */
  it('leaves the window inside the film after zooming at the far edge', () => {
    const v = view().set(FILM - 20, 20);
    v.zoomAt(1, 4);
    expect(v.start).toBeGreaterThanOrEqual(0);
    expect(v.end).toBeLessThanOrEqual(FILM + 1e-6);
  });
});

describe('mapping to pixels', () => {
  it('round trips a time through a pixel and back', () => {
    const v = view().set(120, 60);
    const t = 150.25;
    expect(v.fromX(v.toX(t, 1200), 1200)).toBeCloseTo(t, 6);
  });

  it('puts the window edges at the lane edges', () => {
    const v = view().set(120, 60);
    expect(v.toX(120, 1000)).toBeCloseTo(0, 6);
    expect(v.toX(180, 1000)).toBeCloseTo(1000, 6);
  });

  it('knows what is off screen, which is how nothing off screen gets drawn', () => {
    const v = view().set(100, 50);
    expect(v.intersects(10, 20)).toBe(false);
    expect(v.intersects(200, 300)).toBe(false);
    expect(v.intersects(90, 110)).toBe(true);
    expect(v.intersects(140, 160)).toBe(true);
    expect(v.intersects(0, 900)).toBe(true);
  });
});

describe('revealing the playhead', () => {
  it('does nothing while it is comfortably in view', () => {
    const v = view().set(100, 60);
    const before = v.snapshot();
    v.reveal(130);
    expect(v.snapshot()).toEqual(before);
  });

  it('nudges when it reaches the edge during playback', () => {
    const v = view().set(100, 60);
    v.reveal(159);
    expect(v.start).toBeGreaterThan(100);
    expect(v.start).toBeLessThan(130);
  });

  it('jumps and re-centres when the playhead is somewhere else entirely', () => {
    const v = view().set(100, 60);
    v.reveal(600);
    expect(v.start).toBeCloseTo(600 - v.span * 0.33, 3);
  });

  it('cannot be pushed outside the film by revealing the very end', () => {
    const v = view().set(0, 60);
    v.reveal(FILM);
    expect(v.end).toBeLessThanOrEqual(FILM + 1e-6);
  });
});

describe('ruler ticks', () => {
  it('lands on values a person recognises, not on 3.7 second intervals', () => {
    const v = view().set(0, 60);
    const t = ticks(v, 1200);
    const steps = t.slice(1).map((x, i) => x.t - t[i].t);
    for (const s of steps) expect([1, 2, 5, 10]).toContain(Math.round(s * 1000) / 1000);
  });

  it('switches to frames when zoomed right in', () => {
    const v = view().set(10, 1);
    const t = ticks(v, 1200);
    expect(t.length).toBeGreaterThan(2);
    const step = t[1].t - t[0].t;
    expect(step).toBeLessThan(0.5);
  });

  it('switches to minutes when showing the whole film', () => {
    const v = view().fit();
    const t = ticks(v, 1200);
    const step = t[1].t - t[0].t;
    expect(step).toBeGreaterThanOrEqual(60);
  });

  it('never asks for an unbounded number of ticks', () => {
    const v = new TimeView(3600 * 4, FPS).fit();
    expect(ticks(v, 20).length).toBeLessThan(4001);
    const tiny = new TimeView(0.02, 240);
    expect(ticks(tiny.fit(), 4000).length).toBeLessThan(4001);
  });

  it('stays inside the film', () => {
    const v = view().fit();
    for (const t of ticks(v, 1200)) {
      expect(t.t).toBeGreaterThanOrEqual(0);
      expect(t.t).toBeLessThanOrEqual(FILM);
    }
  });
});
