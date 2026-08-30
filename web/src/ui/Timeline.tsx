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

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { TimeView, ticks } from '../core/view';
import { canCollapse, layout, summaryLabel, type Layout } from '../core/layout';
import { timecode } from '../core/time';
import { channelsOf, latencyOf, type Rig, type Score } from '../core/score';
import { DrawList, paint } from '../render/drawlist';
import {
  drawCalm, drawCollapsedEnvelope, drawCues, drawCurve, drawLatency,
  drawPlayhead, drawRibbon,
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
  /**
   * Bumped whenever the document changes structurally.
   *
   * The score object is mutated in place by commands, so its identity never
   * changes and a memo keyed on it alone never recomputes. Value edits still
   * drew, because the canvas re-reads the data every frame; adding or removing
   * a track or a point changes the *rows*, and those were silently stale.
   */
  revision: number;
  /** Which of the advisory overlays to draw. */
  overlays: { calm: boolean; latency: boolean };
}

export function Timeline(props: TimelineProps) {
  const { score, rig, view, time, collapsed, order, onView, edit, revision, overlays } = props;
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const size = useRef({ w: 0, h: 0 });

  const lay: Layout = useMemo(
    () => layout(score.tracks ?? [], { collapsed, rig, order }),
    [score, collapsed, rig, order, revision],
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

    /* Rest first, behind everything: it is a property of the show rather than
     * of any one instrument, so it spans every lane. */
    if (overlays.calm && score.calm?.length) {
      drawCalm(list, score.calm, view, { x: 0, y: RULER_H, w, h: h - RULER_H }, theme);
    }

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
          if (overlays.latency) {
            drawLatency(list, track, latencyOf(track.instrument, rig), view, box, theme);
          }
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
  }, [score, rig, view, time, lay, fps, overlays, edit.selected, edit.band, edit.guide, edit.version]);

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

  const wheel = useCallback((e: WheelEvent) => {
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

  /* Bound natively rather than through onWheel, so the page stays put.
   *
   * React registers wheel at the root as a passive listener, which means
   * preventDefault() inside an onWheel handler is silently ignored — the
   * timeline panned and the window scrolled at the same time, and the thing
   * you were looking at slid off the screen while you were looking at it. Only
   * a listener attached to the element with { passive: false } may refuse the
   * page scroll.
   *
   * The handler is reached through a ref so this subscribes once. Keying the
   * effect on the callback would tear the listener down and rebuild it on
   * every render, which is how a wheel event lands between the two and does
   * nothing at all.
   */
  const wheelRef = useRef(wheel);
  wheelRef.current = wheel;
  useEffect(() => {
    const host = wrap.current;
    if (!host) return;
    const handle = (e: WheelEvent) => wheelRef.current(e);
    host.addEventListener('wheel', handle, { passive: false });
    return () => host.removeEventListener('wheel', handle);
  }, []);

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
  /** Drop `instrument` before `before`, or at the end when it is null. */
  onMoveTo: (instrument: string, before: string | null) => void;
  /** Null when the rig has nothing left to add. */
  onAddTrack: ((e: React.MouseEvent) => void) | null;
  revision: number;
}) {
  const { score, rig, collapsed, order, onToggleCollapse, onMove, onMoveTo, onAddTrack, revision } = props;
  /* Which group is being carried, and which one it would land in front of.
   * Held here rather than in the app: it is entirely about this column. */
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<string | null>(null);
  const lay = useMemo(
    () => layout(score.tracks ?? [], { collapsed, rig, order }),
    [score, collapsed, rig, order, revision],
  );

  return (
    <div className="tl-heads" style={{ paddingTop: RULER_H }}>
      {lay.rows.map((row, i) => (
        <div
          key={row.instrument + '/' + (row.channel ?? '') + i}
          className={'tl-head'
            + (row.head ? ' is-head' : '')
            + (dragging === row.instrument ? ' is-dragging' : '')
            + (over === row.instrument && row.head ? ' is-over' : '')}
          style={{ height: row.h }}
          /* Only the row carrying the name is a handle: dragging a channel
             lane would be ambiguous about what is being moved. */
          draggable={row.head}
          onDragStart={(e) => {
            if (!row.head) return;
            setDragging(row.instrument);
            e.dataTransfer.effectAllowed = 'move';
            /* Firefox refuses to start a drag without data on it. */
            e.dataTransfer.setData('text/plain', row.instrument);
          }}
          onDragEnd={() => { setDragging(null); setOver(null); }}
          onDragOver={(e) => {
            if (!dragging || dragging === row.instrument) return;
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';
            setOver(row.instrument);
          }}
          onDrop={(e) => {
            if (!dragging || dragging === row.instrument) return;
            e.preventDefault();
            onMoveTo(dragging, row.instrument);
            setDragging(null);
            setOver(null);
          }}
        >
          {row.head ? (
            <>
              {/* A chevron only where folding means something. A single
                  channel track has nothing to fold into, and a control that
                  only changes a row's height is a control that lies about
                  having an effect. */}
              {canCollapse(score.tracks[row.track], rig) ? (
                <button
                  className="tl-chev"
                  onClick={() => onToggleCollapse(row.instrument)}
                  title={collapsed.has(row.instrument) ? 'Expand channels' : 'Collapse channels'}
                  aria-label={collapsed.has(row.instrument) ? 'Expand channels' : 'Collapse channels'}
                  aria-expanded={!collapsed.has(row.instrument)}
                >
                  {collapsed.has(row.instrument) ? '▶' : '▼'}
                </button>
              ) : <span className="tl-chev-gap" />}

              <span className="tl-name" title={row.instrument}>{row.instrument}</span>

              {/* What the compound row is showing, when there is one. */}
              {!row.editable && (
                <span className="tl-summary">{summaryLabel(score.tracks[row.track], rig)}</span>
              )}
              {row.channel && row.editable && (
                <span className={'tl-chan inline ch-' + row.channel}>{row.channel}</span>
              )}

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

      {/* Always present rather than hidden behind a right click on empty
          space: the canvas is exactly as tall as its rows, so there is no
          empty space to click and the action would be unreachable. */}
      {onAddTrack && (
        <button
          className="tl-add"
          onClick={onAddTrack}
          title="Add a track for an instrument the rig has and the score does not"
        >
          + Add track
        </button>
      )}
    </div>
  );
}

export { RULER_H };
