import { describe, it, expect, beforeEach } from 'vitest';
import {
  History, moveCues, movePoints, removePoints, insertPoints, removeCues,
  resizeCues, withOrphans,
} from './history';
import type { Cue, Point, Track } from './score';

function cueTrack(): Track {
  return {
    instrument: 'wind.main', type: 'cue',
    cues: [
      { t: 10, action: 'gust', params: { intensity: 0.5 }, duration: 4 },
      { t: 30, action: 'gust', params: { intensity: 1 }, duration: 2 },
    ],
  };
}

function curveTrack(): Track {
  return {
    instrument: 'light.ambient', type: 'curve',
    points: [
      { t: 0, value: { r: 0, g: 0, b: 0 } },
      { t: 10, value: { r: 1, g: 0.5, b: 0 } },
      { t: 20, value: { r: 0.2, g: 0.2, b: 0.5 } },
    ],
  };
}

let h: History;
beforeEach(() => { h = new History(); });

describe('undo and redo', () => {
  it('puts a moved event back', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    h.run(moveCues([{ track, cue, from: 10, to: 25 }]));
    expect(cue.t).toBe(25);
    expect(h.undo()).toBe(true);
    expect(cue.t).toBe(10);
    expect(h.redo()).toBe(true);
    expect(cue.t).toBe(25);
  });

  it('refuses to undo past the beginning', () => {
    expect(h.undo()).toBe(false);
    expect(h.canUndo).toBe(false);
  });

  it('throws away the redo stack once you do something new', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    h.run(moveCues([{ track, cue, from: 10, to: 25 }]));
    h.undo();
    expect(h.canRedo).toBe(true);
    h.run(moveCues([{ track, cue, from: 10, to: 40 }]));
    expect(h.canRedo).toBe(false);
  });

  it('names what it will undo, for a menu', () => {
    const track = cueTrack();
    h.run(removeCues(track, [track.cues![0]]));
    expect(h.undoLabel).toBe('Remove event');
  });
});

describe('a drag is one entry, not two hundred', () => {
  /* Without coalescing, undo after a drag steps the event back one pixel at a
   * time — which is not undo, it is a replay. */
  it('collapses a gesture into a single undo', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    for (let px = 1; px <= 50; px++) {
      h.run(moveCues([{ track, cue, from: 10, to: 10 + px * 0.2 }]), 'drag-1');
    }
    h.seal();
    expect(h.depth).toBe(1);
    expect(cue.t).toBeCloseTo(20, 6);
    h.undo();
    expect(cue.t).toBe(10);
  });

  it('keeps where the drag started, not where the last pixel was', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    h.run(moveCues([{ track, cue, from: 10, to: 12 }]), 'g');
    h.run(moveCues([{ track, cue, from: 12, to: 18 }]), 'g');
    h.undo();
    expect(cue.t).toBe(10);
  });

  it('starts a new entry once the gesture is sealed', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    h.run(moveCues([{ track, cue, from: 10, to: 12 }]), 'g');
    h.seal();
    h.run(moveCues([{ track, cue, from: 12, to: 14 }]), 'g');
    expect(h.depth).toBe(2);
  });
});

describe('sorting, and why commands hold objects', () => {
  /* Dragging one event past another re-sorts the array. Anything holding an
   * index is now pointing at a different event; anything holding the object is
   * still right. Undo has to survive that. */
  it('undoes correctly after a move reorders the track', () => {
    const track = cueTrack();
    const first = track.cues![0];
    h.run(moveCues([{ track, cue: first, from: 10, to: 50 }]));
    expect(track.cues!.map((c) => c.t)).toEqual([30, 50]);
    expect(track.cues![1]).toBe(first);
    h.undo();
    expect(track.cues!.map((c) => c.t)).toEqual([10, 30]);
    expect(track.cues![0]).toBe(first);
  });

  it('keeps points sorted when one is dragged past its neighbour', () => {
    const track = curveTrack();
    const point = track.points![0];
    h.run(movePoints([{ track, point, fromT: 0, toT: 15 }]));
    expect(track.points!.map((p) => p.t)).toEqual([10, 15, 20]);
    h.undo();
    expect(track.points!.map((p) => p.t)).toEqual([0, 10, 20]);
  });
});

describe('the orphan rule', () => {
  it('takes the partner when removing would leave one point', () => {
    const track: Track = {
      instrument: 'l', type: 'curve',
      points: [{ t: 0, value: { r: 0 } }, { t: 5, value: { r: 1 } }],
    };
    expect(withOrphans(track, [track.points![0]]).length).toBe(2);
    h.run(removePoints(track, [track.points![0]]));
    expect(track.points!.length).toBe(0);
  });

  it('leaves two behind when removing from three', () => {
    const track = curveTrack();
    h.run(removePoints(track, [track.points![1]]));
    expect(track.points!.length).toBe(2);
  });

  /* Undo has to restore both, or the rule quietly eats a point. */
  it('restores the partner on undo', () => {
    const track: Track = {
      instrument: 'l', type: 'curve',
      points: [{ t: 0, value: { r: 0 } }, { t: 5, value: { r: 1 } }],
    };
    h.run(removePoints(track, [track.points![0]]));
    h.undo();
    expect(track.points!.map((p) => p.t)).toEqual([0, 5]);
  });

  it('says in the label that a partner went too', () => {
    const track: Track = {
      instrument: 'l', type: 'curve',
      points: [{ t: 0, value: { r: 0 } }, { t: 5, value: { r: 1 } }],
    };
    h.run(removePoints(track, [track.points![0]]));
    expect(h.undoLabel).toContain('partner');
  });
});

describe('adding and removing', () => {
  it('inserts in time order', () => {
    const track = curveTrack();
    const p: Point = { t: 5, value: { r: 0.5, g: 0.25, b: 0 } };
    h.run(insertPoints(track, [p]));
    expect(track.points!.map((x) => x.t)).toEqual([0, 5, 10, 20]);
    h.undo();
    expect(track.points!.map((x) => x.t)).toEqual([0, 10, 20]);
  });

  it('round trips a removed cue', () => {
    const track = cueTrack();
    const cue = track.cues![1];
    h.run(removeCues(track, [cue]));
    expect(track.cues!.length).toBe(1);
    h.undo();
    expect(track.cues!.length).toBe(2);
    expect(track.cues![1]).toBe(cue);
  });
});

describe('resizing', () => {
  it('changes a length and puts it back', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    h.run(resizeCues([{ track, cue, from: 4, to: 9 }]));
    expect(cue.duration).toBe(9);
    h.undo();
    expect(cue.duration).toBe(4);
  });

  it('will not resize a span to nothing', () => {
    const track = cueTrack();
    const cue = track.cues![0];
    h.run(resizeCues([{ track, cue, from: 4, to: -3 }]));
    expect(cue.duration).toBeGreaterThan(0);
  });
});

describe('multiple events at once', () => {
  it('moves a selection together and undoes it together', () => {
    const track = cueTrack();
    const [a, b] = track.cues as Cue[];
    h.run(moveCues([
      { track, cue: a, from: 10, to: 15 },
      { track, cue: b, from: 30, to: 35 },
    ]));
    expect(track.cues!.map((c) => c.t)).toEqual([15, 35]);
    h.undo();
    expect(track.cues!.map((c) => c.t)).toEqual([10, 30]);
  });
});

describe('dirty tracking', () => {
  it('knows there is something to save, and that saving cleared it', () => {
    const track = cueTrack();
    expect(h.dirty).toBe(false);
    h.run(moveCues([{ track, cue: track.cues![0], from: 10, to: 11 }]));
    expect(h.dirty).toBe(true);
    h.saved();
    expect(h.dirty).toBe(false);
    h.undo();
    expect(h.dirty).toBe(true);
  });
});
