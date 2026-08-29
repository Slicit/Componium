import { describe, it, expect, beforeEach } from 'vitest';
import { History } from './history';
import {
  splitCue, duplicateCues, nudge, scaleAmplitude, smoothPoints, toggleSpan,
  copy, paste,
} from './edits';
import { cueEnd, type Cue, type Point, type Score } from './score';

function fixture(): Score {
  return {
    title: 'x', duration: 120, fps: 24,
    tracks: [
      {
        instrument: 'wind.main', type: 'cue',
        cues: [
          { t: 10, action: 'gust', params: { intensity: 0.5 }, duration: 4 },
          { t: 30, action: 'pop', params: { intensity: 1 } },
        ],
      },
      {
        instrument: 'light.ambient', type: 'curve',
        points: [
          { t: 0, value: { r: 0, g: 0, b: 0 } },
          { t: 10, value: { r: 1, g: 0.4, b: 0 } },
          { t: 20, value: { r: 0.2, g: 0.2, b: 0.4 } },
        ],
      },
    ],
  };
}

let h: History;
let score: Score;
beforeEach(() => { h = new History(); score = fixture(); });

describe('splitting a span', () => {
  it('makes two spans that together cover the original', () => {
    const track = score.tracks[0];
    const cue = track.cues![0];
    const cmd = splitCue(track, cue, 12)!;
    expect(cmd).not.toBeNull();
    h.run(cmd);
    const spans = track.cues!.filter((c) => (c.duration ?? 0) > 0);
    expect(spans.length).toBe(2);
    expect(spans[0].t).toBe(10);
    expect(spans[0].duration).toBe(2);
    expect(spans[1].t).toBe(12);
    expect(spans[1].duration).toBe(2);
    expect(cueEnd(spans[1])).toBe(14);
  });

  it('gives both halves the original action and parameters', () => {
    const track = score.tracks[0];
    h.run(splitCue(track, track.cues![0], 12)!);
    for (const c of track.cues!.filter((x) => (x.duration ?? 0) > 0)) {
      expect(c.action).toBe('gust');
      expect(c.params!.intensity).toBe(0.5);
    }
  });

  it('undoes as one step, back to a single span', () => {
    const track = score.tracks[0];
    h.run(splitCue(track, track.cues![0], 12)!);
    h.undo();
    expect(track.cues!.length).toBe(2);
    expect(track.cues![0].duration).toBe(4);
  });

  /* Splitting where there is nothing to split produces a zero length piece,
   * which is a span that can never fire. Refusing is the honest answer. */
  it('refuses outside the span, at its very edges, and on a momentary cue', () => {
    const track = score.tracks[0];
    expect(splitCue(track, track.cues![0], 30)).toBeNull();
    expect(splitCue(track, track.cues![0], 10)).toBeNull();
    expect(splitCue(track, track.cues![0], 14)).toBeNull();
    expect(splitCue(track, track.cues![0], 10.01)).toBeNull();
    expect(splitCue(track, track.cues![1], 30.5)).toBeNull();
  });
});

describe('duplicating', () => {
  it('lands the copy immediately after the original', () => {
    const track = score.tracks[0];
    h.run(duplicateCues(track, [track.cues![0]])!);
    const copies = track.cues!.filter((c) => c.action === 'gust');
    expect(copies.length).toBe(2);
    expect(copies[1].t).toBe(14);
    expect(copies[1].duration).toBe(4);
  });

  it('copies the parameters rather than sharing them', () => {
    const track = score.tracks[0];
    h.run(duplicateCues(track, [track.cues![0]])!);
    const [a, b] = track.cues!.filter((c) => c.action === 'gust');
    b.params!.intensity = 0.1;
    expect(a.params!.intensity).toBe(0.5);
  });

  it('shifts a group by the span of the whole group', () => {
    const track = score.tracks[0];
    h.run(duplicateCues(track, track.cues!)!);
    // 10 to 30 is a span of 20, so the copies start at 30 and 50.
    const times = track.cues!.map((c) => c.t).sort((a, b) => a - b);
    expect(times).toEqual([10, 30, 30, 50]);
  });
});

describe('nudging', () => {
  it('moves everything selected, across tracks, as one undo', () => {
    const cue = score.tracks[0].cues![0];
    const point = score.tracks[1].points![1];
    h.run(nudge(score, new Set<Cue | Point>([cue, point]), 1)!);
    expect(cue.t).toBe(11);
    expect(point.t).toBe(11);
    h.undo();
    expect(cue.t).toBe(10);
    expect(point.t).toBe(10);
  });

  it('will not push anything off the front of the film', () => {
    const point = score.tracks[1].points![0];
    h.run(nudge(score, new Set<Cue | Point>([point]), -5)!);
    expect(point.t).toBe(0);
  });

  it('does nothing when nothing is selected', () => {
    expect(nudge(score, new Set(), 1)).toBeNull();
  });
});

describe('amplitude', () => {
  it('scales like a fader, keeping the shape', () => {
    const track = score.tracks[1];
    const [a, b] = [track.points![1], track.points![2]];
    const ratioBefore = a.value.r / b.value.r;
    h.run(scaleAmplitude(score, new Set<Cue | Point>([a, b]), 0.5)!);
    expect(a.value.r).toBe(0.5);
    expect(a.value.r / b.value.r).toBeCloseTo(ratioBefore, 6);
  });

  it('clamps at full rather than going past it', () => {
    const track = score.tracks[1];
    const p = track.points![1];
    h.run(scaleAmplitude(score, new Set<Cue | Point>([p]), 4)!);
    expect(p.value.r).toBe(1);
  });

  it('undoes every channel it touched', () => {
    const track = score.tracks[1];
    const p = track.points![1];
    h.run(scaleAmplitude(score, new Set<Cue | Point>([p]), 0.5)!);
    h.undo();
    expect(p.value).toEqual({ r: 1, g: 0.4, b: 0 });
  });
});

describe('smoothing', () => {
  it('pulls a spike towards its neighbours', () => {
    const track = score.tracks[1];
    const p = track.points![1];
    const before = p.value.r;
    h.run(smoothPoints(track, [p])!);
    expect(p.value.r).toBeLessThan(before);
    expect(p.value.r).toBeGreaterThan(track.points![0].value.r);
  });

  it('leaves the endpoints alone, having no neighbours on both sides', () => {
    const track = score.tracks[1];
    expect(smoothPoints(track, [track.points![0]])).toBeNull();
  });
});

describe('span and instant', () => {
  it('turns a momentary cue into a span and back', () => {
    const track = score.tracks[0];
    const cue = track.cues![1];
    h.run(toggleSpan(track, cue));
    expect(cue.duration).toBe(1);
    h.run(toggleSpan(track, cue));
    expect(cue.duration).toBe(0);
  });
});

describe('the clipboard', () => {
  it('stores times relative to the earliest thing copied', () => {
    const track = score.tracks[0];
    const clip = copy(score, new Set<Cue | Point>(track.cues!))!;
    expect(clip.cues.map((c) => c.t)).toEqual([0, 20]);
  });

  it('pastes at a time, keeping the internal spacing', () => {
    const track = score.tracks[0];
    const clip = copy(score, new Set<Cue | Point>(track.cues!))!;
    h.run(paste(clip, track, 60, score)!);
    const times = track.cues!.map((c) => c.t).sort((a, b) => a - b);
    expect(times).toEqual([10, 30, 60, 80]);
  });

  /* The copy is taken at copy time. Holding the live objects would mean an
   * edit to the original silently changed what the clipboard pastes. */
  it('is not disturbed by later edits to what was copied', () => {
    const track = score.tracks[0];
    const cue = track.cues![0];
    const clip = copy(score, new Set<Cue | Point>([cue]))!;
    cue.params!.intensity = 0.01;
    h.run(paste(clip, track, 60, score)!);
    const pasted = track.cues!.find((c) => c.t === 60)!;
    expect(pasted.params!.intensity).toBe(0.5);
  });

  /* A track holding both cues and points is something the parser refuses, so
   * a cross paste would produce a score that cannot be saved — discovered
   * long after the mistake. */
  it('refuses to paste events into a curve track, or points into a cue track', () => {
    const cueTrack = score.tracks[0];
    const curveTrack = score.tracks[1];
    const cueClip = copy(score, new Set<Cue | Point>(cueTrack.cues!))!;
    const pointClip = copy(score, new Set<Cue | Point>(curveTrack.points!))!;
    expect(paste(cueClip, curveTrack, 50, score)).toBeNull();
    expect(paste(pointClip, cueTrack, 50, score)).toBeNull();
  });

  it('keeps the destination"s other channels when pasting one', () => {
    const curveTrack = score.tracks[1];
    const clip: ReturnType<typeof copy> = {
      fromKind: 'light',
      cues: [],
      points: [{ t: 0, point: { t: 0, value: { r: 0.9 } } }],
    };
    h.run(paste(clip, curveTrack, 5, score)!);
    const pasted = curveTrack.points!.find((p) => p.t === 5)!;
    expect(pasted.value.r).toBe(0.9);
    // g was on its way from 0 to 0.4 across 0-10s, so half way it is 0.2.
    expect(pasted.value.g).toBeCloseTo(0.2, 3);
  });

  it('copies nothing when nothing is selected', () => {
    expect(copy(score, new Set())).toBeNull();
  });
});
