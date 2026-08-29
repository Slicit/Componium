import { describe, it, expect } from 'vitest';
import {
  layout, orderTracks, rowAt, canCollapse, summaryLabel,
  ROW_CUE, ROW_CHANNEL, ROW_COLLAPSED,
} from './layout';
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
  /* A multi-channel curve is a compound row *plus* its channels, not a first
   * channel wearing the instrument's name. The old layout made red the head
   * row, so the timeline's first light lane was secretly r and was the one
   * lane with no label. */
  it('gives a colour curve a compound row and one per channel', () => {
    const l = open();
    const rows = l.rows.filter((r) => r.instrument === 'light.ambient');
    expect(rows.length).toBe(4);
    expect(rows[0].head).toBe(true);
    expect(rows[0].channel).toBeUndefined();
    expect(rows[0].draw).toBe('ribbon');
    expect(rows.slice(1).map((r) => r.channel)).toEqual(['r', 'g', 'b']);
  });

  it('gives a cue track one row', () => {
    expect(open().rows.filter((r) => r.instrument === 'wind.main').length).toBe(1);
  });

  /* One channel has nothing to compound, so it gets one row and no ceremony. */
  it('gives a single-channel curve exactly one row, which is editable', () => {
    const rows = open().rows.filter((r) => r.instrument === 'shake.seat');
    expect(rows.length).toBe(1);
    expect(rows[0].head).toBe(true);
    expect(rows[0].channel).toBe('intensity');
    expect(rows[0].editable).toBe(true);
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
    const heads = open().rows.filter((r) => r.head).map((r) => r.instrument);
    expect(heads).toEqual(['wind.main', 'light.ambient', 'shake.seat']);
  });
});

describe('what can be edited', () => {
  /* The compound row shows what the channels add up to. There is nothing on it
   * to drag — moving "the colour" up has no single meaning — so it is drawn
   * and never edited. */
  it('marks the compound row as not editable', () => {
    const l = open();
    const compound = l.rows.find((r) => r.instrument === 'light.ambient' && !r.channel)!;
    expect(compound.editable).toBe(false);
  });

  it('marks every channel lane editable', () => {
    const l = open();
    for (const r of l.rows.filter((x) => x.channel)) expect(r.editable).toBe(true);
  });
});

describe('collapsing', () => {
  it('is offered only where folding means something', () => {
    expect(canCollapse(tracks[1])).toBe(true);
    expect(canCollapse(tracks[2])).toBe(false);
    expect(canCollapse(tracks[0])).toBe(false);
  });

  it('leaves the compound row and drops the channels', () => {
    const l = layout(tracks, { collapsed: new Set(['light.ambient']) });
    const rows = l.rows.filter((r) => r.instrument === 'light.ambient');
    expect(rows.length).toBe(1);
    expect(rows[0].draw).toBe('ribbon');
    expect(rows[0].editable).toBe(false);
    expect(rows[0].h).toBe(ROW_COLLAPSED);
  });

  it('shortens the timeline by exactly the channels it hid', () => {
    const before = open().height;
    const after = layout(tracks, { collapsed: new Set(['light.ambient']) }).height;
    expect(after).toBe(before - ROW_CHANNEL * 3);
  });

  /* Collapsing something that cannot be collapsed must not silently remove
   * its only lane. */
  it('ignores a collapse asked for on a single-channel track', () => {
    const l = layout(tracks, { collapsed: new Set(['shake.seat']) });
    expect(l.rows.filter((r) => r.instrument === 'shake.seat').length).toBe(1);
  });

  it('compounds a non-colour multi-channel curve to an envelope', () => {
    const motion: Track = {
      instrument: 'motion.platform', type: 'curve',
      points: [
        { t: 0, value: { heave: 0, roll: 0 } },
        { t: 5, value: { heave: 1, roll: 0.5 } },
      ],
    };
    const l = layout([motion], { collapsed: new Set() });
    expect(l.rows[0].draw).toBe('envelope');
    expect(summaryLabel(motion)).toBe('all');
    expect(summaryLabel(tracks[1])).toBe('colour');
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
