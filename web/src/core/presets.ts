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

  /* Wind: a fan is dimmed, so these are curves. */
  {
    id: 'wind-gust', name: 'Gust', kinds: ['wind'], seconds: 4,
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
    id: 'shake-hit', name: 'Impact', kinds: ['shake'], seconds: 1.2,
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
   * these are about how it moves rather than what colour it is. */
  {
    id: 'light-flash', name: 'Flash', kinds: ['light'], seconds: 0.3,
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

/** The presets that suit a kind, in the order the library lists them. */
export function presetsFor(kind: string): Preset[] {
  return PRESETS.filter((p) => p.kinds.length === 0 || p.kinds.includes(kind));
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
export function build(
  preset: Preset,
  at: Seconds,
  channels: readonly string[] = [],
  opts: { seconds?: number; scale?: number; base?: Params } = {},
): Insertion {
  const seconds = opts.seconds && opts.seconds > 0 ? opts.seconds : preset.seconds;
  const scale = opts.scale ?? 1;
  const from = round3(at);
  const to = round3(at + seconds);

  if (preset.action) {
    /* An event device is told one thing: how hard, for how long. The envelope
     * still decides how hard — its peak is the dose — but the shape itself is
     * the instrument's business once the burst has started. */
    let peak = 0;
    for (const [, v] of preset.shape) peak = Math.max(peak, Math.abs(v));
    const params: Params = {};
    for (const c of channels) params[c] = round3(Math.min(1, peak * scale));
    return {
      from, to,
      cues: [{ t: from, action: preset.action, params, duration: round3(seconds) }],
    };
  }

  const points: Point[] = preset.shape.map(([f, v]) => {
    const value: Params = {};
    for (const c of channels) {
      const base = opts.base?.[c];
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
