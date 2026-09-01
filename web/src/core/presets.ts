/* A library of effects to drop onto a track.
 *
 * Authoring by hand meant clicking points one at a time, and the shapes worth
 * having are shapes: a fogger that fades over five seconds, a mister that
 * comes up from nothing over three, a lamp that stutters. Those are recipes,
 * and a person should pick one rather than build it.
 *
 * A preset is a normalised envelope — pairs of (fraction of the span, value
 * 0..1) — and nothing else. Declarative on purpose: it makes the library cheap
 * to extend, makes every entry testable without a renderer, and means the
 * shapes can be drawn as thumbnails from the same data that builds the points.
 *
 * What it inserts is ordinary points and ordinary cues. There is no preset
 * object living on in the score, nothing to re-open or re-apply. You drop a
 * shape in and then it is yours, which is the whole point of a starting shape.
 */

import type { Cue, Params, Point } from './score';
import type { Seconds } from './time';

/** A point on an envelope: how far through, and how hard. */
export type Node = readonly [number, number];

export interface Preset {
  id: string;
  name: string;
  /**
   * The device kinds this suits. Empty means any.
   *
   * A shape is not universal: a fogger fading over five seconds is a good
   * default and a lamp fading over five seconds is a slow dissolve nobody
   * asked for, so the list is what stops the picker offering everything to
   * everything.
   */
  kinds: readonly string[];
  /** How long the shape lasts by default, in seconds. */
  seconds: number;
  /** What it is for, in the picker. */
  hint: string;
  /**
   * The shape, as fractions of the span against levels.
   *
   * Always starts at 0 and ends at 1 so the span is unambiguous, and a preset
   * that ends above zero is saying it leaves the device lit — which is a
   * legitimate thing to want and the reason this is not enforced.
   */
  shape: readonly Node[];
  /**
   * An action name, for the devices driven by events rather than levels.
   *
   * With one, the preset inserts a single cue of that action lasting the whole
   * span, carrying its peak as the parameter — because that is what a fogger
   * is actually told: burst this hard for this long. Without one it inserts
   * the envelope as curve points.
   */
  action?: string;
}

/* --- the shapes --------------------------------------------------------- */

const RAMP_UP: Node[] = [[0, 0], [1, 1]];
const RAMP_DOWN: Node[] = [[0, 1], [1, 0]];
const HOLD: Node[] = [[0, 1], [1, 1]];
/* Fast in, slow out: the shape of almost everything physical that is struck. */
const HIT: Node[] = [[0, 0], [0.04, 1], [1, 0]];
const SWELL: Node[] = [[0, 0], [0.5, 1], [1, 0]];
/* A long tail rather than a symmetric one — smoke and dust hang about. */
const PUFF: Node[] = [[0, 0], [0.08, 1], [0.35, 0.55], [1, 0]];
const BREATHE: Node[] = [[0, 0.15], [0.25, 0.8], [0.5, 0.15], [0.75, 0.8], [1, 0.15]];

/**
 * A square wave of n pulses, for stutters and strobes.
 *
 * Built rather than written out, because the difference between four pulses
 * and twelve is a number and should not be forty lines of literal.
 */
function stutter(pulses: number, duty = 0.5): Node[] {
  const out: Node[] = [];
  const step = 1 / pulses;
  for (let i = 0; i < pulses; i++) {
    const from = i * step;
    const upTo = from + step * duty;
    /* Two points at each edge, a hair apart, so linear interpolation cannot
     * round the corners off into a triangle wave. */
    out.push([from, 0], [from + 1e-4, 1], [upTo - 1e-4, 1], [upTo, 0]);
  }
  out.push([1, 0]);
  return out;
}

/** A decay that keeps dropping fast at first, unlike a straight line. */
function decay(steps = 6): Node[] {
  const out: Node[] = [];
  for (let i = 0; i <= steps; i++) {
    const f = i / steps;
    out.push([f, Math.pow(1 - f, 2.2)]);
  }
  return out;
}

/**
 * The stretches of a shape that are above zero: [from, to, peak] in fractions.
 *
 * A shape is either one gesture or several, and which it is decides what it
 * becomes on a device driven by events rather than levels. A strobe is not a
 * two-second flash held at full — it is twelve flashes — and collapsing it into
 * one is not a simplification of the preset, it is a different preset.
 */
export function pulses(shape: readonly Node[]): [number, number, number][] {
  const out: [number, number, number][] = [];
  let from: number | null = null;
  let peak = 0;
  let last = 0;
  for (const [f, v] of shape) {
    const level = Math.abs(v);
    if (level > 0) {
      /* Open at the zero edge before the rise, so a square pulse keeps its
       * width instead of starting a hair late. */
      if (from === null) from = last;
      peak = Math.max(peak, level);
    } else if (from !== null) {
      out.push([from, f, peak]);
      from = null;
      peak = 0;
    }
    last = f;
  }
  if (from !== null) out.push([from, last, peak]);
  return out;
}

/**
 * Channels a preset's number is a level for, rather than a colour or an axis.
 *
 * A preset says how hard, once. `i` is a level and `h` is a hue: writing the
 * same number into both turns a white flash into a saturated red one, and a
 * fade up into a sweep through the spectrum. Where a track names none of these
 * — red, green, blue — every channel takes the number, which is white at full
 * and the honest reading of "brighter".
 */
const LEVEL_KEYS = ['i', 'intensity', 'output'];

export function levelKey(channels: readonly string[]): string | null {
  return channels.find((c) => LEVEL_KEYS.includes(c)) ?? null;
}

/* What a colour channel is worth when the preset has nothing to say about it
 * and the track has nothing to keep: no saturation, so no hue to argue over. */
const NEUTRAL: Record<string, number> = { h: 0, s: 0 };

/** The shortest a pulse can become a cue as. Below this it is not an event. */
const MIN_CUE_SECONDS = 0.02;

/* --- the library -------------------------------------------------------- */

export const PRESETS: readonly Preset[] = [
  /* Fog and mist: dosed devices, so these insert cues. */
  {
    id: 'fog-burst', name: 'Fog burst', kinds: ['fog'], seconds: 4, action: 'burst',
    hint: 'A full burst, held for four seconds.', shape: HOLD,
  },
  {
    id: 'fog-fade', name: 'Fog, fading', kinds: ['fog'], seconds: 5, action: 'burst',
    hint: 'Comes in at full and falls away over five seconds.', shape: decay(),
  },
  {
    id: 'fog-swell', name: 'Fog swell', kinds: ['fog'], seconds: 8, action: 'burst',
    hint: 'Builds and clears — for a room filling rather than a blast.', shape: SWELL,
  },
  {
    id: 'fog-puff', name: 'Dust puff', kinds: ['fog'], seconds: 3, action: 'burst',
    hint: 'A sharp puff with a long hang, the way dust behaves.', shape: PUFF,
  },
  {
    id: 'mist-spray', name: 'Mist, three seconds', kinds: ['mist'], seconds: 3, action: 'spray',
    hint: 'Rising from nothing to full over three seconds.', shape: RAMP_UP,
  },
  {
    id: 'mist-splash', name: 'Splash', kinds: ['mist'], seconds: 1.5, action: 'spray',
    hint: 'A short hard hit of water.', shape: HIT,
  },
  {
    id: 'mist-drizzle', name: 'Drizzle', kinds: ['mist'], seconds: 10, action: 'spray',
    hint: 'A long, light fall.', shape: [[0, 0], [0.1, 0.35], [0.9, 0.35], [1, 0]],
  },
  {
    id: 'scent-puff', name: 'Scent puff', kinds: ['scent'], seconds: 1, action: 'puff',
    hint: 'One dose. A smell cannot be taken back, so this stays short.',
    shape: HOLD,
  },

  /* Wind: a fan is dimmed, so these are curves, except the one that is an
     event. A gust is a thing that happens; a breeze building over twelve
     seconds is a level, and no cue can say that. */
  {
    id: 'wind-gust', name: 'Gust', kinds: ['wind'], seconds: 4, action: 'gust',
    hint: 'Up fast, down slowly.', shape: HIT,
  },
  {
    id: 'wind-build', name: 'Building wind', kinds: ['wind'], seconds: 12,
    hint: 'A slow rise to full, for weather closing in.', shape: RAMP_UP,
  },
  {
    id: 'wind-drop', name: 'Wind dropping', kinds: ['wind'], seconds: 8,
    hint: 'Full to nothing, for a storm passing.', shape: RAMP_DOWN,
  },
  {
    id: 'wind-buffet', name: 'Buffeting', kinds: ['wind'], seconds: 10,
    hint: 'Rising and falling, never settling.', shape: BREATHE,
  },
  {
    id: 'wind-steady', name: 'Steady breeze', kinds: ['wind'], seconds: 15,
    hint: 'Holds at full for as long as it lasts.', shape: HOLD,
  },

  /* Shake. */
  {
    id: 'shake-hit', name: 'Impact', kinds: ['shake'], seconds: 1.2, action: 'hit',
    hint: 'One jolt with a short ring-out.', shape: HIT,
  },
  {
    id: 'shake-rumble', name: 'Rumble', kinds: ['shake'], seconds: 6,
    hint: 'A sustained low shake that fades.', shape: [[0, 0], [0.12, 0.7], [0.7, 0.6], [1, 0]],
  },
  {
    id: 'shake-quake', name: 'Earthquake', kinds: ['shake'], seconds: 8,
    hint: 'Builds, holds hard, then subsides.', shape: SWELL,
  },
  {
    id: 'shake-stutter', name: 'Stutter', kinds: ['shake'], seconds: 2,
    hint: 'Six sharp knocks — footfalls, gunfire, machinery.', shape: stutter(6),
  },

  /* Light. Shapes only: the colour is whatever the track already carries, so
   * these are about how it moves rather than what colour it is.
   *
   * Two of them are events and the rest are levels, which is the difference
   * between what a flash bulb can be told and what a dimmer can. A flash is
   * one instant and a strobe is twelve of them; a fade, a breath and a
   * flicker are shapes, and a cue cannot hold a shape. */
  {
    id: 'light-flash', name: 'Flash', kinds: ['light'], seconds: 0.3, action: 'flash',
    hint: 'One bright instant.', shape: HIT,
  },
  {
    id: 'light-strobe', name: 'Strobe', kinds: ['light'], seconds: 2,
    hint: 'Twelve hard pulses.', shape: stutter(12),
  },
  {
    id: 'light-lightning', name: 'Lightning', kinds: ['light'], seconds: 1.4,
    hint: 'A strike, a gap, and a weaker second flash.',
    shape: [[0, 0], [0.02, 1], [0.09, 0.1], [0.14, 0.85], [0.22, 0.05], [0.45, 0], [1, 0]],
  },
  {
    id: 'light-fade-in', name: 'Fade up', kinds: ['light'], seconds: 3,
    hint: 'Dark to full.', shape: RAMP_UP,
  },
  {
    id: 'light-fade-out', name: 'Fade down', kinds: ['light'], seconds: 3,
    hint: 'Full to dark.', shape: RAMP_DOWN,
  },
  {
    id: 'light-pulse', name: 'Slow pulse', kinds: ['light'], seconds: 8,
    hint: 'Breathing in and out, twice.', shape: BREATHE,
  },
  {
    id: 'light-firelight', name: 'Firelight', kinds: ['light'], seconds: 6,
    hint: 'An uneven flicker that never settles.',
    shape: [[0, 0.55], [0.08, 0.85], [0.16, 0.5], [0.27, 0.95], [0.36, 0.6],
            [0.48, 0.8], [0.57, 0.45], [0.69, 0.9], [0.78, 0.55], [0.9, 0.75], [1, 0.6]],
  },

  /* Motion: the envelope drives every axis the track carries, which is a
   * starting shape rather than a finished move. Shaping one axis against
   * another is what the editor is for. */
  {
    id: 'motion-drop', name: 'Drop', kinds: ['motion'], seconds: 1.5,
    hint: 'Falls and recovers.', shape: [[0, 0], [0.15, -1], [0.45, 0.25], [1, 0]],
  },
  {
    id: 'motion-lurch', name: 'Lurch', kinds: ['motion'], seconds: 1,
    hint: 'One hard throw back to rest.', shape: HIT,
  },
  {
    id: 'motion-sway', name: 'Sway', kinds: ['motion'], seconds: 12,
    hint: 'A long roll from side to side, for water or flight.',
    shape: [[0, 0], [0.25, 1], [0.5, 0], [0.75, -1], [1, 0]],
  },
  {
    id: 'motion-climb', name: 'Climb', kinds: ['motion'], seconds: 6,
    hint: 'Tilts back and holds, then levels.',
    shape: [[0, 0], [0.2, 0.8], [0.8, 0.8], [1, 0]],
  },
  {
    id: 'motion-settle', name: 'Settle', kinds: ['motion'], seconds: 4,
    hint: 'Comes to rest from wherever it is.', shape: RAMP_DOWN,
  },
];

/**
 * The presets that suit a kind and the track in front of you.
 *
 * A cue track can only be sent verbs, and there is no truthful verb for a
 * platform: you do not tell a motion rig to "move", you tell it where to be.
 * Offering a shape that cannot be built is an insert that silently does
 * nothing, so the picker declines it here rather than the insert declining it
 * later and telling nobody.
 */
export function presetsFor(kind: string, holds: 'cue' | 'curve' = 'curve'): Preset[] {
  return PRESETS.filter((p) => {
    if (p.kinds.length && !p.kinds.includes(kind)) return false;
    if (holds !== 'cue') return true;
    /* Nothing to send it. */
    if (!(p.action ?? actionForKind(kind))) return false;
    /* A cue carries three facts: when, for how long, and how hard. Anything
     * else in the shape is lost, so a shape may only become cues when that is
     * all it ever was.
     *
     * A preset with an action is an event by declaration — the instrument owns
     * the envelope once the burst has started, which is what a fogger and a
     * shaker and a flash bulb actually do with one. A preset without an action
     * is a level shape, and the only level shape a cue track can carry is a
     * rhythm: several pulses arriving as several cues. Flatten a single one
     * and what is left is "on at full for three seconds". That is not a fade
     * up. It is a stripe, which is exactly how it was reported. */
    return !!p.action || pulses(p.shape).length > 1;
  });
}

export function presetById(id: string): Preset | null {
  return PRESETS.find((p) => p.id === id) ?? null;
}

/**
 * What the envelope is worth a fraction of the way through.
 *
 * Linear between nodes and held at the ends, which is the same rule the score
 * itself uses for curves — so what the preview shows and what the inserted
 * points do are the same shape, not two shapes that ought to agree.
 */
export function valueOf(shape: readonly Node[], f: number): number {
  if (shape.length === 0) return 0;
  if (f <= shape[0][0]) return shape[0][1];
  const last = shape[shape.length - 1];
  if (f >= last[0]) return last[1];
  for (let i = 1; i < shape.length; i++) {
    const [x1, y1] = shape[i];
    if (x1 >= f) {
      const [x0, y0] = shape[i - 1];
      const span = x1 - x0;
      return span <= 0 ? y1 : y0 + (y1 - y0) * ((f - x0) / span);
    }
  }
  return last[1];
}

const round3 = (n: number) => Math.round(n * 1000) / 1000;

export interface Insertion {
  points?: Point[];
  cues?: Cue[];
  /** The span it occupies, for replacing whatever was already there. */
  from: Seconds;
  to: Seconds;
}

/**
 * Build what a preset inserts at a moment.
 *
 * `scale` is how hard, so the same shape can be dropped in gently. `channels`
 * are the ones the track carries, and every one of them gets the envelope —
 * a motion preset moves each axis it finds, which is a starting shape rather
 * than a finished move.
 */
/**
 * The action a kind answers to, for a preset that names none of its own.
 *
 * Only the actions the vocabulary already uses. Inventing a name here would
 * produce a cue addressed to an instrument that has never heard of it, which
 * fails silently at play time rather than loudly here.
 */
const ACTION_BY_KIND: Record<string, string> = {
  light: 'flash',
  shake: 'hit',
  wind: 'gust',
  fog: 'burst',
  mist: 'spray',
  scent: 'puff',
};

export function actionForKind(kind: string): string | null {
  return ACTION_BY_KIND[kind] ?? null;
}

export function build(
  preset: Preset,
  at: Seconds,
  channels: readonly string[] = [],
  opts: {
    seconds?: number; scale?: number; base?: Params;
    /**
     * What the target track holds, which is not the same question as what the
     * preset is naturally.
     *
     * A fog burst dropped on a fog CURVE track has to arrive as points: the
     * format refuses cues on a curve track, so building by the preset's own
     * nature wrote a cue nothing drew and a score that would not save. The
     * track decides.
     */
    as?: 'cue' | 'curve';
    /** The action to use when the preset names none and a cue is wanted. */
    action?: string;
  } = {},
): Insertion | null {
  const seconds = opts.seconds && opts.seconds > 0 ? opts.seconds : preset.seconds;
  const scale = opts.scale ?? 1;
  const from = round3(at);
  const to = round3(at + seconds);

  const as = opts.as ?? (preset.action ? 'cue' : 'curve');

  if (as === 'cue') {
    const action = preset.action ?? opts.action;
    /* No action and none offered: there is nothing truthful to send. Better to
     * refuse than to invent a verb the instrument has never heard. */
    if (!action) return null;
    /* One cue per pulse. An event device is told how hard and for how long, so
     * a single gesture is still a single cue carrying its peak — the shape
     * inside it is the instrument's business once the burst has started. But a
     * shape whose whole identity is repetition has to arrive as repetition:
     * twelve pulses arrived as one flash, and the preset is called Strobe. */
    const level = levelKey(channels);
    const made: Cue[] = [];
    for (const [a, b, peak] of pulses(preset.shape)) {
      const params: Params = {};
      for (const c of channels) {
        params[c] = level && c !== level
          ? round3(opts.base?.[c] ?? NEUTRAL[c] ?? 0)
          : round3(Math.min(1, peak * scale));
      }
      made.push({
        t: round3(at + a * seconds),
        action,
        params,
        duration: round3(Math.max(MIN_CUE_SECONDS, (b - a) * seconds)),
      });
    }
    if (!made.length) return null;
    return { from, to, cues: made };
  }

  const level = levelKey(channels);
  const points: Point[] = preset.shape.map(([f, v]) => {
    const value: Params = {};
    for (const c of channels) {
      const base = opts.base?.[c];
      /* Colour is not a level: a fade up asks a lamp to get brighter, not
       * redder. Where the track names a level, everything else holds what the
       * curve already had under the playhead. */
      if (level && c !== level) {
        value[c] = round3(base ?? NEUTRAL[c] ?? 0);
        continue;
      }
      /* A shape of zero means "leave it where the track already was" only for
       * a preset that starts and ends at rest; anything else would make a fade
       * to black impossible to author. So the base is a floor, not a mixer. */
      const scaled = v * scale;
      value[c] = round3(base !== undefined && v === 0 ? base : scaled);
    }
    return { t: round3(at + f * seconds), value };
  });

  return { from, to, points };
}
