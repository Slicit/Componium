import { describe, it, expect } from 'vitest';
import { Meter } from './meter';

describe('Meter', () => {
  it('says nothing until the window is up', () => {
    const m = new Meter(0, 500);
    expect(m.drew(4, 100)).toBe(false);
    expect(m.rate).toBe(0);
  });

  it('counts drawn frames per second', () => {
    const m = new Meter(0, 500);
    for (let i = 1; i <= 30; i++) m.drew(4, i * 16);
    m.drew(4, 500);
    expect(m.rate).toBeCloseTo(62, 0);
  });

  it('averages what a frame cost', () => {
    const m = new Meter(0, 500);
    m.drew(2, 100);
    m.drew(6, 200);
    m.drew(4, 500);
    expect(m.cost).toBeCloseTo(4, 3);
  });

  it('counts a skipped frame as time and not as a draw', () => {
    /* The whole reason there are two numbers. A room drawing four times a
     * second because the playhead moves four times a second is not a room
     * that is struggling, and a counter that cannot tell those apart sends
     * you optimising a renderer that is idle. */
    const m = new Meter(0, 500);
    m.drew(3, 10);
    m.drew(3, 20);
    for (let t = 30; t <= 500; t += 10) m.skipped(t);
    expect(m.rate).toBeCloseTo(4, 0);
    expect(m.cost).toBeCloseTo(3, 3);
  });

  it('keeps the last real cost through a still window', () => {
    // Zero would read as "drawing is free", which is the opposite of unknown.
    const m = new Meter(0, 500);
    m.drew(7, 100);
    m.drew(7, 500);
    expect(m.cost).toBeCloseTo(7, 3);
    for (let t = 600; t <= 1100; t += 100) m.skipped(t);
    expect(m.rate).toBe(0);
    expect(m.cost).toBeCloseTo(7, 3);
  });

  it('reports once per window rather than once per frame', () => {
    const m = new Meter(0, 500);
    let said = 0;
    for (let t = 16; t <= 2000; t += 16) if (m.drew(4, t)) said++;
    expect(said).toBe(3);
  });
});
