import { describe, it, expect } from 'vitest';
import {
  DEFAULT_VIEWPORT, MAX_VIEWPORTS, normalise, normaliseCamera, normaliseList,
  cleanName, sameLayout, put, drop, type Viewport,
} from './viewport';

const view = (over: Partial<Viewport> = {}): Viewport => ({ ...DEFAULT_VIEWPORT, ...over });

describe('normalise', () => {
  it('gives the default for anything that is not an object', () => {
    for (const junk of [null, undefined, 7, 'x', []]) {
      expect(normalise(junk)).toEqual(DEFAULT_VIEWPORT);
    }
  });

  it('clamps a stored split that is out of range', () => {
    expect(normalise({ columns: 99 }).columns).toBe(10);
    expect(normalise({ columns: -4 }).columns).toBe(2);
    expect(normalise({ height: 20000 }).height).toBe(900);
  });

  it('keeps an explicit false rather than treating it as absent', () => {
    expect(normalise({ room: false, force: false })).toMatchObject({ room: false, force: false });
  });

  it('defaults flags a previous version never wrote', () => {
    expect(normalise({ columns: 4 })).toMatchObject({ room: true, force: true, camera: null });
  });
});

describe('normaliseCamera', () => {
  it('takes a complete view', () => {
    expect(normaliseCamera({ pos: [1, 2, 3], target: [0, 1, 2] }))
      .toEqual({ pos: [1, 2, 3], target: [0, 1, 2] });
  });

  it('refuses half a view rather than aiming at the floor', () => {
    expect(normaliseCamera({ pos: [1, 2, 3] })).toBeNull();
    expect(normaliseCamera({ target: [1, 2, 3] })).toBeNull();
  });

  it('refuses a view with a NaN in it', () => {
    expect(normaliseCamera({ pos: [1, NaN, 3], target: [0, 0, 0] })).toBeNull();
  });

  it('refuses the wrong number of axes', () => {
    expect(normaliseCamera({ pos: [1, 2], target: [0, 0, 0] })).toBeNull();
  });
});

describe('cleanName', () => {
  it('collapses whitespace so two names cannot differ by a space', () => {
    expect(cleanName('  wide   room ')).toBe('wide room');
  });
  it('rejects a name with nothing in it', () => {
    expect(cleanName('   ')).toBe('');
    expect(cleanName(42)).toBe('');
  });
  it('caps the length', () => {
    expect(cleanName('x'.repeat(200)).length).toBe(40);
  });
});

describe('put', () => {
  it('adds a viewport', () => {
    const list = put([], 'wide', view({ columns: 9 }));
    expect(list).toHaveLength(1);
    expect(list[0].name).toBe('wide');
  });

  it('replaces in place rather than moving to the end', () => {
    let list = put(put(put([], 'a', view()), 'b', view()), 'c', view());
    list = put(list, 'a', view({ columns: 3 }));
    expect(list.map((v) => v.name)).toEqual(['a', 'b', 'c']);
    expect(list[0].viewport.columns).toBe(3);
  });

  it('refuses an empty name rather than storing a nameless row', () => {
    expect(put([], '   ', view())).toHaveLength(0);
  });

  it('stops adding at the cap but still lets you overwrite', () => {
    let list: ReturnType<typeof put> = [];
    for (let i = 0; i < MAX_VIEWPORTS + 3; i++) list = put(list, 'v' + i, view());
    expect(list).toHaveLength(MAX_VIEWPORTS);
    list = put(list, 'v0', view({ columns: 8 }));
    expect(list).toHaveLength(MAX_VIEWPORTS);
    expect(list[0].viewport.columns).toBe(8);
  });
});

describe('drop', () => {
  it('removes one and leaves the rest', () => {
    const list = put(put([], 'a', view()), 'b', view());
    expect(drop(list, 'a').map((v) => v.name)).toEqual(['b']);
  });
  it('is quiet about a name that is not there', () => {
    expect(drop([], 'nope')).toEqual([]);
  });
});

describe('normaliseList', () => {
  it('drops rows with no usable name', () => {
    expect(normaliseList([{ name: '', viewport: {} }, { name: 'ok', viewport: {} }]))
      .toHaveLength(1);
  });
  it('drops a duplicate name rather than keeping two rows that look alike', () => {
    const out = normaliseList([
      { name: 'a', viewport: { columns: 3 } },
      { name: 'a', viewport: { columns: 9 } },
    ]);
    expect(out).toHaveLength(1);
    expect(out[0].viewport.columns).toBe(3);
  });
  it('gives an empty list for junk', () => {
    expect(normaliseList('nope')).toEqual([]);
    expect(normaliseList(null)).toEqual([]);
  });
});

describe('sameLayout', () => {
  it('ignores the camera, which moves on its own', () => {
    const a = view({ camera: { pos: [1, 1, 1], target: [0, 0, 0] } });
    expect(sameLayout(a, view())).toBe(true);
  });
  it('notices a different split', () => {
    expect(sameLayout(view({ columns: 3 }), view({ columns: 9 }))).toBe(false);
  });
  it('notices the sliders being hidden', () => {
    expect(sameLayout(view({ force: false }), view())).toBe(false);
  });
});
