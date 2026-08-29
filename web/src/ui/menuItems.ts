/* What the right-click menu offers, given what was clicked.
 *
 * Kept apart from the menu component so the decisions — which actions apply to
 * a span but not an instant, what "split" means when the playhead is elsewhere
 * — are readable in one place rather than tangled in markup.
 */

import type { MenuEntry } from './Menu';
import type { Hit } from '../core/hit';
import { History, removeCues, removePoints } from '../core/history';
import {
  copy, duplicateCues, paste, scaleAmplitude, smoothPoints, splitCue, toggleSpan,
  type Clip,
} from '../core/edits';
import { cueEnd, isSpan, type Cue, type Point, type Rig, type Score } from '../core/score';
import { durationLabel, timecode } from '../core/time';

export interface MenuContext {
  hit: Hit;
  score: Score;
  rig: Rig | null;
  history: History;
  time: number;
  fps: number;
  selected: ReadonlySet<Cue | Point>;
  clipboard: Clip | null;
  setClipboard: (c: Clip | null) => void;
  setSelected: (s: Set<Cue | Point>) => void;
  changed: () => void;
  seek: (t: number) => void;
  zoomTo: (a: number, b: number) => void;
  toggleCollapse: (instrument: string) => void;
}

export function menuFor(ctx: MenuContext): MenuEntry[] {
  const { hit } = ctx;
  switch (hit.k) {
    case 'cue': return cueMenu(ctx, hit);
    case 'point': return pointMenu(ctx, hit);
    case 'lane': return laneMenu(ctx, hit);
    case 'ruler': return rulerMenu(ctx, hit);
    default: return [];
  }
}

function run(ctx: MenuContext, cmd: ReturnType<typeof splitCue>) {
  if (!cmd) return;
  ctx.history.run(cmd);
  ctx.history.seal();
  ctx.changed();
}

function cueMenu(ctx: MenuContext, hit: Extract<Hit, { k: 'cue' }>): MenuEntry[] {
  const track = ctx.score.tracks[hit.row.track];
  const cue = hit.cue;
  const selected = [...ctx.selected].filter((s): s is Cue => (track.cues ?? []).includes(s as Cue));
  const acting = selected.includes(cue) && selected.length > 1 ? selected : [cue];
  const inside = ctx.time > cue.t && ctx.time < cueEnd(cue);

  return [
    {
      label: acting.length > 1 ? `${acting.length} events` : `${cue.action} at ${timecode(cue.t, ctx.fps)}`,
      why: isSpan(cue) ? `lasts ${durationLabel(cue.duration ?? 0, ctx.fps)}` : 'an instant',
    },
    { separator: true },
    {
      label: 'Split at playhead',
      key: 'S',
      /* Disabled with a reason rather than hidden: this is the one action
       * whose absence would be puzzling, because whether it applies depends on
       * where the playhead is rather than on what was clicked. */
      why: !isSpan(cue) ? 'only a span can be split'
        : !inside ? 'move the playhead inside this event first' : undefined,
      run: () => run(ctx, splitCue(track, cue, ctx.time)),
    },
    {
      label: 'Duplicate',
      key: '⌘D',
      run: () => run(ctx, duplicateCues(track, acting)),
    },
    {
      label: isSpan(cue) ? 'Make instant' : 'Make a span',
      run: () => run(ctx, toggleSpan(track, cue)),
    },
    { separator: true },
    {
      label: 'Copy', key: '⌘C',
      run: () => ctx.setClipboard(copy(ctx.score, new Set(acting))),
    },
    {
      label: 'Cut', key: '⌘X',
      run: () => {
        ctx.setClipboard(copy(ctx.score, new Set(acting)));
        run(ctx, removeCues(track, acting));
        ctx.setSelected(new Set());
      },
    },
    { separator: true },
    {
      label: 'Zoom to this',
      run: () => ctx.zoomTo(cue.t, cueEnd(cue)),
    },
    {
      label: 'Move playhead here',
      run: () => ctx.seek(cue.t),
    },
    { separator: true },
    {
      label: acting.length > 1 ? `Delete ${acting.length} events` : 'Delete',
      key: '⌫',
      danger: true,
      run: () => {
        run(ctx, removeCues(track, acting));
        ctx.setSelected(new Set());
      },
    },
  ];
}

function pointMenu(ctx: MenuContext, hit: Extract<Hit, { k: 'point' }>): MenuEntry[] {
  const track = ctx.score.tracks[hit.row.track];
  const point = hit.point;
  const selected = [...ctx.selected].filter((s): s is Point => (track.points ?? []).includes(s as Point));
  const acting = selected.includes(point) && selected.length > 1 ? selected : [point];
  const value = point.value?.[hit.channel];
  const remaining = (track.points ?? []).length - acting.length;

  return [
    {
      label: acting.length > 1 ? `${acting.length} points` : `${hit.channel} = ${value?.toFixed(3)}`,
      why: timecode(point.t, ctx.fps),
    },
    { separator: true },
    {
      label: 'Smooth',
      why: acting.every((p) => {
        const all = track.points ?? [];
        const i = all.indexOf(p);
        return i <= 0 || i >= all.length - 1;
      }) ? 'an end point has no neighbours to average with' : undefined,
      run: () => run(ctx, smoothPoints(track, acting)),
    },
    {
      label: 'Stronger',
      run: () => run(ctx, scaleAmplitude(ctx.score, new Set(acting), 1.25)),
    },
    {
      label: 'Softer',
      run: () => run(ctx, scaleAmplitude(ctx.score, new Set(acting), 0.8)),
    },
    { separator: true },
    {
      label: 'Copy', key: '⌘C',
      run: () => ctx.setClipboard(copy(ctx.score, new Set(acting))),
    },
    { separator: true },
    {
      label: acting.length > 1 ? `Delete ${acting.length} points` : 'Delete point',
      key: '⌫',
      danger: true,
      /* The orphan rule made visible before it happens rather than after: a
       * curve is two points or none, so a deletion that would leave one takes
       * the survivor too. Being told that in the menu is better than watching
       * two disappear. */
      why: remaining === 1 ? undefined : undefined,
      run: () => {
        run(ctx, removePoints(track, acting));
        ctx.setSelected(new Set());
      },
    },
    ...(remaining === 1 ? [{
      label: '…which empties the track',
      why: 'a curve needs two points or none, so the last one goes too',
    } as MenuEntry] : []),
  ];
}

function laneMenu(ctx: MenuContext, hit: Extract<Hit, { k: 'lane' }>): MenuEntry[] {
  const track = ctx.score.tracks[hit.row.track];
  const canPaste = ctx.clipboard
    && (track.type === 'curve' ? ctx.clipboard.points.length : ctx.clipboard.cues.length);

  return [
    { label: track.instrument, why: timecode(hit.t, ctx.fps) },
    { separator: true },
    {
      label: 'Paste here',
      key: '⌘V',
      why: !ctx.clipboard ? 'nothing copied yet'
        : !canPaste ? `the clipboard holds ${track.type === 'curve' ? 'events, not points' : 'points, not events'}`
          : undefined,
      run: () => run(ctx, paste(ctx.clipboard!, track, hit.t, ctx.score, ctx.rig)),
    },
    { separator: true },
    {
      label: 'Select everything in this track',
      run: () => {
        const all = new Set<Cue | Point>();
        for (const c of track.cues ?? []) all.add(c);
        for (const p of track.points ?? []) all.add(p);
        ctx.setSelected(all);
      },
    },
    {
      label: ctx.score.tracks[hit.row.track].type === 'curve' ? 'Collapse or expand' : 'Collapse',
      run: () => ctx.toggleCollapse(track.instrument),
    },
    { separator: true },
    { label: 'Move playhead here', run: () => ctx.seek(hit.t) },
  ];
}

function rulerMenu(ctx: MenuContext, hit: Extract<Hit, { k: 'ruler' }>): MenuEntry[] {
  return [
    { label: timecode(hit.t, ctx.fps, { hours: true }) },
    { separator: true },
    { label: 'Move playhead here', run: () => ctx.seek(hit.t) },
    { label: 'Zoom to fit', key: 'F', run: () => ctx.zoomTo(0, ctx.score.duration) },
  ];
}
