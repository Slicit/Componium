/* What is under the pointer.
 *
 * Kept out of the component and out of the renderer, because it has to agree
 * with the renderer exactly — a hit box that is four pixels from what was
 * drawn is a control that misses, and that is not a thing you can see in a
 * screenshot. Both read the same layout and the same view, and the numbers
 * here mirror the ones in lanes.ts on purpose.
 */

import { TimeView } from './view';
import { rowAt, type Layout, type Row } from './layout';
import { cueEnd, isSpan, type Cue, type Point, type Score } from './score';

/** How close a pointer has to be, in pixels. Generous: fingers and trackpads. */
export const GRAB = 7;

export type Hit =
  | { k: 'ruler'; t: number }
  | { k: 'cue'; row: Row; cue: Cue; part: 'body' | 'start' | 'end' }
  | { k: 'point'; row: Row; point: Point; channel: string }
  | { k: 'lane'; row: Row; t: number }
  | { k: 'empty' };

export interface HitContext {
  score: Score;
  layout: Layout;
  view: TimeView;
  width: number;
  rulerH: number;
}

export function hitTest(ctx: HitContext, x: number, y: number): Hit {
  const { view, width, rulerH } = ctx;
  if (y < rulerH) return { k: 'ruler', t: view.fromX(x, width) };

  const row = rowAt(ctx.layout, y - rulerH);
  if (!row) return { k: 'empty' };

  const track = ctx.score.tracks[row.track];
  const localY = y - rulerH - row.y;

  if (row.draw === 'cues') {
    const hit = hitCue(track.cues ?? [], view, width, x);
    if (hit) return { k: 'cue', row, ...hit };
  } else if (row.draw === 'curve' && row.channel) {
    const hit = hitPoint(track.points ?? [], row.channel, view, width, row.h, x, localY);
    if (hit) return { k: 'point', row, point: hit, channel: row.channel };
  }

  return { k: 'lane', row, t: view.fromX(x, width) };
}

function hitCue(cues: Cue[], view: TimeView, width: number, x: number):
  { cue: Cue; part: 'body' | 'start' | 'end' } | null {
  /* Backwards, so the topmost of two overlapping events wins — the same one
   * the renderer drew last. */
  for (let i = cues.length - 1; i >= 0; i--) {
    const cue = cues[i];
    const end = cueEnd(cue);
    if (!view.intersects(cue.t, end)) continue;

    const x1 = view.toX(cue.t, width);
    const x2 = view.toX(end, width);

    if (isSpan(cue)) {
      /* Edges before body: a span narrower than two grab zones would
       * otherwise be impossible to trim, and trimming is the commoner
       * intent when the pointer is on an edge. */
      if (Math.abs(x - x2) <= GRAB) return { cue, part: 'end' };
      if (Math.abs(x - x1) <= GRAB) return { cue, part: 'start' };
      if (x >= x1 && x <= x2) return { cue, part: 'body' };
    } else if (Math.abs(x - x1) <= GRAB) {
      return { cue, part: 'body' };
    }
  }
  return null;
}

function hitPoint(
  points: Point[], channel: string, view: TimeView, width: number,
  rowH: number, x: number, localY: number,
): Point | null {
  const pad = 3;
  const bottom = rowH - pad;
  const top = pad;
  const yOf = (v: number) => bottom - Math.max(0, Math.min(1, v)) * (bottom - top);

  let best: Point | null = null;
  let bestD = GRAB * GRAB;
  for (const p of points) {
    const v = p.value?.[channel];
    if (typeof v !== 'number') continue;
    if (!view.intersects(p.t, p.t)) continue;
    const dx = view.toX(p.t, width) - x;
    if (Math.abs(dx) > GRAB) continue;
    const dy = yOf(v) - localY;
    const d = dx * dx + dy * dy;
    if (d <= bestD) { bestD = d; best = p; }
  }
  return best;
}

/** Everything inside a rubber band, for box selection. */
export function hitRange(
  ctx: HitContext, x1: number, y1: number, x2: number, y2: number,
): { cues: Array<{ row: Row; cue: Cue }>; points: Array<{ row: Row; point: Point; channel: string }> } {
  const lo = Math.min(x1, x2);
  const hi = Math.max(x1, x2);
  const top = Math.min(y1, y2) - ctx.rulerH;
  const bottom = Math.max(y1, y2) - ctx.rulerH;
  const tA = ctx.view.fromX(lo, ctx.width);
  const tB = ctx.view.fromX(hi, ctx.width);

  const cues: Array<{ row: Row; cue: Cue }> = [];
  const points: Array<{ row: Row; point: Point; channel: string }> = [];

  for (const row of ctx.layout.rows) {
    if (row.y + row.h < top || row.y > bottom) continue;
    const track = ctx.score.tracks[row.track];

    if (row.draw === 'cues') {
      for (const cue of track.cues ?? []) {
        /* Any overlap counts, not containment: a four second span half inside
         * the band is something you meant to catch. */
        if (cueEnd(cue) >= tA && cue.t <= tB) cues.push({ row, cue });
      }
    } else if (row.draw === 'curve' && row.channel) {
      for (const point of track.points ?? []) {
        if (point.t >= tA && point.t <= tB && row.channel in (point.value ?? {})) {
          points.push({ row, point, channel: row.channel });
        }
      }
    }
  }
  return { cues, points };
}

/** The pointer shape for a hit, so the cursor says what will happen. */
export function cursorFor(hit: Hit): string {
  switch (hit.k) {
    case 'ruler': return 'ew-resize';
    case 'cue': return hit.part === 'body' ? 'grab' : 'col-resize';
    case 'point': return 'ns-resize';
    default: return 'crosshair';
  }
}
