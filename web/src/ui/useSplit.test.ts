import { describe, it, expect } from 'vitest';
import {
  columnsAt, clampHeight, COLUMNS, MIN_COLUMNS, MAX_COLUMNS,
  MIN_HEIGHT, MAX_HEIGHT, DEFAULT_COLUMNS,
} from './useSplit';

describe('the twelve column split', () => {
  it('lands on a twelfth rather than wherever the pointer was', () => {
    expect(columnsAt(0.5)).toBe(6);
    expect(columnsAt(0.51)).toBe(6);
    expect(columnsAt(0.46)).toBe(6);
    expect(columnsAt(0.25)).toBe(3);
    expect(columnsAt(0.334)).toBe(4);
  });

  /* Half and half must be exactly half, not 47%. That is most of the point of
   * snapping: two machines end up with the same layout, and there is a way
   * back to the middle. */
  it('makes the middle exactly the middle', () => {
    expect(columnsAt(0.5)).toBe(COLUMNS / 2);
    expect(DEFAULT_COLUMNS).toBe(COLUMNS / 2);
  });

  /* Below the floor a pane stops being a preview and becomes a handle you
   * cannot find again. */
  it('never squeezes either pane to nothing', () => {
    expect(columnsAt(0)).toBe(MIN_COLUMNS);
    expect(columnsAt(1)).toBe(MAX_COLUMNS);
    expect(columnsAt(-5)).toBe(MIN_COLUMNS);
    expect(columnsAt(99)).toBe(MAX_COLUMNS);
  });

  it('always leaves the other pane a usable share', () => {
    for (let f = 0; f <= 1.0001; f += 0.02) {
      const n = columnsAt(f);
      expect(COLUMNS - n).toBeGreaterThanOrEqual(MIN_COLUMNS);
      expect(n).toBeGreaterThanOrEqual(MIN_COLUMNS);
    }
  });

  /* Neither is a position a pointer can be at — a NaN comes from dividing by
   * a zero width — so both fall back to the middle rather than slamming a
   * pane against its limit. */
  it('falls back rather than producing a nonsense split', () => {
    expect(columnsAt(NaN)).toBe(DEFAULT_COLUMNS);
    expect(columnsAt(Infinity)).toBe(DEFAULT_COLUMNS);
    expect(columnsAt(-Infinity)).toBe(DEFAULT_COLUMNS);
  });
});

describe('the stage height', () => {
  it('stays between something usable and something absurd', () => {
    expect(clampHeight(50)).toBe(MIN_HEIGHT);
    expect(clampHeight(5000)).toBe(MAX_HEIGHT);
    expect(clampHeight(420)).toBe(420);
  });

  it('rounds to whole pixels', () => {
    expect(clampHeight(301.7)).toBe(302);
  });

  it('falls back rather than producing NaN', () => {
    expect(clampHeight(NaN)).toBe(300);
  });
});
