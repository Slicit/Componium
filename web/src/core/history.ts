/* Edits, and taking them back.
 *
 * Every change to a score goes through a command, and every command knows how
 * to undo itself. Nothing anywhere else may reach in and set a `t`. That rule
 * is the whole reason this file exists: an editor without undo is not an
 * editor, because editing is trying things, and undo is only trustworthy if
 * there is exactly one way for the document to change.
 *
 * Commands hold *object references*, never indices. A score's cues and points
 * must stay sorted by time, so almost every edit re-sorts the array it touched
 * and every index into it goes stale immediately. Object identity survives a
 * sort; an index does not. This was the difference between an undo stack that
 * works and one that silently moves the wrong event.
 */

import { round3, type Seconds } from './time';
import type { Cue, Point, Score, Track } from './score';

export interface MoveCue { track: Track; cue: Cue; from: Seconds; to: Seconds }
export interface ResizeCue { track: Track; cue: Cue; from: Seconds; to: Seconds }
export interface MovePoint {
  track: Track;
  point: Point;
  fromT: Seconds; toT: Seconds;
  channel?: string;
  fromV?: number; toV?: number;
}

export type Command =
  | { k: 'batch'; label: string; cmds: Command[] }
  | { k: 'moveCues'; label: string; items: MoveCue[] }
  | { k: 'resizeCues'; label: string; items: ResizeCue[] }
  | { k: 'movePoints'; label: string; items: MovePoint[] }
  | { k: 'insertPoints'; label: string; track: Track; points: Point[] }
  | { k: 'removePoints'; label: string; track: Track; points: Point[] }
  | { k: 'insertCues'; label: string; track: Track; cues: Cue[] }
  | { k: 'removeCues'; label: string; track: Track; cues: Cue[] }
  | { k: 'addTrack'; label: string; score: Score; track: Track }
  | { k: 'removeTrack'; label: string; score: Score; track: Track; at: number };

/** Keep a track's events in time order, which the score format requires. */
export function normalise(track: Track): void {
  if (track.cues) track.cues.sort((a, b) => a.t - b.t);
  if (track.points) track.points.sort((a, b) => a.t - b.t);
}

/**
 * Which points must go along with the ones being removed.
 *
 * A curve is two points or none, never one: it holds its value before the
 * first point and after the last, so a single point pins the channel for the
 * whole film with nothing on the timeline to show it — and the score parser
 * rejects it outright, so leaving one behind produces an edit that cannot be
 * saved. Removing down to one therefore removes the other as well.
 */
export function withOrphans(track: Track, points: Point[]): Point[] {
  const all = track.points ?? [];
  const going = new Set(points);
  const left = all.filter((p) => !going.has(p));
  if (left.length === 1) return [...points, left[0]];
  return points;
}

export function apply(cmd: Command): void {
  switch (cmd.k) {
    case 'batch':
      for (const c of cmd.cmds) apply(c);
      break;
    case 'moveCues':
      for (const it of cmd.items) it.cue.t = it.to;
      for (const t of tracksOf(cmd.items)) normalise(t);
      break;
    case 'resizeCues':
      for (const it of cmd.items) it.cue.duration = it.to;
      break;
    case 'movePoints':
      for (const it of cmd.items) {
        it.point.t = it.toT;
        if (it.channel !== undefined && it.toV !== undefined) {
          it.point.value[it.channel] = it.toV;
        }
      }
      for (const t of tracksOf(cmd.items)) normalise(t);
      break;
    case 'insertPoints':
      cmd.track.points = [...(cmd.track.points ?? []), ...cmd.points];
      normalise(cmd.track);
      break;
    case 'removePoints': {
      const going = new Set(cmd.points);
      cmd.track.points = (cmd.track.points ?? []).filter((p) => !going.has(p));
      break;
    }
    case 'insertCues':
      cmd.track.cues = [...(cmd.track.cues ?? []), ...cmd.cues];
      normalise(cmd.track);
      break;
    case 'removeCues': {
      const going = new Set(cmd.cues);
      cmd.track.cues = (cmd.track.cues ?? []).filter((c) => !going.has(c));
      break;
    }
    case 'addTrack':
      cmd.score.tracks = [...(cmd.score.tracks ?? []), cmd.track];
      break;
    case 'removeTrack':
      cmd.score.tracks = (cmd.score.tracks ?? []).filter((t) => t !== cmd.track);
      break;
  }
}

export function revert(cmd: Command): void {
  switch (cmd.k) {
    case 'batch':
      /* Backwards. A batch that removed a span and inserted two halves must
       * put the halves back before restoring the original, or the two undo
       * steps fight over the same array. */
      for (let i = cmd.cmds.length - 1; i >= 0; i--) revert(cmd.cmds[i]);
      break;
    case 'moveCues':
      for (const it of cmd.items) it.cue.t = it.from;
      for (const t of tracksOf(cmd.items)) normalise(t);
      break;
    case 'resizeCues':
      for (const it of cmd.items) it.cue.duration = it.from;
      break;
    case 'movePoints':
      for (const it of cmd.items) {
        it.point.t = it.fromT;
        if (it.channel !== undefined && it.fromV !== undefined) {
          it.point.value[it.channel] = it.fromV;
        }
      }
      for (const t of tracksOf(cmd.items)) normalise(t);
      break;
    case 'insertPoints':
      apply({ ...cmd, k: 'removePoints' });
      break;
    case 'removePoints':
      apply({ ...cmd, k: 'insertPoints' });
      break;
    case 'insertCues':
      apply({ ...cmd, k: 'removeCues' });
      break;
    case 'removeCues':
      apply({ ...cmd, k: 'insertCues' });
      break;
    case 'addTrack':
      apply({ ...cmd, k: 'removeTrack', at: 0 });
      break;
    case 'removeTrack':
      /* Back where it was, not on the end: a track reappearing somewhere else
       * after an undo is disorienting, and the order is what a person
       * arranged. */
      cmd.score.tracks = [...(cmd.score.tracks ?? [])];
      cmd.score.tracks.splice(cmd.at, 0, cmd.track);
      break;
  }
}

function tracksOf(items: Array<{ track: Track }>): Set<Track> {
  return new Set(items.map((i) => i.track));
}

/**
 * The undo stack.
 *
 * `coalesce` is what makes a drag one entry instead of two hundred. While a
 * gesture is in progress every update carries the same key, and each one
 * replaces the top of the stack rather than pushing — so the `from` recorded
 * at the start survives to the end and one undo puts the event back where it
 * started. Releasing the pointer clears the key, and the next gesture is a new
 * entry.
 */
export class History {
  private past: Command[] = [];
  private future: Command[] = [];
  private key: string | null = null;
  /** Bumped on every change, so a view can tell that something happened. */
  version = 0;

  get canUndo(): boolean { return this.past.length > 0; }
  get canRedo(): boolean { return this.future.length > 0; }
  get undoLabel(): string | null { return this.past[this.past.length - 1]?.label ?? null; }
  get redoLabel(): string | null { return this.future[this.future.length - 1]?.label ?? null; }
  get depth(): number { return this.past.length; }

  /** True once anything has been done that is not yet saved. */
  dirty = false;

  run(cmd: Command, coalesce?: string): void {
    apply(cmd);
    if (coalesce && this.key === coalesce && this.past.length) {
      /* Same gesture: replace the top, keeping its original `from` so undo
       * returns to where the drag began rather than to the last pixel. */
      const top = this.past[this.past.length - 1];
      this.past[this.past.length - 1] = mergeInto(top, cmd);
    } else {
      this.past.push(cmd);
      this.key = coalesce ?? null;
    }
    this.future.length = 0;
    this.version++;
    this.dirty = true;
  }

  /** End a gesture, so the next edit starts a new undo entry. */
  seal(): void { this.key = null; }

  undo(): boolean {
    const cmd = this.past.pop();
    if (!cmd) return false;
    revert(cmd);
    this.future.push(cmd);
    this.key = null;
    this.version++;
    this.dirty = true;
    return true;
  }

  redo(): boolean {
    const cmd = this.future.pop();
    if (!cmd) return false;
    apply(cmd);
    this.past.push(cmd);
    this.key = null;
    this.version++;
    this.dirty = true;
    return true;
  }

  saved(): void { this.dirty = false; }

  /**
   * Forget everything, for when a different score is opened.
   *
   * Not a convenience. Commands hold references to the tracks and cues they
   * act on, never indices, because every edit re-sorts the track — so an undo
   * stack built against one score would, after another is loaded, quietly
   * mutate objects belonging to a score nobody is looking at any more. The
   * version is bumped because the timeline recomputes its layout from it, and
   * the score it is drawing has just been replaced wholesale.
   */
  reset(): void {
    this.past.length = 0;
    this.future.length = 0;
    this.key = null;
    this.dirty = false;
    this.version++;
  }
}

/** Keep the older command's starting values and the newer one's end values. */
function mergeInto(top: Command, next: Command): Command {
  if (top.k !== next.k) return next;
  switch (top.k) {
    case 'moveCues':
    case 'resizeCues': {
      const items = (next as typeof top).items.map((n) => {
        const was = top.items.find((o) => o.cue === n.cue);
        return was ? { ...n, from: was.from } : n;
      });
      return { ...top, items } as Command;
    }
    case 'movePoints': {
      const items = (next as typeof top).items.map((n) => {
        const was = top.items.find((o) => o.point === n.point && o.channel === n.channel);
        return was ? { ...n, fromT: was.fromT, fromV: was.fromV } : n;
      });
      return { ...top, items } as Command;
    }
    default:
      return next;
  }
}

/* --- constructors ------------------------------------------------------- */

/** Several edits that undo as one. */
export function batch(label: string, cmds: Command[]): Command {
  return { k: 'batch', label, cmds };
}

export function moveCues(items: MoveCue[]): Command {
  return {
    k: 'moveCues',
    label: items.length > 1 ? `Move ${items.length} events` : 'Move event',
    items: items.map((i) => ({ ...i, to: round3(i.to) })),
  };
}

/**
 * Change how long spans last.
 *
 * `min` is the floor a drag may not go below, so trimming cannot collapse a
 * span into something that can never fire. Deliberately turning a span into an
 * instant passes zero, because that is a different intent from a drag that
 * went too far and should not be silently prevented.
 */
export function resizeCues(items: ResizeCue[], min = 0.02): Command {
  return {
    k: 'resizeCues',
    label: 'Change length',
    items: items.map((i) => ({ ...i, to: round3(Math.max(min, i.to)) })),
  };
}

export function movePoints(items: MovePoint[]): Command {
  return {
    k: 'movePoints',
    label: items.length > 1 ? `Move ${items.length} points` : 'Move point',
    items: items.map((i) => ({
      ...i,
      toT: round3(i.toT),
      toV: i.toV === undefined ? undefined : round3(i.toV),
    })),
  };
}

export function insertPoints(track: Track, points: Point[]): Command {
  return { k: 'insertPoints', label: 'Add point', track, points };
}

export function removePoints(track: Track, points: Point[]): Command {
  const all = withOrphans(track, points);
  return {
    k: 'removePoints',
    label: all.length > points.length
      /* Said out loud in the undo label, because removing one point and
       * watching two disappear is alarming if you do not know the rule. */
      ? 'Remove point (and its partner)'
      : all.length > 1 ? `Remove ${all.length} points` : 'Remove point',
    track,
    points: all,
  };
}

export function insertCues(track: Track, cues: Cue[]): Command {
  return { k: 'insertCues', label: 'Add event', track, cues };
}

export function removeCues(track: Track, cues: Cue[]): Command {
  return {
    k: 'removeCues',
    label: cues.length > 1 ? `Remove ${cues.length} events` : 'Remove event',
    track,
    cues,
  };
}
