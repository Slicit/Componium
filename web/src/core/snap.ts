/* Snapping.
 *
 * A dragged event lands on something meaningful rather than wherever the
 * pointer happened to be: a frame boundary, the playhead, or another event.
 * Without it every edit is off by a few milliseconds and the score fills up
 * with times nobody chose.
 *
 * The threshold is in *pixels*, not seconds, which is the part that is easy to
 * get wrong. A fixed time threshold snaps from half a screen away when zoomed
 * in and never fires when zoomed out; a fixed pixel distance behaves the same
 * at every zoom, which is what makes it feel like a tool rather than a
 * surprise.
 */

import { TimeView } from './view';
import { snapToFrame, type Seconds } from './time';
import { cueEnd, type Score } from './score';

export const SNAP_PX = 8;

export interface SnapTargets {
  /** Times other events start or end at. */
  events: Seconds[];
  playhead: Seconds;
  duration: Seconds;
}

export interface SnapResult {
  t: Seconds;
  /** What it snapped to, for drawing a guide. Null when nothing was near. */
  to: Seconds | null;
  kind: 'event' | 'playhead' | 'edge' | 'frame' | null;
}

/**
 * Collect what a drag can snap to.
 *
 * `exclude` keeps an event from snapping to itself, which would pin it in
 * place and read as the timeline being frozen.
 */
export function snapTargets(
  score: Score, playhead: Seconds, exclude: Set<object> = new Set(),
): SnapTargets {
  const events: Seconds[] = [];
  for (const track of score.tracks ?? []) {
    for (const cue of track.cues ?? []) {
      if (exclude.has(cue)) continue;
      events.push(cue.t);
      if ((cue.duration ?? 0) > 0) events.push(cueEnd(cue));
    }
    for (const p of track.points ?? []) {
      if (exclude.has(p)) continue;
      events.push(p.t);
    }
  }
  events.sort((a, b) => a - b);
  return { events, playhead, duration: score.duration };
}

/**
 * Snap a time, or don't.
 *
 * Order matters: the playhead and the film's edges beat other events, because
 * they are the things a person is deliberately aligning to. Frames are the
 * floor — when nothing else is near, the result still lands on a frame, so a
 * drag can never produce a time between two frames.
 */
export function snap(
  t: Seconds, view: TimeView, width: number, targets: SnapTargets,
  fps: number, enabled = true,
): SnapResult {
  if (!enabled) return { t, to: null, kind: null };

  const tolerance = view.secondsPerPixel(width) * SNAP_PX;
  let best: { t: Seconds; kind: SnapResult['kind'] } | null = null;
  let bestD = tolerance;

  /* Nearest wins, and the playhead wins a tie. A stronger preference — the
   * playhead beating a genuinely closer event — sounds helpful and is not:
   * snapping becomes unpredictable the moment two targets are close, and
   * unpredictable snapping is worse than none. */
  const consider = (candidate: Seconds, kind: SnapResult['kind']) => {
    const d = Math.abs(candidate - t);
    if (d < bestD || (d === bestD && kind === 'playhead')) {
      bestD = d;
      best = { t: candidate, kind } as { t: Seconds; kind: SnapResult['kind'] };
    }
  };

  consider(targets.playhead, 'playhead');
  consider(0, 'edge');
  consider(targets.duration, 'edge');

  /* Binary search rather than a scan: this runs on every pointer move, and a
   * feature length score has tens of thousands of candidates. */
  const near = nearest(targets.events, t);
  for (const c of near) consider(c, 'event');

  const found = best as { t: Seconds; kind: SnapResult['kind'] } | null;
  if (found) return { t: found.t, to: found.t, kind: found.kind };
  return { t: snapToFrame(t, fps), to: null, kind: 'frame' };
}

function nearest(sorted: Seconds[], t: Seconds): Seconds[] {
  if (!sorted.length) return [];
  let lo = 0;
  let hi = sorted.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (sorted[mid] < t) lo = mid + 1; else hi = mid;
  }
  const out: Seconds[] = [];
  if (lo > 0) out.push(sorted[lo - 1]);
  if (lo < sorted.length) out.push(sorted[lo]);
  return out;
}
