import { describe, it, expect } from 'vitest';
import { Activity } from './activity';

/** Run n frames, saying whether each one had motion in it. */
function frames(a: Activity, moving: boolean[]): boolean[] {
  return moving.map((m) => {
    if (m) a.moved();
    return a.take();
  });
}

describe('when the room needs drawing', () => {
  it('draws the first frame, because nothing has been drawn yet', () => {
    expect(new Activity().take()).toBe(true);
  });

  it('draws every frame while something is moving', () => {
    const a = new Activity();
    a.take();
    expect(frames(a, [true, true, true])).toEqual([true, true, true]);
  });

  it('draws one more frame after everything stops', () => {
    // The wrinkle. Without it, a cue ending leaves the room showing the frame
    // before it ended — a stale picture rather than a saved one.
    const a = new Activity();
    a.take();
    expect(frames(a, [true, false, false, false])).toEqual([true, true, false, false]);
  });

  it('stays quiet for as long as nothing happens', () => {
    const a = new Activity();
    a.take();
    frames(a, [true, false]);
    expect(frames(a, new Array(60).fill(false)).some(Boolean)).toBe(false);
  });

  it('wakes on the frame something starts moving again', () => {
    const a = new Activity();
    a.take();
    frames(a, [true, false, false]);
    expect(frames(a, [true])).toEqual([true]);
  });

  it('draws when told, even with nothing moving', () => {
    // A slider moved, a film was chosen, the camera was placed. None of those
    // repeat, so they have to be remembered until a frame carries them.
    const a = new Activity();
    frames(a, [true, false, false]);
    a.changed();
    expect(a.take()).toBe(true);
  });

  it('a change made between frames is not lost', () => {
    const a = new Activity();
    frames(a, [false, false, false]);
    a.changed();
    a.changed();
    expect(a.take()).toBe(true);
    // And having been drawn, it does not keep asking. One more for the
    // trailing frame, then quiet.
    expect(a.take()).toBe(true);
    expect(a.take()).toBe(false);
  });

  it('does not go quiet while motion and changes interleave', () => {
    const a = new Activity();
    a.take();
    a.moved();
    expect(a.take()).toBe(true);
    a.changed();
    expect(a.take()).toBe(true);
    a.moved();
    expect(a.take()).toBe(true);
  });
});
