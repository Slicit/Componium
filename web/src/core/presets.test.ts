import { describe, it, expect } from 'vitest';
import {
  PRESETS, actionForKind, build, presetById, presetsFor, valueOf, type Insertion,
} from './presets';

/** build may refuse; these tests are about what it does when it does not. */
function must(out: Insertion | null): Insertion {
  if (!out) throw new Error('build refused');
  return out;
}

describe('the library', () => {
  it('offers something for every kind the rig has', () => {
    for (const kind of ['fog', 'mist', 'scent', 'wind', 'shake', 'light', 'motion']) {
      expect(presetsFor(kind).length, kind).toBeGreaterThan(0);
    }
  });

  it('offers a fogger only fog shapes', () => {
    // A lamp fading over five seconds is a slow dissolve nobody asked for.
    for (const p of presetsFor('fog')) expect(p.kinds).toContain('fog');
  });

  it('has unique ids', () => {
    const ids = PRESETS.map((p) => p.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('gives every preset a name, a hint and a span', () => {
    for (const p of PRESETS) {
      expect(p.name.length, p.id).toBeGreaterThan(0);
      expect(p.hint.length, p.id).toBeGreaterThan(0);
      expect(p.seconds, p.id).toBeGreaterThan(0);
    }
  });

  it('gives every shape at least two nodes, spanning zero to one', () => {
    for (const p of PRESETS) {
      expect(p.shape.length, p.id).toBeGreaterThanOrEqual(2);
      expect(p.shape[0][0], p.id).toBe(0);
      expect(p.shape[p.shape.length - 1][0], p.id).toBe(1);
    }
  });

  it('keeps every shape node in order and in range', () => {
    for (const p of PRESETS) {
      let last = -1;
      for (const [f, v] of p.shape) {
        expect(f, p.id).toBeGreaterThanOrEqual(last);
        expect(Math.abs(v), p.id).toBeLessThanOrEqual(1);
        last = f;
      }
    }
  });

  it('finds one by id, and says so when there is none', () => {
    expect(presetById('fog-fade')?.name).toBe('Fog, fading');
    expect(presetById('nonsense')).toBeNull();
  });
});

describe('actionForKind', () => {
  it('names only actions the vocabulary already uses', () => {
    // Inventing a verb here produces a cue addressed to an instrument that has
    // never heard of it, which fails at play time rather than here.
    const known = ['flash', 'hit', 'gust', 'burst', 'spray', 'puff'];
    for (const kind of ['light', 'shake', 'wind', 'fog', 'mist', 'scent']) {
      expect(known, kind).toContain(actionForKind(kind));
    }
  });

  it('refuses a kind it has no verb for', () => {
    // Motion is driven as a curve; there is no cue action for it.
    expect(actionForKind('motion')).toBeNull();
    expect(actionForKind('teleporter')).toBeNull();
  });
});

describe('valueOf', () => {
  const ramp = [[0, 0], [1, 1]] as const;

  it('reads a ramp linearly', () => {
    expect(valueOf(ramp, 0)).toBe(0);
    expect(valueOf(ramp, 0.5)).toBeCloseTo(0.5);
    expect(valueOf(ramp, 1)).toBe(1);
  });

  it('holds at the ends rather than extrapolating', () => {
    // The same rule the score uses for curves, so the preview and the inserted
    // points are one shape rather than two that ought to agree.
    expect(valueOf(ramp, -1)).toBe(0);
    expect(valueOf(ramp, 5)).toBe(1);
  });

  it('interpolates between the right pair of nodes', () => {
    const shape = [[0, 0], [0.5, 1], [1, 0]] as const;
    expect(valueOf(shape, 0.25)).toBeCloseTo(0.5);
    expect(valueOf(shape, 0.75)).toBeCloseTo(0.5);
  });

  it('survives an empty shape', () => {
    expect(valueOf([], 0.5)).toBe(0);
  });

  it('does not divide by zero on two nodes at one instant', () => {
    const shape = [[0, 0], [0.5, 0], [0.5, 1], [1, 1]] as const;
    expect(Number.isFinite(valueOf(shape, 0.5))).toBe(true);
  });
});

describe('build', () => {
  const fade = presetById('fog-fade')!;
  const gust = presetById('wind-gust')!;

  it('makes a cue for a dosed device, lasting the whole span', () => {
    const out = must(build(fade, 100, ['output']));
    expect(out.points).toBeUndefined();
    expect(out.cues).toHaveLength(1);
    expect(out.cues![0].t).toBe(100);
    expect(out.cues![0].action).toBe('burst');
    expect(out.cues![0].duration).toBe(fade.seconds);
    expect(out.cues![0].params!.output).toBe(1);
  });

  it('makes points for a dimmed device', () => {
    const out = must(build(gust, 60, ['intensity']));
    expect(out.cues).toBeUndefined();
    expect(out.points!.length).toBe(gust.shape.length);
    expect(out.points![0].t).toBe(60);
    expect(out.points![out.points!.length - 1].t).toBeCloseTo(60 + gust.seconds);
  });

  it('follows the target when it disagrees with the preset', () => {
    /* The bug this exists for: a fog burst dropped on a fog CURVE track was
     * built as a cue, because the preset has an action. The format refuses
     * cues on a curve track, so it drew nothing and would not have saved. */
    const out = must(build(fade, 10, ['output'], { as: 'curve' }));
    expect(out.cues).toBeUndefined();
    expect(out.points!.length).toBe(fade.shape.length);
    expect(out.points![0].value.output).toBe(1);
  });

  it('makes a cue from a preset with no action of its own, when told one', () => {
    const out = must(build(gust, 10, ['intensity'], { as: 'cue', action: 'gust' }));
    expect(out.cues![0].action).toBe('gust');
  });

  it('refuses a cue it has no action for, rather than inventing a verb', () => {
    expect(build(gust, 10, ['intensity'], { as: 'cue' })).toBeNull();
  });

  it('gives every channel the envelope', () => {
    // A motion preset moves each axis it finds: a starting shape, not a
    // finished move.
    const sway = presetById('motion-sway')!;
    const out = must(build(sway, 0, ['heave', 'roll', 'pitch']));
    for (const p of out.points!) {
      expect(Object.keys(p.value).sort()).toEqual(['heave', 'pitch', 'roll']);
    }
  });

  it('reports the span it occupies', () => {
    const out = must(build(gust, 30));
    expect(out.from).toBe(30);
    expect(out.to).toBeCloseTo(30 + gust.seconds);
  });

  it('takes a length that overrides the default', () => {
    const out = must(build(gust, 0, ['intensity'], { seconds: 20 }));
    expect(out.to).toBe(20);
    expect(out.points![out.points!.length - 1].t).toBe(20);
  });

  it('ignores a length of zero or less rather than collapsing the shape', () => {
    expect(must(build(gust, 0, ['intensity'], { seconds: 0 })).to).toBeCloseTo(gust.seconds);
    expect(must(build(gust, 0, ['intensity'], { seconds: -5 })).to).toBeCloseTo(gust.seconds);
  });

  it('scales how hard it is', () => {
    const out = must(build(gust, 0, ['intensity'], { scale: 0.5 }));
    const peak = Math.max(...out.points!.map((p) => p.value.intensity));
    expect(peak).toBeCloseTo(0.5);
  });

  it('never scales a cue past full', () => {
    expect(must(build(fade, 0, ['output'], { scale: 3 })).cues![0].params!.output).toBe(1);
  });

  it('takes a cue peak from the envelope, not from its last value', () => {
    // A fade ends at zero and is still a full dose at the moment it starts.
    expect(must(build(fade, 0, ['output'])).cues![0].params!.output).toBe(1);
  });

  it('builds with no channels without throwing', () => {
    expect(() => build(gust, 0, [])).not.toThrow();
    expect(() => build(fade, 0, [])).not.toThrow();
  });
});
