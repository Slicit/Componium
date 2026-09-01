/* The library and the timeline have to agree, and this is what makes them.
 *
 * Reported as "I insert a Strobe and get one light event with no intensity and
 * no points". Three separate ways the same sentence can come true, all of them
 * a preset being mistranslated on the way into a track:
 *
 *   - a shape of twelve pulses collapsed into one cue, because an event device
 *     was assumed to want one dose;
 *   - the number landing in `r`, `g` and `b` on a track whose every other cue
 *     was written in `h`, `s` and `i`, because channels were read from points
 *     and a cue track has none;
 *   - the same number written into a hue as well as an intensity, because a
 *     channel was a channel.
 *
 * So the rule, and the reason this file exists rather than a comment: anything
 * the picker offers must insert, into the track it was offered for, something
 * that keeps the shape's pulses and lands in that track's own channels. Adding
 * a preset, a kind, or a channel vocabulary without honouring that fails here.
 */

import { describe, it, expect } from 'vitest';
import { insertPreset } from './edits';
import { PRESETS, presetsFor, presetById, pulses, actionForKind } from './presets';
import { History } from './history';
import { channelsForKind, channelsOf } from './score';
import type { Rig, Track } from './score';

const KINDS = [...new Set(PRESETS.flatMap((p) => p.kinds))].sort();

const rig = {
  name: 'parity',
  instruments: KINDS.map((kind) => ({ id: kind + '.main', kind })),
} as Rig;

function run(cmd: ReturnType<typeof insertPreset>) {
  const h = new History();
  if (cmd) h.run(cmd);
  return cmd;
}

const track = (kind: string, type: 'cue' | 'curve'): Track =>
  (type === 'cue'
    ? { instrument: kind + '.main', type, cues: [] }
    : { instrument: kind + '.main', type, points: [] }) as Track;

/* --- what is offered is what can be built ------------------------------- */

describe('the picker offers exactly what the insert can build', () => {
  for (const kind of KINDS) {
    for (const holds of ['curve', 'cue'] as const) {
      const offered = new Set(presetsFor(kind, holds).map((p) => p.id));

      it('agrees with itself for a ' + kind + ' ' + holds + ' track', () => {
        for (const preset of PRESETS) {
          if (preset.kinds.length && !preset.kinds.includes(kind)) continue;
          const t = track(kind, holds);
          const built = insertPreset(t, preset, 10, channelsOf(t, rig), {}, rig);
          /* Both directions. Offering something that refuses is an insert that
           * silently does nothing; refusing something that would have worked
           * is a shape a person cannot reach. */
          expect(!!built, preset.id + ' on a ' + kind + ' ' + holds + ' track')
            .toBe(offered.has(preset.id));
        }
      });
    }
  }
});

/* --- the shape survives the trip ---------------------------------------- */

describe('an inserted preset keeps its shape', () => {
  for (const kind of KINDS) {
    for (const holds of ['curve', 'cue'] as const) {
      for (const preset of presetsFor(kind, holds)) {
        it(preset.id + ' into a ' + kind + ' ' + holds + ' track', () => {
          const t = track(kind, holds);
          const channels = channelsOf(t, rig);
          run(insertPreset(t, preset, 10, channels, {}, rig));

          if (holds === 'cue') {
            expect(t.points ?? []).toHaveLength(0);
            /* One cue per pulse: a single gesture stays one cue, and a strobe
             * arrives as a strobe. */
            expect(t.cues).toHaveLength(pulses(preset.shape).length);
            for (const c of t.cues!) {
              expect(c.action).toBeTruthy();
              expect(c.duration).toBeGreaterThan(0);
            }
          } else {
            expect(t.cues ?? []).toHaveLength(0);
            expect(t.points).toHaveLength(preset.shape.length);
            /* One point is not a curve, and the format says so. */
            expect(t.points!.length).toBeGreaterThanOrEqual(2);
          }
        });
      }
    }
  }
});

/* --- the channels are the track's, not the kind's guess ----------------- */

describe('an insert writes the channels the track is already written in', () => {
  for (const kind of KINDS) {
    for (const holds of ['curve', 'cue'] as const) {
      for (const preset of presetsFor(kind, holds)) {
        it(preset.id + ' into a ' + kind + ' ' + holds + ' track', () => {
          const t = track(kind, holds);
          const want = [...channelsForKind(kind)].sort();
          run(insertPreset(t, preset, 10, channelsOf(t, rig), {}, rig));
          const written = holds === 'cue'
            ? t.cues!.map((c) => c.params ?? {})
            : t.points!.map((p) => p.value ?? {});
          for (const v of written) {
            expect(Object.keys(v).sort()).toEqual(want);
          }
        });
      }
    }
  }
});

describe('a cue track has channels, and they come from its cues', () => {
  /* The composer writes light events in hue, saturation and intensity. The
   * fallback answers red, green, blue — so the insert used to land in a
   * vocabulary nothing else in the track spoke, and the editor, which offers
   * the lanes the track uses, had no intensity to show. */
  const hsi = {
    instrument: 'light.event', type: 'cue',
    cues: [{ t: 1, action: 'flash', params: { h: 0.5, s: 0.2, i: 1 }, duration: 0.2 }],
  } as unknown as Track;

  it('reads them', () => {
    expect(channelsOf(hsi, rig)).toEqual(['h', 's', 'i']);
  });

  it('inserts into them', () => {
    run(insertPreset(hsi, presetById('light-flash')!, 10, channelsOf(hsi, rig), {}, rig));
    const made = hsi.cues!.filter((c) => c.t >= 10);
    expect(made.length).toBeGreaterThan(0);
    for (const c of made) expect(Object.keys(c.params!).sort()).toEqual(['h', 'i', 's']);
  });
});

/* --- the number means a level, not a colour ----------------------------- */

describe('a shape drives the level and leaves the colour alone', () => {
  const ambient = (): Track => ({
    instrument: 'light.ambient', type: 'curve', space: 'hsi',
    points: [
      { t: 0, value: { h: 0.6, s: 0.8, i: 0.2 } },
      { t: 100, value: { h: 0.6, s: 0.8, i: 0.2 } },
    ],
  }) as unknown as Track;

  it('a fade up brightens without changing hue', () => {
    const t = ambient();
    run(insertPreset(t, presetById('light-fade-in')!, 10, channelsOf(t, rig), {}, rig));
    const made = t.points!.filter((p) => p.t >= 10 && p.t <= 13);
    expect(made.length).toBeGreaterThan(1);
    for (const p of made) {
      expect(p.value.h).toBeCloseTo(0.6, 3);
      expect(p.value.s).toBeCloseTo(0.8, 3);
    }
    /* And it did what it was for. */
    expect(Math.max(...made.map((p) => p.value.i))).toBeGreaterThan(0.9);
  });

  it('a flash on an h/s/i cue track is white rather than red', () => {
    const t = {
      instrument: 'light.event', type: 'cue',
      cues: [{ t: 1, action: 'flash', params: { h: 0.5, s: 0.2, i: 1 }, duration: 0.2 }],
    } as unknown as Track;
    run(insertPreset(t, presetById('light-flash')!, 10, channelsOf(t, rig), {}, rig));
    const made = t.cues!.find((c) => c.t >= 10)!;
    expect(made.params!.i).toBeCloseTo(1, 3);
    expect(made.params!.s).toBe(0);
  });
});

/* --- repetition arrives as repetition ----------------------------------- */

describe('a strobe is twelve flashes', () => {
  it('on a cue track', () => {
    const t = track('light', 'cue');
    run(insertPreset(t, presetById('light-strobe')!, 10, channelsOf(t, rig), {}, rig));
    expect(t.cues).toHaveLength(12);
    /* Inside the span it was given, and none of them zero length. */
    for (const c of t.cues!) {
      expect(c.t).toBeGreaterThanOrEqual(10);
      expect(c.t + (c.duration as number)).toBeLessThanOrEqual(12.001);
      expect(c.duration).toBeGreaterThan(0);
    }
  });

  it('and a single gesture is still one cue', () => {
    const t = track('fog', 'cue');
    run(insertPreset(t, presetById('fog-burst')!, 10, channelsOf(t, rig), {}, rig));
    expect(t.cues).toHaveLength(1);
  });
});

describe('pulses', () => {
  it('counts a square wave', () => {
    expect(pulses([[0, 0], [0.1, 1], [0.2, 0], [0.3, 1], [0.4, 0], [1, 0]]))
      .toHaveLength(2);
  });

  it('calls a shape that never rests one gesture', () => {
    expect(pulses([[0, 0.2], [0.5, 1], [1, 0.2]])).toHaveLength(1);
  });

  it('opens at the edge before the rise, not at the peak', () => {
    expect(pulses([[0, 0], [0.5, 1], [1, 0]])[0][0]).toBe(0);
  });

  it('has nothing to say about a flat zero', () => {
    expect(pulses([[0, 0], [1, 0]])).toEqual([]);
  });
});

/* --- the vocabulary is closed on purpose -------------------------------- */

describe('every kind the library names is one the rest of the app knows', () => {
  it('has an action, or is curve-only by decision rather than by omission', () => {
    /* Motion is the only kind driven by where to be rather than what to do.
     * If this list grows, it grew because somebody chose it: a kind with no
     * action gets no cue presets at all, and the picker will go empty. */
    const curveOnly = KINDS.filter((k) => !actionForKind(k));
    expect(curveOnly).toEqual(['motion']);
  });

  it('has channels', () => {
    for (const kind of KINDS) expect(channelsForKind(kind).length).toBeGreaterThan(0);
  });
});
