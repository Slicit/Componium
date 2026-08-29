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
  /** How colour is written down. Absent means rgb, as every older score is. */
  space?: 'rgb' | 'hsi' | string;
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
const LEVEL_KEYS = ['i', 'intensity', 'level', 'amount', 'speed', 'strength'];

/** Colour channels, which are amplitude when taken together. */
const COLOUR_KEYS = ['r', 'g', 'b'];

/** Hue, saturation and intensity, in the order lanes should appear. */
const HSI_KEYS = ['h', 's', 'i'];

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

  /* Authored as hue: convert, so the ribbon and every event tint show the
   * colour a fixture will actually be sent rather than nothing at all. */
  if (typeof params.h === 'number' || typeof params.s === 'number') {
    const [r, g, b] = hsiToRGB(params.h ?? 0, params.s ?? 0, params.i ?? 0);
    const c = (v: number) => Math.round(clamp01(v) * 255);
    return `rgb(${c(r)}, ${c(g)}, ${c(b)})`;
  }

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
    /* Hue, then saturation, then intensity — the order they are thought about
     * — or red, green, blue for a track written the older way. */
    const order = HSI_KEYS.some((k) => seen.has(k)) ? HSI_KEYS : COLOUR_KEYS;
    const first = order.filter((c) => seen.has(c));
    const rest = [...seen].filter((c) => !order.includes(c)).sort();
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
export function valueAt(points: Point[], t: Seconds, channels: string[], hsi = false): Params {
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

  /* Hue is not a number that can be averaged: it wraps, and it does not exist
   * without saturation. Doing it channel by channel above sweeps a fade from
   * red to red the long way round through cyan. */
  if (hsi) Object.assign(out, lerpHSI(a.value, b.value, f));
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

/* --- colour spaces ------------------------------------------------------ */

/** True when a track's colour is written as hue, saturation and intensity. */
export function isHSI(track: Track): boolean {
  if (track.space === 'hsi') return true;
  /* A track can carry hsi values without declaring the space — a paste, or a
   * hand edit — and the lanes should still be named properly. */
  for (const p of track.points ?? []) {
    if ('h' in (p.value ?? {}) && 's' in (p.value ?? {})) return true;
  }
  return false;
}

/**
 * Hue, saturation and intensity to red, green and blue.
 *
 * The same geometry as internal/colour in Go, and it has to stay that way:
 * this is what the editor previews and that is what the fixture is sent, so a
 * disagreement between them is a preview that lies.
 */
export function hsiToRGB(h: number, s: number, i: number): [number, number, number] {
  let hue = h % 1;
  if (hue < 0) hue += 1;
  const sat = clamp01(s);
  const val = clamp01(i);
  if (sat === 0) return [val, val, val];

  const sector = hue * 6;
  const k = Math.floor(sector) % 6;
  const f = sector - Math.floor(sector);
  const p = val * (1 - sat);
  const q = val * (1 - sat * f);
  const t = val * (1 - sat * (1 - f));
  switch (k) {
    case 0: return [val, t, p];
    case 1: return [q, val, p];
    case 2: return [p, val, t];
    case 3: return [p, q, val];
    case 4: return [t, p, val];
    default: return [val, p, q];
  }
}

/**
 * Interpolate a colour, taking hue the short way round and carrying a hue
 * across a point that has none.
 *
 * Mirrors colour.Lerp in Go. See that file for why hue cannot simply be
 * averaged: the seam is red, and white has no hue to average with.
 */
export function lerpHSI(
  a: Params, b: Params, f: number,
): { h: number; s: number; i: number } {
  const neutral = 1e-4;
  const wrap = (h: number) => { const x = h % 1; return x < 0 ? x + 1 : x; };
  let ah = wrap(a.h ?? 0);
  let bh = wrap(b.h ?? 0);
  const as = a.s ?? 0;
  const bs = b.s ?? 0;
  if (as <= neutral && bs <= neutral) { ah = 0; bh = 0; }
  else if (as <= neutral) ah = bh;
  else if (bs <= neutral) bh = ah;

  let d = bh - ah;
  if (d > 0.5) d -= 1; else if (d < -0.5) d += 1;

  return {
    h: wrap(ah + d * f),
    s: as + (bs - as) * f,
    i: (a.i ?? 0) + ((b.i ?? 0) - (a.i ?? 0)) * f,
  };
}
