/* The gestures, and what they do to the score.
 *
 * Split out of the component because it is the fiddliest part and it is nearly
 * all decisions rather than markup: what a drag means, when it becomes a drag
 * at all, what it snaps to, and which command it turns into. The component
 * below it stays about pixels.
 *
 * Every change goes through a command on the history, without exception. The
 * temptation during a drag is to set `cue.t` directly and record a command at
 * the end, which is one line shorter and means the undo stack and the document
 * disagree for the length of the gesture.
 */

import { useCallback, useRef, useState } from 'react';
import { TimeView } from '../core/view';
import type { Layout } from '../core/layout';
import { cursorFor, hitRange, hitTest, type Hit, type HitContext } from '../core/hit';
import { snap, snapTargets } from '../core/snap';
import {
  History, insertPoints, moveCues, movePoints, removeCues, removePoints, resizeCues,
} from '../core/history';
import { clamp, clamp01, round3 } from '../core/time';
import { cueEnd, isHSI, isSpan, valueAt, channelsOf, type Cue, type Point, type Rig, type Score } from '../core/score';

/** Below this many pixels a press is a click, not a drag. */
const SLOP = 3;

export type Selected = Set<Cue | Point>;

export interface Band { x1: number; y1: number; x2: number; y2: number }

export interface Editing {
  selected: Selected;
  band: Band | null;
  guide: number | null;
  cursor: string;
  version: number;
  onPointerDown: (e: React.PointerEvent, geom: Geometry) => void;
  onPointerMove: (e: React.PointerEvent, geom: Geometry) => void;
  onDoubleClick: (e: React.MouseEvent, geom: Geometry) => void;
  onContextMenu: (e: React.MouseEvent, geom: Geometry) => void;
  menu: { x: number; y: number; hit: Hit } | null;
  /** What a plain click landed on, for the inspector. Null when nothing is. */
  focus: { track: number; cue?: Cue; point?: Point; channel?: string } | null;
  clearFocus: () => void;
  closeMenu: () => void;
  setSelected: (s: Selected) => void;
  deleteSelection: () => void;
  clearSelection: () => void;
  selectAll: () => void;
}

export interface Geometry {
  rect: DOMRect;
  width: number;
  rulerH: number;
  layout: Layout;
}

export function useEditing(opts: {
  score: Score;
  rig: Rig | null;
  view: TimeView;
  history: History;
  time: number;
  fps: number;
  onSeek: (t: number) => void;
  onChanged: () => void;
}): Editing {
  const { score, rig, view, history, time, fps, onSeek, onChanged } = opts;
  const [selected, setSelected] = useState<Selected>(new Set());
  const [band, setBand] = useState<Band | null>(null);
  const [guide, setGuide] = useState<number | null>(null);
  const [cursor, setCursor] = useState('crosshair');
  const [menu, setMenu] = useState<{ x: number; y: number; hit: Hit } | null>(null);
  const [focus, setFocus] = useState<Editing['focus']>(null);
  const [version, setVersion] = useState(0);
  const gesture = useRef(0);

  const context = useCallback((geom: Geometry): HitContext => ({
    score, layout: geom.layout, view, width: geom.width, rulerH: geom.rulerH,
  }), [score, view]);

  const changed = useCallback(() => {
    setVersion((n) => n + 1);
    onChanged();
  }, [onChanged]);

  /* --- hover ---------------------------------------------------------- */

  const onPointerMove = useCallback((e: React.PointerEvent, geom: Geometry) => {
    const x = e.clientX - geom.rect.left;
    const y = e.clientY - geom.rect.top;
    setCursor(cursorFor(hitTest(context(geom), x, y)));
  }, [context]);

  /* --- press ---------------------------------------------------------- */

  const onPointerDown = useCallback((e: React.PointerEvent, geom: Geometry) => {
    if (e.button !== 0) return;
    const x = e.clientX - geom.rect.left;
    const y = e.clientY - geom.rect.top;
    const hit = hitTest(context(geom), x, y);
    const additive = e.shiftKey || e.metaKey || e.ctrlKey;
    const key = 'g' + (++gesture.current);

    if (hit.k === 'ruler') { scrub(geom, hit.t); return; }

    /* Selection happens on press, not release: an editor expects the thing it
     * grabbed to be selected before it starts moving. */
    let picked = selected;
    if (hit.k === 'cue' || hit.k === 'point') {
      const item = hit.k === 'cue' ? hit.cue : hit.point;
      if (additive) {
        picked = new Set(selected);
        picked.has(item) ? picked.delete(item) : picked.add(item);
      } else if (!selected.has(item)) {
        picked = new Set([item]);
      }
      setSelected(picked);
      /* A plain click opens the inspector on what was clicked. Dragging is how
       * you find a shape and a typed field is how you pin it down; both want
       * to be available without choosing a mode first. */
      setFocus(hit.k === 'cue'
        ? { track: hit.row.track, cue: hit.cue }
        : { track: hit.row.track, point: hit.point, channel: hit.channel });
    } else if (!additive) {
      picked = new Set();
      setSelected(picked);
      setFocus(null);
    }

    if (hit.k === 'cue') {
      dragCues(e, geom, hit, picked, key);
    } else if (hit.k === 'point') {
      dragPoints(e, geom, hit, picked, key);
    } else {
      rubberBand(e, geom, additive, picked);
    }
  }, [context, selected, view, history, score, time, fps]);

  /* --- gestures ------------------------------------------------------- */

  function scrub(geom: Geometry, at: number) {
    onSeek(at);
    const move = (ev: PointerEvent) => {
      onSeek(view.fromX(ev.clientX - geom.rect.left, geom.width));
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }

  function dragCues(
    e: React.PointerEvent, geom: Geometry,
    hit: Extract<Hit, { k: 'cue' }>, picked: Selected, key: string,
  ) {
    const track = score.tracks[hit.row.track];
    const startX = e.clientX;

    /* Trimming acts on the one you grabbed; moving acts on the whole
     * selection, which is what makes multi-select worth having. */
    const trimming = hit.part !== 'body';
    const cues = trimming
      ? [hit.cue]
      : (track.cues ?? []).filter((c) => picked.has(c) || c === hit.cue);
    const before = cues.map((c) => ({ cue: c, t: c.t, d: c.duration ?? 0 }));
    const targets = snapTargets(score, time, new Set(cues));
    let moved = false;

    const move = (ev: PointerEvent) => {
      const dxPx = ev.clientX - startX;
      if (!moved && Math.abs(dxPx) < SLOP) return;
      moved = true;
      const dt = (dxPx / Math.max(1, geom.width)) * view.span;
      const free = ev.altKey;

      if (trimming) {
        const b = before[0];
        if (hit.part === 'end') {
          const s = snap(b.t + b.d + dt, view, geom.width, targets, fps, !free);
          setGuide(s.to);
          history.run(resizeCues([{ track, cue: b.cue, from: b.d, to: s.t - b.cue.t }]), key);
        } else {
          const s = snap(b.t + dt, view, geom.width, targets, fps, !free);
          setGuide(s.to);
          const start = clamp(s.t, 0, b.t + b.d - 0.02);
          history.run(moveCues([{ track, cue: b.cue, from: b.t, to: start }]), key);
          history.run(resizeCues([{ track, cue: b.cue, from: b.d, to: b.t + b.d - start }]), key + 'r');
        }
      } else {
        /* Snap the event the pointer is on, then shift the rest by the same
         * amount — so a selection keeps its internal spacing instead of every
         * member snapping independently and collapsing together. */
        const lead = before.find((b) => b.cue === hit.cue)!;
        const s = snap(lead.t + dt, view, geom.width, targets, fps, !free);
        setGuide(s.to);
        const shift = s.t - lead.t;
        history.run(moveCues(before.map((b) => ({
          track, cue: b.cue, from: b.t, to: clamp(b.t + shift, 0, score.duration),
        }))), key);
      }
      changed();
    };

    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      history.seal();
      setGuide(null);
      if (moved) changed();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }

  function dragPoints(
    e: React.PointerEvent, geom: Geometry,
    hit: Extract<Hit, { k: 'point' }>, picked: Selected, key: string,
  ) {
    const track = score.tracks[hit.row.track];
    const channel = hit.channel;
    const startX = e.clientX;
    const startY = e.clientY;
    const points = (track.points ?? []).filter((p) => picked.has(p) || p === hit.point);
    const before = points.map((p) => ({
      point: p, t: p.t, v: p.value[channel] ?? 0,
    }));
    const targets = snapTargets(score, time, new Set(points));
    let moved = false;

    const move = (ev: PointerEvent) => {
      const dxPx = ev.clientX - startX;
      const dyPx = ev.clientY - startY;
      if (!moved && Math.abs(dxPx) < SLOP && Math.abs(dyPx) < SLOP) return;
      moved = true;

      const dt = (dxPx / Math.max(1, geom.width)) * view.span;
      /* Value is geared to the lane's height, so a full-height drag is a full
       * swing whatever the row is. Shift constrains to one axis, which is the
       * only way to change a value without also nudging its time. */
      const dv = -dyPx / Math.max(1, hit.row.h - 6);
      const lockTime = ev.shiftKey;
      const lead = before.find((b) => b.point === hit.point)!;
      const s = lockTime
        ? { t: lead.t, to: null }
        : snap(lead.t + dt, view, geom.width, targets, fps, !ev.altKey);
      setGuide(s.to);
      const shift = s.t - lead.t;

      history.run(movePoints(before.map((b) => ({
        track, point: b.point, channel,
        fromT: b.t, toT: clamp(b.t + shift, 0, score.duration),
        fromV: b.v, toV: clamp01(b.v + dv),
      }))), key);
      changed();
    };

    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      history.seal();
      setGuide(null);
      if (moved) changed();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }

  function rubberBand(e: React.PointerEvent, geom: Geometry, additive: boolean, base: Selected) {
    const x1 = e.clientX - geom.rect.left;
    const y1 = e.clientY - geom.rect.top;
    let moved = false;

    const move = (ev: PointerEvent) => {
      const x2 = ev.clientX - geom.rect.left;
      const y2 = ev.clientY - geom.rect.top;
      if (!moved && Math.abs(x2 - x1) < SLOP && Math.abs(y2 - y1) < SLOP) return;
      moved = true;
      setBand({ x1, y1, x2, y2 });
      const found = hitRange(context(geom), x1, y1, x2, y2);
      const next = additive ? new Set(base) : new Set<Cue | Point>();
      for (const c of found.cues) next.add(c.cue);
      for (const p of found.points) next.add(p.point);
      setSelected(next);
    };
    const up = (ev: PointerEvent) => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      setBand(null);
      /* A press that never became a drag is a click on empty lane, and the
       * useful thing for that to do is move the playhead. */
      if (!moved) onSeek(view.fromX(ev.clientX - geom.rect.left, geom.width));
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }

  /* --- double click: add and remove ----------------------------------- */

  const onDoubleClick = useCallback((e: React.MouseEvent, geom: Geometry) => {
    const x = e.clientX - geom.rect.left;
    const y = e.clientY - geom.rect.top;
    const hit = hitTest(context(geom), x, y);

    if (hit.k === 'point') {
      history.run(removePoints(score.tracks[hit.row.track], [hit.point]));
      history.seal();
      setSelected(new Set());
      changed();
      return;
    }
    if (hit.k === 'cue') {
      history.run(removeCues(score.tracks[hit.row.track], [hit.cue]));
      history.seal();
      setSelected(new Set());
      changed();
      return;
    }
    if (hit.k !== 'lane' || hit.row.draw !== 'curve' || !hit.row.channel) return;

    /* Add a point where the click was, at the clicked value for the channel
     * clicked in, and at whatever the curve is already worth for the others —
     * so inserting a control point in red does not kink green and blue. */
    const track = score.tracks[hit.row.track];
    const channels = channelsOf(track, rig);
    const t = round3(clamp(hit.t, 0, score.duration));
    const localY = y - geom.rulerH - hit.row.y;
    const v = clamp01(1 - (localY - 3) / Math.max(1, hit.row.h - 6));

    const value = valueAt(track.points ?? [], t, channels, isHSI(track));
    value[hit.row.channel] = round3(v);

    const adding: Point[] = [{ t, value }];
    if (!(track.points ?? []).length) {
      /* Two or none, never one. The first click on an empty track lays a short
       * flat segment to shape rather than an orphan the score would refuse. */
      const gap = Math.max(1, Math.min(score.duration - t, score.duration * 0.08));
      adding.push({ t: round3(Math.min(score.duration, t + gap)), value: { ...value } });
    }
    history.run(insertPoints(track, adding));
    history.seal();
    changed();
  }, [context, history, score, rig, changed]);

  /* --- selection commands --------------------------------------------- */

  const deleteSelection = useCallback(() => {
    if (!selected.size) return;
    for (const track of score.tracks ?? []) {
      const cues = (track.cues ?? []).filter((c) => selected.has(c));
      if (cues.length) history.run(removeCues(track, cues));
      const points = (track.points ?? []).filter((p) => selected.has(p));
      if (points.length) history.run(removePoints(track, points));
    }
    history.seal();
    setSelected(new Set());
    changed();
  }, [selected, score, history, changed]);

  const clearSelection = useCallback(() => setSelected(new Set()), []);

  const selectAll = useCallback(() => {
    const all = new Set<Cue | Point>();
    for (const track of score.tracks ?? []) {
      for (const c of track.cues ?? []) all.add(c);
      for (const p of track.points ?? []) all.add(p);
    }
    setSelected(all);
  }, [score]);

  /* Right-click selects what is under it first — unless it is already part of
   * the selection, in which case the selection stands. So a menu never acts on
   * something other than what was pointed at, and never destroys a
   * multi-selection you right-clicked in order to use. */
  const onContextMenu = useCallback((e: React.MouseEvent, geom: Geometry) => {
    e.preventDefault();
    const x = e.clientX - geom.rect.left;
    const y = e.clientY - geom.rect.top;
    const hit = hitTest(context(geom), x, y);
    if (hit.k === 'cue' && !selected.has(hit.cue)) setSelected(new Set([hit.cue]));
    if (hit.k === 'point' && !selected.has(hit.point)) setSelected(new Set([hit.point]));
    setMenu({ x: e.clientX, y: e.clientY, hit });
  }, [context, selected]);

  const closeMenu = useCallback(() => setMenu(null), []);
  const clearFocus = useCallback(() => setFocus(null), []);

  return {
    selected, band, guide, cursor, version, menu, focus, clearFocus,
    onPointerDown, onPointerMove, onDoubleClick, onContextMenu, closeMenu,
    deleteSelection, clearSelection, selectAll, setSelected,
  };
}

/** Whether a cue is a span, re-exported so the component need not import score. */
export { isSpan, cueEnd };
