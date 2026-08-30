import { describe, it, expect } from 'vitest';
import { ALL, DEFAULT_PAGE_SIZE, matches, paginate } from './paging';

const films = [
  'Rebel.Moon.Part.Two.The.Scargiver.2024.MULTi.1080p.WEB.H265-CHiLL.mkv',
  'Wanted.2008.MULTi.TRUEFRENCH.1080p.BluRay.x264-FiDO.mkv',
  'big-buck-bunny.mp4',
  'sintel.mp4',
  'crab-dance.mp4',
];
const name = (s: string) => s;

describe('matches', () => {
  it('finds a word in the middle of a release name', () => {
    // What a person remembers is rarely the first word of the filename.
    expect(matches(films, 'scargiver', name)).toHaveLength(1);
  });

  it('ignores case', () => {
    expect(matches(films, 'SINTEL', name)).toEqual(['sintel.mp4']);
  });

  it('requires every term but not their order', () => {
    expect(matches(films, 'rebel 1080', name)).toHaveLength(1);
    expect(matches(films, '1080 rebel', name)).toHaveLength(1);
  });

  it('matches nothing when one term does not', () => {
    expect(matches(films, 'rebel banana', name)).toHaveLength(0);
  });

  it('returns everything for an empty query', () => {
    expect(matches(films, '', name)).toHaveLength(films.length);
    expect(matches(films, '   ', name)).toHaveLength(films.length);
  });

  it('does not hand back the array it was given', () => {
    const out = matches(films, '', name);
    expect(out).not.toBe(films);
  });
});

describe('paginate', () => {
  const many = Array.from({ length: 34 }, (_, i) => 'film-' + i);

  it('shows the first ten by default', () => {
    const p = paginate(many, 1, DEFAULT_PAGE_SIZE);
    expect(p.items).toHaveLength(10);
    expect(p.items[0]).toBe('film-0');
    expect(p.pages).toBe(4);
    expect(p.first).toBe(1);
    expect(p.last).toBe(10);
    expect(p.total).toBe(34);
  });

  it('counts the last, short page correctly', () => {
    const p = paginate(many, 4, 10);
    expect(p.items).toHaveLength(4);
    expect(p.first).toBe(31);
    expect(p.last).toBe(34);
  });

  it('clamps a page past the end rather than showing nothing', () => {
    // Deleting the last film on the last page otherwise leaves the view on a
    // page that no longer exists, showing an empty table and no clue why.
    const p = paginate(many, 99, 10);
    expect(p.page).toBe(4);
    expect(p.items).toHaveLength(4);
  });

  it('clamps a page before the start', () => {
    expect(paginate(many, 0, 10).page).toBe(1);
    expect(paginate(many, -5, 10).page).toBe(1);
  });

  it('survives a page that is not a number', () => {
    expect(paginate(many, NaN, 10).page).toBe(1);
  });

  it('shows everything when the size says so', () => {
    const p = paginate(many, 1, ALL);
    expect(p.items).toHaveLength(34);
    expect(p.pages).toBe(1);
  });

  it('handles an empty list without claiming a first item', () => {
    const p = paginate([], 1, 10);
    expect(p.items).toHaveLength(0);
    expect(p.pages).toBe(1);
    expect(p.first).toBe(0);
    expect(p.last).toBe(0);
    expect(p.total).toBe(0);
  });

  it('gives one page when everything fits', () => {
    const p = paginate(films, 1, 10);
    expect(p.pages).toBe(1);
    expect(p.items).toHaveLength(5);
  });

  it('does not hand back the array it was given', () => {
    expect(paginate(films, 1, ALL).items).not.toBe(films);
  });
});
