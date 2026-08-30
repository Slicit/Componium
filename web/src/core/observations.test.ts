import { describe, it, expect } from 'vitest';
import { tally, effects, scenes, matching, quietShare, type Observation } from './observations';

/* Modelled on a real trace: crab rave, where the interesting fact was that the
 * model had been shown the right frame and said nothing useful about it. */
const trace: Observation[] = [
  { t: 1, labels: ['water', 'scene-calm'], seen: 'A serene island.' },
  { t: 3, labels: ['water', 'scene-calm'], seen: 'A beach with turquoise water.' },
  { t: 39.5, labels: ['dust', 'splash', 'water', 'scene-active'], seen: 'Crabs kicking up sand.' },
  { t: 53, labels: ['dust', 'scene-active'], seen: 'Sand thrown into the air.' },
  { t: 60, labels: ['scene-calm'], seen: 'Red crabs crawling along the shore.' },
  { t: 62, labels: [], seen: '' },
];

describe('what the model found, counted', () => {
  it('puts the commonest first', () => {
    const got = tally(trace);
    for (let i = 1; i < got.length; i++) {
      expect(got[i].count).toBeLessThanOrEqual(got[i - 1].count);
    }
    expect(got.find((x) => x.label === 'water')!.count).toBe(3);
    expect(got.find((x) => x.label === 'splash')!.count).toBe(1);
  });

  it('breaks ties alphabetically, so two runs can be compared', () => {
    // water and scene-calm both appear three times, and dust and scene-active
    // both twice. Without a tiebreak the order of equals is whatever the map
    // happened to iterate, which would make two runs of the same film look
    // different for no reason.
    const got = tally(trace);
    expect(got.slice(0, 2).map((x) => x.label)).toEqual(['scene-calm', 'water']);
    expect(got.filter((x) => x.count === 2).map((x) => x.label))
      .toEqual(['dust', 'scene-active']);
  });

  it('counts nothing from nothing', () => {
    expect(tally([])).toEqual([]);
  });

  it('separates what it saw from how it described the scene', () => {
    const t = tally(trace);
    expect(effects(t).map((x) => x.label)).toEqual(['water', 'dust', 'splash']);
    expect(scenes(t).map((x) => x.label)).toEqual(['scene-calm', 'scene-active']);
  });
});

describe('searching a description', () => {
  it('finds a label', () => {
    expect(matching(trace, 'dust').map((o) => o.t)).toEqual([39.5, 53]);
  });

  it('finds words the model used that no label caught', () => {
    // The case this exists for. A frame described as sand being kicked up and
    // labelled nothing means the model saw it and the vocabulary missed it —
    // a mapping problem, not a model one, and invisible if you only read
    // labels.
    expect(matching(trace, 'sand').map((o) => o.t)).toEqual([39.5, 53]);
    expect(matching(trace, 'crawling').map((o) => o.t)).toEqual([60]);
  });

  it('does not care about case', () => {
    expect(matching(trace, 'SAND').length).toBe(2);
  });

  it('an empty search is everything, not nothing', () => {
    expect(matching(trace, '').length).toBe(trace.length);
    expect(matching(trace, '   ').length).toBe(trace.length);
  });
});

describe('how much of it said nothing', () => {
  it('counts frames with no effect label', () => {
    // Four of the six carry an effect. The one labelled only scene-calm and
    // the one labelled nothing at all are the quiet pair.
    expect(quietShare(trace)).toBeCloseTo(2 / 6, 6);
  });

  it('a frame labelled only scene-calm is quiet', () => {
    expect(quietShare([{ t: 0, labels: ['scene-calm'] }])).toBe(1);
  });

  it('a frame with any effect is not', () => {
    expect(quietShare([{ t: 0, labels: ['scene-calm', 'dust'] }])).toBe(0);
  });

  it('says nothing about an empty description rather than dividing by it', () => {
    expect(quietShare([])).toBe(0);
  });
});
