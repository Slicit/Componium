/* The timeline surface.
 *
 * One canvas for every lane. The track names beside it are DOM, because they
 * are text and buttons and should behave like text and buttons; the lanes are
 * canvas, because a two hour score is 45,000 curve points and no arrangement
 * of DOM nodes survives that. The two are kept in step by `layout()`, which
 * both of them read rather than each working it out.
 *
 * This component owns pixels and pointers and nothing else: what to draw comes
 * from the renderer, and what the numbers mean comes from the view model.
 */

import { useCallback, useEffect, useMemo, useRef } from 'react';
import { TimeView, ticks } from '../core/view';
import { layout, type Layout } from '../core/layout';
import { timecode } from '../core/time';
import { channelsOf, type Rig, type Score } from '../core/score';
import { DrawList, paint } from '../render/drawlist';
import {
  drawCollapsedEnvelope, drawCues, drawCurve, drawPlayhead, drawRibbon,
} from '../render/lanes';
import { dark } from './theme';
import type { Editing } from './useEditing';

const RULER_H = 26;
const FONT = "'IBM Plex Sans', system-ui, sans-serif";
const MONO = "'IBM Plex Mono', ui-monospace, monospace";

export interface TimelineProps {
  score: Score;
  rig: Rig | null;
  view: TimeView;
  time: number;
  collapsed: Set<string>;
  order: string[];
  onSeek: (t: number) => void;
  /** Called whenever the view is panned or zoomed, so React can re-render. */
  onView: () => void;
  edit: Editing;
}

export function Timeline(props: TimelineProps) {
  const { score, rig, view, time, collapsed, order, onView, edit } = props;
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const size = useRef({ w: 0, h: 0 });

  const lay: Layout = useMemo(
    () => layout(score.tracks ?? [], { collapsed, rig, order }),
    [score, collapsed, rig, order],
  );
  const fps = score.fps ?? 24;

  /* One draw. Called from an animation frame, from a resize, and directly
   * after any interaction — the last of those because a preview that only
   * updates on a frame is a preview that shows nothing in an environment that
   * never delivers one. */
  const draw = useCallback(() => {
    const cv = canvas.current;
    const host = wrap.current;
    if (!cv || !host) return;

    const w = host.clientWidth;
    const h = RULER_H + lay.height;
    if (w <= 0) return;

    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    if (size.current.w !== w || size.current.h !== h) {
      size.current = { w, h };
      cv.width = Math.round(w * dpr);
      cv.height = Math.round(h * dpr);
      cv.style.width = w + 'px';
      cv.style.height = h + 'px';
    }

    const ctx = cv.getContext('2d');
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    const list = new DrawList();
    const theme = dark;

    /* --- ruler --- */
    list.rect({ x: 0, y: 0, w, h: RULER_H, fill: theme.eventSoft });
    for (const tick of ticks(view, w)) {
      const x = view.toX(tick.t, w);
      list.line({
        x1: x, y1: tick.major ? RULER_H - 12 : RULER_H - 6, x2: x, y2: RULER_H,
        stroke: tick.major ? theme.muted : theme.line, lineWidth: 1,
      });
      if (tick.major) {
        list.text({
          x: x + 4, y: 12, s: timecode(tick.t, fps), fill: theme.muted,
          size: 10, mono: true,
        });
      }
      /* The same tick, faint, down the lanes: a gridline is what lets you
       * compare two tracks at a glance without dragging a playhead about. */
      list.line({
        x1: x, y1: RULER_H, x2: x, y2: h,
        stroke: theme.line, lineWidth: 1, alpha: tick.major ? 0.9 : 0.4,
      });
    }
    list.line({ x1: 0, y1: RULER_H - 0.5, x2: w, y2: RULER_H - 0.5, stroke: theme.line });

    /* --- lanes --- */
    for (const row of lay.rows) {
      const track = score.tracks[row.track];
      const box = { x: 0, y: RULER_H + row.y, w, h: row.h };

      if (row.y > 0) {
        list.line({
          x1: 0, y1: box.y - 0.5, x2: w, y2: box.y - 0.5,
          stroke: theme.line, alpha: row.head ? 1 : 0.45,
        });
      }

      switch (row.draw) {
        case 'cues':
          drawCues(list, track, view, box, theme, { selected: edit.selected });
          break;
        case 'curve':
          drawCurve(list, track, row.channel!, view, box, theme, { selected: edit.selected });
          break;
        case 'ribbon':
          drawRibbon(list, track, channelsOf(track, rig), view, box, theme);
          break;
        case 'envelope':
          drawCollapsedEnvelope(list, track, view, box, theme);
          break;
      }
    }

    drawPlayhead(list, time, view, { x: 0, y: 0, w, h }, theme);

    /* The snap guide: a line at whatever the drag has locked on to, which is
     * the only way to tell a snap from a coincidence. */
    if (edit.guide !== null && view.intersects(edit.guide, edit.guide)) {
      const gx = view.toX(edit.guide, w);
      list.line({ x1: gx, y1: 0, x2: gx, y2: h, stroke: theme.event, lineWidth: 1, dash: [3, 3] });
    }

    if (edit.band) {
      const bx = Math.min(edit.band.x1, edit.band.x2);
      const by = Math.min(edit.band.y1, edit.band.y2);
      list.rect({
        x: bx, y: by,
        w: Math.abs(edit.band.x2 - edit.band.x1),
        h: Math.abs(edit.band.y2 - edit.band.y1),
        fill: theme.event, stroke: theme.event, alpha: 0.18,
      });
    }

    paint(ctx, list, FONT, MONO);
  }, [score, rig, view, time, lay, fps, edit.selected, edit.band, edit.guide, edit.version]);

  useEffect(() => { draw(); });

  useEffect(() => {
    const on = () => draw();
    window.addEventListener('resize', on);
    /* ResizeObserver is the right tool and is not delivered in every context
     * this runs in, so the window event is the floor rather than the plan. */
    let ro: ResizeObserver | null = null;
    if (typeof ResizeObserver !== 'undefined' && wrap.current) {
      ro = new ResizeObserver(on);
      ro.observe(wrap.current);
    }
    return () => { window.removeEventListener('resize', on); ro?.disconnect(); };
  }, [draw]);

  /* --- pointer --- */

  const wheel = useCallback((e: React.WheelEvent) => {
    const host = wrap.current;
    if (!host) return;
    e.preventDefault();
    const box = host.getBoundingClientRect();
    const anchor = (e.clientX - box.left) / Math.max(1, box.width);

    if (e.ctrlKey || e.metaKey || e.shiftKey) {
      /* Zoom about the cursor. Trackpad pinch arrives as ctrl+wheel, which is
       * why that gesture is the one bound to zoom. */
      view.zoomAt(anchor, Math.exp(e.deltaY * 0.0015));
    } else {
      const by = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
      view.panByFraction((by / Math.max(1, box.width)) * 1.2);
    }
    onView();
  }, [view, onView]);

  const geom = useCallback(() => {
    const host = wrap.current!;
    return {
      rect: host.getBoundingClientRect(),
      width: host.clientWidth,
      rulerH: RULER_H,
      layout: lay,
    };
  }, [lay]);

  return (
    <div
      className="tl-surface"
      ref={wrap}
      style={{ cursor: edit.cursor }}
      onWheel={wheel}
      onPointerDown={(e) => edit.onPointerDown(e, geom())}
      onPointerMove={(e) => edit.onPointerMove(e, geom())}
      onDoubleClick={(e) => edit.onDoubleClick(e, geom())}
      onContextMenu={(e) => edit.onContextMenu(e, geom())}
    >
      <canvas ref={canvas} />
    </div>
  );
}

/** The names and controls beside the lanes, aligned to the same layout. */
export function TrackHeads(props: {
  score: Score;
  rig: Rig | null;
  collapsed: Set<string>;
  order: string[];
  onToggleCollapse: (instrument: string) => void;
  onMove: (instrument: string, by: number) => void;
}) {
  const { score, rig, collapsed, order, onToggleCollapse, onMove } = props;
  const lay = useMemo(
    () => layout(score.tracks ?? [], { collapsed, rig, order }),
    [score, collapsed, rig, order],
  );

  return (
    <div className="tl-heads" style={{ paddingTop: RULER_H }}>
      {lay.rows.map((row, i) => (
        <div
          key={row.instrument + '/' + (row.channel ?? '') + i}
          className={'tl-head' + (row.head ? ' is-head' : '')}
          style={{ height: row.h }}
        >
          {row.head ? (
            <>
              <button
                className="tl-chev"
                onClick={() => onToggleCollapse(row.instrument)}
                title={collapsed.has(row.instrument) ? 'Expand' : 'Collapse'}
                aria-label={collapsed.has(row.instrument) ? 'Expand' : 'Collapse'}
              >
                {score.tracks[row.track].type === 'curve'
                  ? (collapsed.has(row.instrument) ? '▸' : '▾')
                  : '·'}
              </button>
              <span className="tl-name" title={row.instrument}>{row.instrument}</span>
              {/* The head row of an expanded curve is also a channel lane, so
                  it needs its channel named too. Without this the first lane —
                  red, always — is the one lane with no label on it. */}
              {row.channel && <span className={'tl-chan inline ch-' + row.channel}>{row.channel}</span>}
              <span className="tl-move">
                <button onClick={() => onMove(row.instrument, -1)} title="Move up" aria-label="Move up">↑</button>
                <button onClick={() => onMove(row.instrument, 1)} title="Move down" aria-label="Move down">↓</button>
              </span>
            </>
          ) : (
            <span className={'tl-chan ch-' + row.channel}>{row.channel}</span>
          )}
        </div>
      ))}
    </div>
  );
}

export { RULER_H };
