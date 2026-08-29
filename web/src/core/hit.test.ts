import { describe, it, expect } from 'vitest';
import { TimeView } from './view';
import { layout } from './layout';
import { hitTest, hitRange, GRAB, type HitContext } from './hit';
import { snap, snapTargets } from './snap';
import type { Score } from './score';

const RULER = 26;
const W = 1000;

const score: Score = {
  title: 'x', duration: 100, fps: 24,
  tracks: [
    {
      instrument: 'wind.main', type: 'cue',
      cues: [
        { t: 10, action: 'gust', params: { intensity: 0.5 }, duration: 10 },
        { t: 50, action: 'pop', params: { intensity: 1 } },
      ],
    },
    {
      instrument: 'light.ambient', type: 'curve',
      points: [
        { t: 0, value: { r: 0, g: 0, b: 0 } },
        { t: 40, value: { r: 1, g: 0.5, b: 0 } },
        { t: 80, value: { r: 0.2, g: 0.2, b: 0.5 } },
      ],
    },
  ],
};

function ctx(view = new TimeView(100, 24).fit()): HitContext {
  return {
    score,
    layout: layout(score.tracks, { collapsed: new Set() }),
    view,
    width: W,
    rulerH: RULER,
  };
}

const cueRowY = () => RULER + 5;

describe('hitting cues', () => {
  it('finds the body of a span', () => {
    const hit = hitTest(ctx(), 150, cueRowY());
    expect(hit.k).toBe('cue');
    if (hit.k === 'cue') {
      expect(hit.cue.action).toBe('gust');
      expect(hit.part).toBe('body');
    }
  });

  /* Edges beat the body, because a span narrower than two grab zones would
   * otherwise be untrimmable — and on an edge, trimming is what you meant. */
  it('prefers an edge to the body', () => {
    const view = new TimeView(100, 24).fit();
    const endX = view.toX(20, W);
    const hit = hitTest(ctx(view), endX, cueRowY());
    expect(hit.k).toBe('cue');
    if (hit.k === 'cue') expect(hit.part).toBe('end');
  });

  it('finds the start edge too', () => {
    const view = new TimeView(100, 24).fit();
    const hit = hitTest(ctx(view), view.toX(10, W), cueRowY());
    if (hit.k === 'cue') expect(hit.part).toBe('start');
    else expect.fail('missed the start edge');
  });

  it('gives a momentary cue a grab zone rather than a single pixel', () => {
    const view = new TimeView(100, 24).fit();
    const x = view.toX(50, W);
    for (const dx of [-GRAB + 1, 0, GRAB - 1]) {
      const hit = hitTest(ctx(view), x + dx, cueRowY());
      expect(hit.k).toBe('cue');
    }
    expect(hitTest(ctx(view), x + GRAB * 3, cueRowY()).k).toBe('lane');
  });

  it('returns the lane, with a time, when nothing is there', () => {
    const hit = hitTest(ctx(), 350, cueRowY());
    expect(hit.k).toBe('lane');
    if (hit.k === 'lane') expect(hit.t).toBeCloseTo(35, 1);
  });
});

describe('hitting curve points', () => {
  it('finds a point in its own channel lane', () => {
    const l = layout(score.tracks, { collapsed: new Set() });
    const rRow = l.rows.find((r) => r.channel === 'r')!;
    const view = new TimeView(100, 24).fit();
    /* r is 1.0 at t=40, so the handle sits at the top of its lane. */
    const y = RULER + rRow.y + 3;
    const hit = hitTest(ctx(view), view.toX(40, W), y);
    expect(hit.k).toBe('point');
    if (hit.k === 'point') {
      expect(hit.channel).toBe('r');
      expect(hit.point.t).toBe(40);
    }
  });

  /* The bug the old timeline had: r, g and b handles stacked at the same pixel
   * wherever their values matched, and only the topmost could ever be grabbed.
   * With a lane each, the same x in a different lane is a different point. */
  it('picks the channel whose lane the pointer is in', () => {
    const l = layout(score.tracks, { collapsed: new Set() });
    const view = new TimeView(100, 24).fit();
    const seen = new Set<string>();
    for (const ch of ['r', 'g', 'b']) {
      const row = l.rows.find((r) => r.channel === ch)!;
      const v = score.tracks[1].points![0].value[ch];
      const bottom = row.h - 3;
      const y = RULER + row.y + (bottom - v * (bottom - 3));
      const hit = hitTest(ctx(view), view.toX(0, W), y);
      if (hit.k === 'point') seen.add(hit.channel);
    }
    expect(seen).toEqual(new Set(['r', 'g', 'b']));
  });

  it('misses when the pointer is nowhere near the value', () => {
    const l = layout(score.tracks, { collapsed: new Set() });
    const rRow = l.rows.find((r) => r.channel === 'r')!;
    const view = new TimeView(100, 24).fit();
    // r is 0 at t=0, which is the bottom; look at the top instead.
    const hit = hitTest(ctx(view), view.toX(0, W), RULER + rRow.y + 2);
    expect(hit.k).toBe('lane');
  });
});

describe('the ruler', () => {
  it('reports a time anywhere above the lanes', () => {
    const hit = hitTest(ctx(), 500, 5);
    expect(hit.k).toBe('ruler');
    if (hit.k === 'ruler') expect(hit.t).toBeCloseTo(50, 1);
  });
});

describe('box selection', () => {
  it('catches events overlapping the band, not only those inside it', () => {
    const view = new TimeView(100, 24).fit();
    const c = ctx(view);
    /* A band from 15s to 17s, entirely inside the 10–20s span. */
    const got = hitRange(c, view.toX(15, W), cueRowY(), view.toX(17, W), cueRowY() + 10);
    expect(got.cues.length).toBe(1);
  });

  it('spans several lanes at once', () => {
    const view = new TimeView(100, 24).fit();
    const c = ctx(view);
    const got = hitRange(c, 0, RULER + 1, W, RULER + c.layout.height - 1);
    expect(got.cues.length).toBe(2);
    // Three channel lanes, three points each.
    expect(got.points.length).toBe(9);
  });
});

describe('snapping', () => {
  const view = new TimeView(100, 24).set(0, 100);
  const targets = snapTargets(score, 33);

  it('measures in pixels, so it behaves the same at every zoom', () => {
    const wide = new TimeView(100, 24).set(0, 100);
    const tight = new TimeView(100, 24).set(30, 5);
    /* Half a second away: within eight pixels when the whole film is on
     * screen, nowhere near it when five seconds are. */
    expect(snap(33.5, wide, W, targets, 24).kind).toBe('playhead');
    expect(snap(33.5, tight, W, targets, 24).kind).not.toBe('playhead');
  });

  /* Nearest wins; the playhead only wins a tie. Anything stronger makes
   * snapping unpredictable whenever two targets are close together. */
  it('takes the nearest target, and the playhead on a tie', () => {
    const closerEvent = snapTargets(score, 10.06);
    expect(snap(10.02, view, W, closerEvent, 24).kind).toBe('event');

    const tie = snapTargets(score, 9.98);
    expect(snap(9.99, view, W, tie, 24).kind).toBe('playhead');
  });

  it('snaps to the start and end of other events', () => {
    const r = snap(20.1, view, W, targets, 24);
    expect(r.kind).toBe('event');
    expect(r.t).toBe(20);
  });

  it('will not snap an event to itself', () => {
    const cue = score.tracks[0].cues![0];
    const t = snapTargets(score, -999, new Set([cue]));
    expect(t.events).not.toContain(10);
  });

  it('lands on a frame when nothing is near', () => {
    const r = snap(70.3333, view, W, targets, 24);
    expect(r.kind).toBe('frame');
    expect(Math.abs(r.t * 24 - Math.round(r.t * 24))).toBeLessThan(1e-9);
  });

  it('does nothing at all when suspended', () => {
    const r = snap(33.4, view, W, targets, 24, false);
    expect(r.t).toBe(33.4);
    expect(r.kind).toBeNull();
  });
});
