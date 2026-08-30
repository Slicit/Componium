import { describe, it, expect } from 'vitest';
import { repeatForAspect, tileWidth } from './tiling';

/* The wall behind the television, which is the case this exists for.
 * The room is 5.0 by 3.0 and the panel photograph is 464 by 1024. */
const WALL_W = 5.0;
const WALL_H = 3.0;
const PANEL_W = 464;
const PANEL_H = 1024;

describe('repeating a material without distorting it', () => {
  it('keeps the tile at the shape it was photographed', () => {
    const r = repeatForAspect(WALL_W, WALL_H, PANEL_W, PANEL_H);
    // The drawn tile is the wall divided by the repeat. Its shape has to be
    // the photograph's, which is the whole requirement.
    const drawnW = WALL_W / r.x;
    const drawnH = WALL_H / r.y;
    expect(drawnW / drawnH).toBeCloseTo(PANEL_W / PANEL_H, 6);
  });

  it('stands one tile up the full height', () => {
    expect(repeatForAspect(WALL_W, WALL_H, PANEL_W, PANEL_H).y).toBe(1);
  });

  it('puts the slats at a believable size', () => {
    // One tile is 1.36m and the photograph holds about fourteen slats, so a
    // slat lands near ten centimetres. Stretched to the wall it would be a
    // third of a metre and stop reading as a slat at all.
    const w = tileWidth(WALL_H, PANEL_W, PANEL_H);
    expect(w).toBeCloseTo(1.359, 3);
    expect(WALL_W / w).toBeCloseTo(3.678, 3);
  });

  it('holds the shape whatever the wall is', () => {
    for (const [w, h] of [[5, 3], [4, 2.4], [8, 3], [2, 4]]) {
      const r = repeatForAspect(w, h, PANEL_W, PANEL_H);
      expect((w / r.x) / (h / r.y)).toBeCloseTo(PANEL_W / PANEL_H, 6);
    }
  });

  it('stacks when asked, and still holds the shape', () => {
    const r = repeatForAspect(WALL_W, WALL_H, 512, 512, 3);
    expect(r.y).toBe(3);
    expect((WALL_W / r.x) / (WALL_H / r.y)).toBeCloseTo(1, 6);
  });

  it('refuses to guess from nonsense', () => {
    expect(repeatForAspect(0, 3, 464, 1024)).toEqual({ x: 1, y: 1 });
    expect(repeatForAspect(5, 3, 464, 0)).toEqual({ x: 1, y: 1 });
    expect(repeatForAspect(5, NaN, 464, 1024)).toEqual({ x: 1, y: 1 });
    expect(tileWidth(0, 464, 1024)).toBe(0);
  });
});
