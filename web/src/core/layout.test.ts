import { describe, it, expect } from 'vitest';
import { layout, orderTracks, rowAt, ROW_CUE, ROW_CHANNEL, ROW_COLLAPSED } from './layout';
import type { Track } from './score';

const tracks: Track[] = [
  { instrument: 'wind.main', type: 'cue', cues: [{ t: 1, action: 'gust' }] },
  {
    instrument: 'light.ambient', type: 'curve',
    points: [{ t: 0, value: { r: 0, g: 0, b: 0 } }, { t: 5, value: { r: 1, g: 1, b: 1 } }],
  },
  {
    instrument: 'shake.seat', type: 'curve',
    points: [{ t: 0, value: { intensity: 0 } }, { t: 5, value: { intensity: 1 } }],
  },
];

const open = () => layout(tracks, { collapsed: new Set() });

describe('rows', () => {
  it('gives a cue track one row and a colour curve one per channel', () => {
    const l = open();
    expect(l.rows.filter((r) => r.instrument === 'wind.main').length).toBe(1);
    expect(l.rows.filter((r) => r.instrument === 'light.ambient').length).toBe(3);
    expect(l.rows.filter((r) => r.instrument === 'shake.seat').length).toBe(1);
  });

  it('stacks rows without gaps or overlaps', () => {
    const l = open();
    let y = 0;
    for (const r of l.rows) {
      expect(r.y).toBe(y);
      y += r.h;
    }
    expect(l.height).toBe(y);
  });

  it('names the instrument once per group, on its first row', () => {
    const l = open();
    const heads = l.rows.filter((r) => r.head).map((r) => r.instrument);
    expect(heads).toEqual(['wind.main', 'light.ambient', 'shake.seat']);
  });

  it('puts red first so lanes never reorder between films', () => {
    const l = open();
    const chans = l.rows.filter((r) => r.instrument === 'light.ambient').map((r) => r.channel);
    expect(chans).toEqual(['r', 'g', 'b']);
  });
});

describe('collapsing', () => {
  it('turns a colour curve into one ribbon row', () => {
    const l = layout(tracks, { collapsed: new Set(['light.ambient']) });
    const rows = l.rows.filter((r) => r.instrument === 'light.ambient');
    expect(rows.length).toBe(1);
    expect(rows[0].draw).toBe('ribbon');
    expect(rows[0].h).toBe(ROW_COLLAPSED);
  });

  it('collapses a non-colour curve to an envelope, not a ribbon', () => {
    const l = layout(tracks, { collapsed: new Set(['shake.seat']) });
    expect(l.rows.find((r) => r.instrument === 'shake.seat')!.draw).toBe('envelope');
  });

  it('shortens the whole timeline, which is the point of collapsing', () => {
    const before = open().height;
    const after = layout(tracks, { collapsed: new Set(['light.ambient']) }).height;
    expect(after).toBe(before - ROW_CHANNEL * 3 + ROW_COLLAPSED);
    expect(after).toBeLessThan(before);
  });
});

describe('ordering and hiding', () => {
  it('follows the arrangement a person chose', () => {
    const l = layout(tracks, { collapsed: new Set(), order: ['shake.seat', 'wind.main'] });
    const heads = l.rows.filter((r) => r.head).map((r) => r.instrument);
    expect(heads).toEqual(['shake.seat', 'wind.main', 'light.ambient']);
  });

  /* A rebuild can add a track the saved arrangement has never heard of. It
   * must end up somewhere visible rather than being silently dropped. */
  it('keeps unlisted tracks rather than losing them', () => {
    const idx = orderTracks(tracks, ['light.ambient']);
    expect(idx.length).toBe(3);
    expect(tracks[idx[0]].instrument).toBe('light.ambient');
  });

  it('leaves hidden instruments out entirely', () => {
    const l = layout(tracks, { collapsed: new Set(), hidden: new Set(['light.ambient']) });
    expect(l.rows.some((r) => r.instrument === 'light.ambient')).toBe(false);
    expect(l.height).toBe(ROW_CUE + ROW_CHANNEL);
  });
});

describe('hit testing', () => {
  it('finds the row under a point, and nothing past the end', () => {
    const l = open();
    expect(rowAt(l, 0)!.instrument).toBe('wind.main');
    expect(rowAt(l, ROW_CUE - 1)!.instrument).toBe('wind.main');
    expect(rowAt(l, ROW_CUE)!.instrument).toBe('light.ambient');
    expect(rowAt(l, l.height + 10)).toBeNull();
    expect(rowAt(l, -5)).toBeNull();
  });
});
