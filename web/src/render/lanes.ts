/* Turning a track into a drawing.
 *
 * Two rules run through all of it.
 *
 * Nothing off screen is drawn. That is what makes a two hour score — around
 * 45,000 curve points — possible at all, and every loop here starts by asking
 * the view whether the thing is visible.
 *
 * Amplitude is always visible. The previous timeline drew a cue as a fixed
 * block, so a gust at 0.2 and a gust at 1.0 were the same rectangle: it showed
 * *when* and never *how hard*, which is half of what a score says. Here an
 * event's height and fill both carry its amplitude, and its width carries its
 * duration, so a track reads as a bar chart of strength over time.
 */

import { TimeView } from '../core/view';
import {
  amplitudeOf, colourOf, cueEnd, isHSI, isNominated, isSpan, valueAt,
  type Cue, type Point, type Track,
} from '../core/score';
import { DrawList } from './drawlist';

export interface Theme {
  ink: string;
  muted: string;
  line: string;
  grid: string;
  event: string;
  eventSoft: string;
  playhead: string;
  warn: string;
  channel: Record<string, string>;
}

export interface LaneBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Minimum width a span gets, so a two frame burst is still a target. */
const MIN_EVENT_W = 3;

/** Above this many points per pixel, draw an envelope instead of a line. */
const ENVELOPE_THRESHOLD = 1.5;

/**
 * Above this many points per pixel, draw the line but not its handles.
 *
 * Three bands, not two. A handle is a thing you take hold of, and drawing one
 * every four pixels produces a chain of overlapping circles that hides the
 * curve it is supposed to let you edit — visually it reads as noise, and none
 * of them can be hit anyway. Roughly one every twelve pixels is the point
 * where they are still individually grabbable.
 */
const HANDLE_THRESHOLD = 1 / 12;

/* --- cue lanes ---------------------------------------------------------- */

/**
 * Cues, drawn as bars whose height is amplitude and whose width is duration.
 *
 * A momentary cue has no width to give it, so it becomes a tapered marker
 * instead of a one pixel bar — the two must never be confusable, because the
 * difference between an instant and a four second hold is the entire reason
 * spans exist in this format.
 */
export function drawCues(
  list: DrawList, track: Track, view: TimeView, box: LaneBox, theme: Theme,
  opts: { selected?: ReadonlySet<unknown> } = {},
): void {
  const cues = track.cues ?? [];
  const pad = 4;
  const floor = box.y + box.h - pad;
  const height = box.h - pad * 2;

  for (let i = 0; i < cues.length; i++) {
    const cue = cues[i];
    const end = cueEnd(cue);
    if (!view.intersects(cue.t, end)) { list.culled++; continue; }

    const x = box.x + view.toX(cue.t, box.w);
    const amp = amplitudeOf(cue.params);
    /* An event with no amplitude at all — a bare stop — is drawn at full
     * height rather than at nothing, because "no amplitude" and "zero
     * amplitude" are different statements and a flat bar would tell the
     * second lie. */
    const level = amp === null ? 1 : amp;
    const h = Math.max(2, height * (0.18 + level * 0.82));
    const y = floor - h;

    const selected = opts.selected?.has(cue) ?? false;
    const tint = colourOf(cue.params) ?? theme.event;

    if (isSpan(cue)) {
      const w = Math.max(MIN_EVENT_W, view.toX(end, box.w) - view.toX(cue.t, box.w));
      list.rect({
        x, y, w, h,
        fill: isNominated(cue) ? undefined : tint,
        stroke: isNominated(cue) ? tint : selected ? theme.ink : undefined,
        lineWidth: selected ? 2 : 1.25,
        radius: 2,
        alpha: isNominated(cue) ? 0.9 : 0.88,
      });
      /* End caps. A span's edges are where the conductor sends a start and a
       * stop, and they are what a person drags, so they are drawn as real
       * edges rather than left implicit in a fill. */
      list.line({ x1: x + 0.5, y1: y, x2: x + 0.5, y2: floor, stroke: tint, lineWidth: 1.5 });
      list.line({ x1: x + w - 0.5, y1: y, x2: x + w - 0.5, y2: floor, stroke: tint, lineWidth: 1.5 });
    } else {
      /* A marker: a stem to the floor with a head at its amplitude. */
      list.line({ x1: x, y1: floor, x2: x, y2: y, stroke: tint, lineWidth: selected ? 2.5 : 1.5 });
      list.dot({ x, y, r: selected ? 4.5 : 3.5, fill: isNominated(cue) ? undefined : tint, stroke: tint, lineWidth: 1.5 });
    }

    /* The label only when there is room for it, so a dense track does not
     * turn into overlapping text. */
    const w = isSpan(cue)
      ? Math.max(MIN_EVENT_W, view.toX(end, box.w) - view.toX(cue.t, box.w))
      : 0;
    if (w > 46 && box.h > 26) {
      list.text({
        x: x + 5, y: y + 12, s: cue.action, fill: theme.ink,
        size: 10, weight: 500, alpha: 0.85,
      });
    }
  }
}

/* --- curve lanes -------------------------------------------------------- */

/**
 * One channel of a curve.
 *
 * Two representations of the same data, chosen by density. Zoomed in, discrete
 * points with handles you can grab. Zoomed out — where a feature film puts
 * thousands of points behind every pixel — a min/max envelope per pixel
 * column, the way an audio editor draws a waveform. Drawing 45,000 line
 * segments to fill 900 pixels is not more honest, it is just slower and it
 * aliases into noise.
 */
export function drawCurve(
  list: DrawList, track: Track, channel: string, view: TimeView, box: LaneBox, theme: Theme,
  opts: { selected?: ReadonlySet<unknown>; showHandles?: boolean } = {},
): void {
  const points = (track.points ?? []).filter((p) => channel in (p.value ?? {}));
  const colour = theme.channel[channel] ?? theme.event;
  const pad = 3;
  const top = box.y + pad;
  const bottom = box.y + box.h - pad;
  const y = (v: number) => bottom - Math.max(0, Math.min(1, v)) * (bottom - top);

  /* Gridline at half, so a value can be read without a ruler. */
  list.line({
    x1: box.x, y1: y(0.5), x2: box.x + box.w, y2: y(0.5),
    stroke: theme.grid, lineWidth: 1, alpha: 0.5, dash: [2, 4],
  });

  if (!points.length) {
    list.text({
      x: box.x + 8, y: box.y + box.h / 2 + 3,
      s: 'no points — double click to start a curve',
      fill: theme.muted, size: 10, alpha: 0.75,
    });
    return;
  }

  const density = points.length / Math.max(1, box.w);
  const visible = visibleRange(points, view);

  if (density > ENVELOPE_THRESHOLD) {
    drawEnvelope(list, points, channel, view, box, colour, y);
  } else {
    const pts: number[] = [];
    for (let i = visible.from; i <= visible.to; i++) {
      const p = points[i];
      pts.push(box.x + view.toX(p.t, box.w), y(p.value[channel]));
    }
    if (pts.length >= 4) {
      list.path({ pts, stroke: colour, fill: colour, baseline: bottom, lineWidth: 1.6, alpha: 1 });
    }
    const visibleDensity = (visible.to - visible.from + 1) / Math.max(1, box.w);
    if (opts.showHandles !== false && visibleDensity <= HANDLE_THRESHOLD) {
      for (let i = visible.from; i <= visible.to; i++) {
        const p = points[i];
        const px = box.x + view.toX(p.t, box.w);
        const sel = opts.selected?.has(p) ?? false;
        list.dot({
          x: px, y: y(p.value[channel]), r: sel ? 5 : 3.6,
          fill: sel ? colour : theme.eventSoft, stroke: colour, lineWidth: 1.5,
        });
      }
    }
  }
  list.culled += points.length - (visible.to - visible.from + 1);
}

/**
 * The min/max envelope: for every pixel column, the highest and lowest value
 * any point in it takes. Fixed cost in the width of the lane, whatever the
 * score contains.
 */
function drawEnvelope(
  list: DrawList, points: Point[], channel: string, view: TimeView, box: LaneBox,
  colour: string, y: (v: number) => number,
): void {
  const cols = Math.max(1, Math.floor(box.w));
  const lo = new Float32Array(cols).fill(Infinity);
  const hi = new Float32Array(cols).fill(-Infinity);
  let any = false;

  const range = visibleRange(points, view);
  for (let i = range.from; i <= range.to; i++) {
    const p = points[i];
    const col = Math.floor(view.fractionOf(p.t) * cols);
    if (col < 0 || col >= cols) continue;
    const v = p.value[channel];
    if (typeof v !== 'number') continue;
    if (v < lo[col]) lo[col] = v;
    if (v > hi[col]) hi[col] = v;
    any = true;
  }
  if (!any) return;

  /* Walk the columns once, filling gaps by holding the last value — a column
   * with no point in it is not a hole in the signal, it is a stretch where the
   * curve is simply between two points. */
  const upper: number[] = [];
  const lower: number[] = [];
  let lastLo = 0;
  let lastHi = 0;
  for (let c = 0; c < cols; c++) {
    if (lo[c] === Infinity) {
      if (!upper.length) continue;
    } else {
      lastLo = lo[c];
      lastHi = hi[c];
    }
    const x = box.x + c;
    upper.push(x, y(lastHi));
    lower.push(x, y(lastLo));
  }
  if (upper.length < 4) return;

  /* One closed shape: down the tops, back along the bottoms. */
  const shape = upper.slice();
  for (let i = lower.length - 2; i >= 0; i -= 2) shape.push(lower[i], lower[i + 1]);
  list.path({ pts: shape, fill: colour, stroke: colour, lineWidth: 1, alpha: 0.55 });
}

/**
 * The slice of a sorted point array that is on screen, by binary search.
 *
 * Scanning the whole array to find it would put the cost back that the window
 * exists to remove: at 45,000 points and sixty frames a second, a linear scan
 * per lane per frame is millions of comparisons for a few hundred results.
 */
export function visibleRange(points: Point[], view: TimeView): { from: number; to: number } {
  if (!points.length) return { from: 0, to: -1 };
  const from = Math.max(0, lowerBound(points, view.start) - 1);
  const to = Math.min(points.length - 1, lowerBound(points, view.end));
  return { from, to };
}

function lowerBound(points: Point[], t: number): number {
  let lo = 0;
  let hi = points.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (points[mid].t < t) lo = mid + 1; else hi = mid;
  }
  return lo;
}

/* --- the collapsed colour ribbon ---------------------------------------- */

/**
 * A colour track, collapsed to the colour it actually makes.
 *
 * For an RGB curve this says more than three value lines do: what a person in
 * the room sees is one colour changing over time, and this is that, sampled
 * across the window. Three separate graphs are the right tool for editing a
 * channel and the wrong one for judging a look.
 */
export function drawRibbon(
  list: DrawList, track: Track, channels: string[], view: TimeView, box: LaneBox, theme: Theme,
  samples = 96,
): void {
  const points = track.points ?? [];
  if (!points.length) {
    list.rect({ x: box.x, y: box.y, w: box.w, h: box.h, fill: theme.eventSoft, alpha: 0.4 });
    list.text({
      x: box.x + 8, y: box.y + box.h / 2 + 3, s: 'no points',
      fill: theme.muted, size: 10, alpha: 0.75,
    });
    return;
  }

  const stops: Array<{ at: number; colour: string }> = [];
  for (let i = 0; i <= samples; i++) {
    const at = i / samples;
    const t = view.start + view.span * at;
    const v = valueAt(points, t, channels, isHSI(track));
    stops.push({ at, colour: colourOf(v) ?? theme.eventSoft });
  }
  list.ribbon({ x: box.x, y: box.y, w: box.w, h: box.h, stops });
}

/**
 * Anything that is not a colour, collapsed: the amplitude envelope of whatever
 * the track does, so a collapsed group still shows where it is busy.
 */
export function drawCollapsedEnvelope(
  list: DrawList, track: Track, view: TimeView, box: LaneBox, theme: Theme,
): void {
  if (track.type === 'curve') {
    const chans = new Set<string>();
    for (const p of track.points ?? []) for (const k of Object.keys(p.value ?? {})) chans.add(k);
    const first = [...chans][0];
    if (first) drawCurve(list, track, first, view, box, theme, { showHandles: false });
    return;
  }
  drawCues(list, track, view, box, theme);
}

/* --- shared chrome ------------------------------------------------------ */

export function drawPlayhead(list: DrawList, t: number, view: TimeView, box: LaneBox, theme: Theme): void {
  if (!view.intersects(t, t)) return;
  const x = box.x + view.toX(t, box.w);
  list.line({ x1: x, y1: box.y, x2: x, y2: box.y + box.h, stroke: theme.playhead, lineWidth: 1.5 });
}

/** A cue's dispatch moment: `latency` earlier than its authored time. */
export function drawLatencyGhost(
  list: DrawList, cue: Cue, latency: number, view: TimeView, box: LaneBox, theme: Theme,
): void {
  if (latency <= 0) return;
  const fireAt = cue.t - latency;
  if (!view.intersects(fireAt, cue.t)) return;
  const x1 = box.x + view.toX(fireAt, box.w);
  const x2 = box.x + view.toX(cue.t, box.w);
  const mid = box.y + box.h / 2;
  list.line({ x1, y1: mid, x2, y2: mid, stroke: theme.muted, lineWidth: 1, dash: [2, 3], alpha: 0.8 });
  list.dot({ x: x1, y: mid, r: 2.5, stroke: theme.muted, lineWidth: 1 });
}
