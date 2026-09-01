import { describe, it, expect } from 'vitest';
import { parseRoute, routeHash, isCurrent } from './route';

describe('reading the address bar', () => {
  it('an empty hash is the studio', () => {
    expect(parseRoute('')).toEqual({ section: '', page: '' });
    expect(parseRoute('#')).toEqual({ section: '', page: '' });
    expect(parseRoute('#/')).toEqual({ section: '', page: '' });
  });

  it('a section, and a page inside it', () => {
    expect(parseRoute('#/admin')).toEqual({ section: 'admin', page: '' });
    expect(parseRoute('#/admin/firmware')).toEqual({ section: 'admin', page: 'firmware' });
  });

  it('forgives the ways a hash gets written', () => {
    /* A hash is a thing people paste, edit and shorten. Every one of these is
     * somebody meaning the same place. */
    for (const written of ['#admin/firmware', '#/admin/firmware/', '#//admin//firmware']) {
      expect(parseRoute(written)).toEqual({ section: 'admin', page: 'firmware' });
    }
  });

  it('ignores anything deeper, rather than failing on it', () => {
    expect(parseRoute('#/admin/firmware/extra')).toEqual({ section: 'admin', page: 'firmware' });
  });
});

describe('writing one', () => {
  it('round trips', () => {
    for (const [section, page] of [['', ''], ['admin', ''], ['admin', 'devices']] as const) {
      expect(parseRoute(routeHash(section, page))).toEqual({ section, page });
    }
  });
});

describe('which link reads as current', () => {
  it('a section matches every page in it', () => {
    const route = parseRoute('#/admin/firmware');
    expect(isCurrent(route, 'admin')).toBe(true);
    expect(isCurrent(route, '')).toBe(false);
  });

  it('a page matches only itself', () => {
    const route = parseRoute('#/admin/firmware');
    expect(isCurrent(route, 'admin', 'firmware')).toBe(true);
    expect(isCurrent(route, 'admin', 'devices')).toBe(false);
  });

  it('the studio is current when nothing is named', () => {
    expect(isCurrent(parseRoute(''), '')).toBe(true);
  });
});
