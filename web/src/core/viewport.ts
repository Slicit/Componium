/* A viewport: everything about how the stage is arranged, as one value.
 *
 * The split already remembered itself, which was enough while there was only
 * one thing to remember. There is more than one now — whether the room is up,
 * whether the sliders are up, where the camera is standing — and they are not
 * independent: reviewing motion wants a big room with the sliders to hand,
 * reviewing light wants a big picture and no sliders at all, and switching
 * between those is four separate adjustments every time.
 *
 * So they are one value that can be named and recalled, rather than four
 * controls that happen to sit near each other.
 */

import { columnsAt, clampHeight, DEFAULT_COLUMNS, DEFAULT_HEIGHT } from '../ui/useSplit';

/** Where the room's camera is standing, and what it is looking at. */
export interface CameraView {
  pos: [number, number, number];
  target: [number, number, number];
}

export interface Viewport {
  /** How many of the twelve columns the picture takes. */
  columns: number;
  /** The stage's height in pixels. */
  height: number;
  /** Is the room pane up. */
  room: boolean;
  /** Are the force sliders up. */
  force: boolean;
  /**
   * Where the camera was, or null to leave it wherever it is.
   *
   * Null is not the same as the default position. A viewport saved before the
   * room was ever opened has nothing to say about the camera, and moving it to
   * some canonical spot would be inventing an instruction the author never
   * gave.
   */
  camera: CameraView | null;
}

export const DEFAULT_VIEWPORT: Viewport = {
  columns: DEFAULT_COLUMNS,
  height: DEFAULT_HEIGHT,
  room: true,
  force: true,
  camera: null,
};

export interface NamedViewport {
  name: string;
  viewport: Viewport;
}

/** The most viewports worth keeping. Past this the list is a filing problem. */
export const MAX_VIEWPORTS = 12;

const isNum = (v: unknown): v is number => typeof v === 'number' && isFinite(v);

function triple(v: unknown): [number, number, number] | null {
  if (!Array.isArray(v) || v.length !== 3) return null;
  if (!v.every(isNum)) return null;
  return [v[0], v[1], v[2]];
}

/**
 * A camera view from whatever was stored, or null.
 *
 * Half a camera is worse than none: a position with no target points the view
 * at the origin, which is the floor, and the room looks broken rather than
 * unremembered.
 */
export function normaliseCamera(raw: unknown): CameraView | null {
  if (!raw || typeof raw !== 'object') return null;
  const o = raw as Record<string, unknown>;
  const pos = triple(o.pos);
  const target = triple(o.target);
  if (!pos || !target) return null;
  return { pos, target };
}

/**
 * A usable viewport from whatever was stored.
 *
 * Everything is bounds checked and every missing field falls back to the
 * default, because this reads localStorage and a file written by an older
 * version of the studio is the normal case, not the exceptional one.
 */
export function normalise(raw: unknown): Viewport {
  if (!raw || typeof raw !== 'object') return { ...DEFAULT_VIEWPORT };
  const o = raw as Record<string, unknown>;
  return {
    columns: isNum(o.columns) ? columnsAt(o.columns / 12) : DEFAULT_VIEWPORT.columns,
    height: isNum(o.height) ? clampHeight(o.height) : DEFAULT_VIEWPORT.height,
    /* Explicitly false, or true. An absent flag means a viewport saved before
     * the flag existed, and the default is the answer there. */
    room: typeof o.room === 'boolean' ? o.room : DEFAULT_VIEWPORT.room,
    force: typeof o.force === 'boolean' ? o.force : DEFAULT_VIEWPORT.force,
    camera: normaliseCamera(o.camera),
  };
}

/** The saved list from whatever was stored, dropping anything unusable. */
export function normaliseList(raw: unknown): NamedViewport[] {
  if (!Array.isArray(raw)) return [];
  const out: NamedViewport[] = [];
  const seen = new Set<string>();
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue;
    const name = cleanName((item as Record<string, unknown>).name);
    /* A duplicate name would make two rows that look identical and delete
     * each other's entry. First one wins, as it does on save. */
    if (!name || seen.has(name)) continue;
    seen.add(name);
    out.push({ name, viewport: normalise((item as Record<string, unknown>).viewport) });
    if (out.length >= MAX_VIEWPORTS) break;
  }
  return out;
}

/** A name fit to store, or '' when there is nothing usable in it. */
export function cleanName(raw: unknown): string {
  if (typeof raw !== 'string') return '';
  /* Collapse the whitespace rather than only trimming the ends, so "wide
   * room" and "wide  room" cannot both exist and be told apart only by
   * counting spaces. */
  return raw.trim().replace(/\s+/g, ' ').slice(0, 40);
}

/** Same arrangement, ignoring the camera, which moves on its own. */
export function sameLayout(a: Viewport, b: Viewport): boolean {
  return a.columns === b.columns && a.height === b.height
    && a.room === b.room && a.force === b.force;
}

/**
 * Store a viewport under a name, replacing any viewport of that name.
 *
 * Returns a new list; saving over a name keeps its position in the list, so a
 * viewport a person has learned the position of does not jump to the end when
 * they adjust it.
 */
export function put(list: readonly NamedViewport[], name: string, viewport: Viewport): NamedViewport[] {
  const clean = cleanName(name);
  if (!clean) return [...list];
  const next = list.map((v) => (v.name === clean ? { name: clean, viewport } : v));
  if (!next.some((v) => v.name === clean)) {
    if (next.length >= MAX_VIEWPORTS) return next;
    next.push({ name: clean, viewport });
  }
  return next;
}

export function drop(list: readonly NamedViewport[], name: string): NamedViewport[] {
  return list.filter((v) => v.name !== name);
}
