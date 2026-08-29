/* The whole film, compacted, with a box showing what the main surface has.
 *
 * The main timeline deliberately shows a tenth of the film, which solves the
 * rendering problem and creates a navigation one: you can no longer see where
 * you are. This is the answer — the entire score at a glance, degraded on
 * purpose, and the visible window drawn on it as something you can grab.
 *
 * Degraded on purpose is the important part. This never draws handles, labels
 * or individual points; it draws each track's envelope across the whole film,
 * at a fixed cost in the width of the strip. It is a map, not a second editor.
 */

import { useCallback, useEffect, useMemo, useRef } from 'react';
import { TimeView } from '../core/view';
import { orderTracks } from '../core/layout';
import { amplitudeOf, cueEnd, type Rig, type Score } from '../core/score';
import { DrawList, paint } from '../render/drawlist';
import { dark, kindColour } from './theme';

const HEIGHT = 46;
const FONT = "'IBM Plex Sans', system-ui, sans-serif";
const MONO = "'IBM Plex Mono', ui-monospace, monospace";

export function Overview(props: {
  score: Score;
  rig: Rig | null;
  view: TimeView;
  time: number;
  onView: () => void;
}) {
  const { score, view, time, onView } = props;
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);

  /* The compacted picture depends only on the score, never on the window, so
   * it is built once per score rather than per frame. At 45,000 points that
   * distinction is the difference between a smooth drag and a slideshow. */
  const bands = useMemo(() => buildBands(score), [score]);

  const draw = useCallback(() => {
    const host = wrap.current;
    const cv = canvas.current;
    if (!host || !cv) return;
    const w = host.clientWidth;
    if (w <= 0) return;

    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    cv.width = Math.round(w * dpr);
    cv.height = Math.round(HEIGHT * dpr);
    cv.style.width = w + 'px';
    cv.style.height = HEIGHT + 'px';

    const ctx = cv.getContext('2d');
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, HEIGHT);

    const theme = dark;
    const list = new DrawList();
    const duration = Math.max(0.001, score.duration);
    const at = (t: number) => (t / duration) * w;

    list.rect({ x: 0, y: 0, w, h: HEIGHT, fill: theme.eventSoft });

    /* Every track as one thin band of activity. Stacked, so a busy stretch of
     * the film is visibly busy across all of them at once — which is the
     * question this strip is really answering. */
    const bandH = Math.max(2, (HEIGHT - 8) / Math.max(1, bands.length));
    bands.forEach((band, i) => {
      const y = 4 + i * bandH;
      for (const seg of band.segments) {
        const x1 = at(seg.a);
        const x2 = Math.max(x1 + 1, at(seg.b));
        list.rect({
          x: x1, y: y + bandH * (1 - seg.level) * 0.75,
          w: x2 - x1, h: Math.max(1.5, bandH * (0.25 + seg.level * 0.75) - 1),
          fill: band.colour, alpha: 0.85,
        });
      }
    });

    /* The window. Everything outside it is dimmed rather than the box being
     * outlined, because "what am I not looking at" is the thing worth
     * showing. */
    const x1 = at(view.start);
    const x2 = at(view.end);
    list.rect({ x: 0, y: 0, w: Math.max(0, x1), h: HEIGHT, fill: '#0d1015', alpha: 0.66 });
    list.rect({ x: x2, y: 0, w: Math.max(0, w - x2), h: HEIGHT, fill: '#0d1015', alpha: 0.66 });
    list.rect({
      x: x1, y: 0.5, w: Math.max(2, x2 - x1), h: HEIGHT - 1,
      stroke: theme.ink, lineWidth: 1.5, radius: 2,
    });
    /* Grips, so it looks like something you can take hold of. */
    for (const gx of [x1, x2]) {
      list.rect({ x: gx - 1.5, y: HEIGHT / 2 - 7, w: 3, h: 14, fill: theme.ink, radius: 1.5 });
    }

    const px = at(time);
    list.line({ x1: px, y1: 0, x2: px, y2: HEIGHT, stroke: theme.playhead, lineWidth: 1, alpha: 0.9 });

    paint(ctx, list, FONT, MONO);
  }, [bands, score.duration, view, time]);

  useEffect(() => { draw(); });
  useEffect(() => {
    const on = () => draw();
    window.addEventListener('resize', on);
    return () => window.removeEventListener('resize', on);
  }, [draw]);

  /* Click to jump there, drag to scrub the window along. Grabbing an edge
   * resizes instead, which is the fastest way to change zoom by a lot. */
  const pointer = useCallback((e: React.PointerEvent) => {
    const host = wrap.current;
    if (!host || e.button !== 0) return;
    const box = host.getBoundingClientRect();
    const duration = Math.max(0.001, score.duration);
    const toTime = (clientX: number) =>
      ((clientX - box.left) / Math.max(1, box.width)) * duration;

    const edgePx = 7;
    const x1 = (view.start / duration) * box.width;
    const x2 = (view.end / duration) * box.width;
    const localX = e.clientX - box.left;
    const grab: 'start' | 'end' | 'move' =
      Math.abs(localX - x1) < edgePx ? 'start'
        : Math.abs(localX - x2) < edgePx ? 'end'
          : 'move';

    if (grab === 'move') view.set(toTime(e.clientX) - view.span / 2, view.span);
    onView();

    const move = (ev: PointerEvent) => {
      const t = toTime(ev.clientX);
      if (grab === 'move') view.set(t - view.span / 2, view.span);
      else if (grab === 'start') view.set(t, view.end - t);
      else view.set(view.start, t - view.start);
      onView();
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }, [view, score.duration, onView]);

  return (
    <div className="tl-overview" ref={wrap} onPointerDown={pointer} title="The whole film. Drag the box to move, its edges to zoom.">
      <canvas ref={canvas} />
    </div>
  );
}

interface Band {
  colour: string;
  segments: Array<{ a: number; b: number; level: number }>;
}

/**
 * Reduce a track to a few hundred segments of "how busy, how hard".
 *
 * Fixed resolution rather than one segment per event: the strip is a few
 * hundred pixels wide however long the film is, so a feature and a test clip
 * cost the same to draw.
 */
function buildBands(score: Score, columns = 480): Band[] {
  const duration = Math.max(0.001, score.duration);
  const theme = dark;
  const out: Band[] = [];

  for (const ti of orderTracks(score.tracks ?? [])) {
    const track = score.tracks[ti];
    const level = new Float32Array(columns);
    const col = (t: number) =>
      Math.max(0, Math.min(columns - 1, Math.floor((t / duration) * columns)));

    if (track.type === 'curve') {
      for (const p of track.points ?? []) {
        const amp = amplitudeOf(p.value) ?? 0;
        const c = col(p.t);
        if (amp > level[c]) level[c] = amp;
      }
    } else {
      for (const cue of track.cues ?? []) {
        const amp = amplitudeOf(cue.params) ?? 1;
        const from = col(cue.t);
        const to = col(cueEnd(cue));
        for (let c = from; c <= to; c++) if (amp > level[c]) level[c] = amp;
      }
    }

    /* Merge runs of similar level into segments, so a dense curve becomes a
     * handful of rectangles instead of one per column. */
    const segments: Band['segments'] = [];
    let c = 0;
    while (c < columns) {
      if (level[c] <= 0) { c++; continue; }
      const start = c;
      let peak = 0;
      while (c < columns && level[c] > 0) { peak = Math.max(peak, level[c]); c++; }
      segments.push({
        a: (start / columns) * duration,
        b: (c / columns) * duration,
        level: peak,
      });
    }

    const kind = track.instrument.split('.')[0];
    out.push({ colour: kindColour[kind] ?? theme.event, segments });
  }
  return out;
}

export { HEIGHT as OVERVIEW_H };
