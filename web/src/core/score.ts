/* The score, as the studio's API hands it over, and the things you have to
 * derive from it before you can draw it.
 *
 * These types mirror `wireScore` in internal/studio/studio.go exactly. They
 * are deliberately a separate declaration rather than something generated: the
 * wire format is small, stable and hand-written on the Go side, and a
 * generator would be more machinery than the thing it generates.
 */

import { clamp01, round3, type Seconds } from './time';

export interface Score {
  title: string;
  duration: Seconds;
  fps?: number;
  path?: string;
  tracks: Track[];
}

export interface Track {
  instrument: string;
  type: 'cue' | 'curve' | string;
  cues?: Cue[];
  points?: Point[];
}

export interface Cue {
  t: Seconds;
  action: string;
  params?: Params;
  /** Zero means momentary. Anything else is a span with a start and a stop. */
  duration?: Seconds;
  /** What nominated this, when the composer guessed rather than measured. */
  source?: string;
}

export interface Point {
  t: Seconds;
  value: Params;
}

export type Params = Record<string, number>;

export interface Instrument {
  id: string;
  kind: string;
  driver?: string;
  /** Dead time, in seconds. The conductor dispatches this much early. */
  latency?: number;
  position?: [number, number, number];
}

export interface Rig {
  name?: string;
  instruments?: Instrument[];
}

/* --- amplitude ---------------------------------------------------------
 *
 * The defect this exists to fix: a cue was drawn as a fixed block, so a 0.2
 * gust and a 1.0 gust were the same rectangle. A cue carries *two* dimensions
 * a person needs — how long, and how hard — and the old timeline showed
 * neither for anything but curves.
 *
 * There is no single field for "how hard", because what that means depends on
 * the instrument: a fan has an intensity, a light has three colour channels, a
 * platform has six axes of displacement. So it is derived, in a fixed order,
 * and the order is the interesting part.
 */

/** The parameter names that are amplitude by another name. */
const LEVEL_KEYS = ['intensity', 'level', 'amount', 'speed', 'strength'];

/** Colour channels, which are amplitude when taken together. */
const COLOUR_KEYS = ['r', 'g', 'b'];

/** Axes of motion, where what matters is how far from rest, in any direction. */
const AXIS_KEYS = ['surge', 'sway', 'heave', 'roll', 'pitch', 'yaw'];

/**
 * How hard an event is, 0 to 1.
 *
 * Returns null when the event genuinely has no amplitude — a bare `stop`, say
 * — so a caller can draw it as a marker rather than as a bar of height zero,
 * which would read as "this does nothing".
 */
export function amplitudeOf(params: Params | undefined): number | null {
  if (!params) return null;
  for (const k of LEVEL_KEYS) {
    if (typeof params[k] === 'number') return clamp01(params[k]);
  }

  /* Colour: the brightest channel, not the average. Averaging makes a
   * saturated red — (1, 0, 0) — read as a third as bright as white, when to a
   * person in the room it is a light at full. */
  let colour = -1;
  for (const k of COLOUR_KEYS) {
    if (typeof params[k] === 'number') colour = Math.max(colour, clamp01(params[k]));
  }
  if (colour >= 0) return colour;

  /* Motion: distance from rest on the strongest axis, and sign is discarded.
   * A hard drop and a hard climb are the same amount of movement to sit
   * through, which is what the height of a block is being asked to convey. */
  let axis = -1;
  for (const k of AXIS_KEYS) {
    if (typeof params[k] === 'number') axis = Math.max(axis, clamp01(Math.abs(params[k])));
  }
  if (axis >= 0) return axis;

  const numbers = Object.values(params).filter((v) => typeof v === 'number');
  if (!numbers.length) return null;
  return clamp01(Math.max(...numbers.map(Math.abs)));
}

/** A colour to tint an event with, when it has one. */
export function colourOf(params: Params | undefined): string | null {
  if (!params) return null;
  const has = COLOUR_KEYS.some((k) => typeof params[k] === 'number');
  if (!has) return null;
  const c = (k: string) => Math.round(clamp01(params[k] ?? 0) * 255);
  return `rgb(${c('r')}, ${c('g')}, ${c('b')})`;
}

/** Where an event ends. Momentary cues end where they start. */
export function cueEnd(cue: Cue): Seconds {
  return cue.t + Math.max(0, cue.duration ?? 0);
}

export function isSpan(cue: Cue): boolean {
  return (cue.duration ?? 0) > 0;
}

/** A nominated event is a guess the composer wants confirmed, not a finding. */
export function isNominated(cue: Cue): boolean {
  return typeof cue.source === 'string' && cue.source.length > 0;
}

/* --- curves ------------------------------------------------------------- */

/**
 * Which channels a curve carries, red first so the lanes never reorder
 * themselves between two films.
 *
 * A track with no points has nothing to read, which is a real state now that
 * emptying a curve is how you say an instrument does nothing — so the rig gets
 * asked instead, and failing that the instrument's own id, which is
 * conventionally `kind.name`.
 */
export function channelsOf(track: Track, rig?: Rig | null): string[] {
  const seen = new Set<string>();
  for (const p of track.points ?? []) {
    for (const k of Object.keys(p.value ?? {})) seen.add(k);
  }
  if (seen.size) {
    const first = COLOUR_KEYS.filter((c) => seen.has(c));
    const rest = [...seen].filter((c) => !COLOUR_KEYS.includes(c)).sort();
    return [...first, ...rest];
  }
  return kindOf(track.instrument, rig) === 'light' ? [...COLOUR_KEYS] : ['intensity'];
}

export function kindOf(instrument: string, rig?: Rig | null): string {
  for (const inst of rig?.instruments ?? []) {
    if (inst.id === instrument) return inst.kind;
  }
  return String(instrument ?? '').split('.')[0] ?? '';
}

export function latencyOf(instrument: string, rig?: Rig | null): number {
  for (const inst of rig?.instruments ?? []) {
    if (inst.id === instrument) return inst.latency ?? 0;
  }
  return 0;
}

/**
 * What a curve is worth at a moment, for every channel asked for.
 *
 * The same rule the player uses: hold before the first point and after the
 * last rather than extrapolating, and interpolate linearly between. The player
 * has its own copy in Go; the two must agree, because a point inserted here at
 * the value this returns must not visibly move when the player evaluates it.
 */
export function valueAt(points: Point[], t: Seconds, channels: string[]): Params {
  const out: Params = {};
  for (const c of channels) out[c] = 0;
  if (!points.length) return out;

  if (t <= points[0].t) return Object.assign(out, points[0].value);
  const last = points[points.length - 1];
  if (t >= last.t) return Object.assign(out, last.value);

  let hi = 0;
  for (let i = 0; i < points.length; i++) {
    if (points[i].t > t) { hi = i; break; }
  }
  const a = points[hi - 1];
  const b = points[hi];
  const span = b.t - a.t;
  const f = span > 0 ? (t - a.t) / span : 0;

  Object.assign(out, a.value);
  for (const k of Object.keys(b.value ?? {})) {
    const av = a.value?.[k];
    out[k] = av === undefined ? b.value[k] : round3(av + (b.value[k] - av) * f);
  }
  return out;
}

/** The span of time a track has anything to say about. */
export function trackExtent(track: Track): { start: Seconds; end: Seconds } | null {
  const cues = track.cues ?? [];
  const points = track.points ?? [];
  if (!cues.length && !points.length) return null;
  let start = Infinity;
  let end = -Infinity;
  for (const c of cues) {
    start = Math.min(start, c.t);
    end = Math.max(end, cueEnd(c));
  }
  for (const p of points) {
    start = Math.min(start, p.t);
    end = Math.max(end, p.t);
  }
  return { start, end };
}
