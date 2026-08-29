/* The operations a menu or a keystroke invokes.
 *
 * Each one returns a command rather than performing an edit, so everything
 * still goes through the history and everything is one undo. They live here
 * rather than in the component for the usual reason: this is where the rules
 * are, and rules are testable in node.
 */

import { batch, insertCues, insertPoints, movePoints, moveCues, removeCues, resizeCues, type Command } from './history';
import { clamp, clamp01, round3, type Seconds } from './time';
import { cueEnd, isHSI, isSpan, valueAt, channelsOf, type Cue, type Instrument, type Point, type Rig, type Score, type Track } from './score';

/** The shortest a split can leave either half. Below this it is not a span. */
const MIN_PIECE = 0.04;

/**
 * Cut a span in two at a moment.
 *
 * The two halves keep the original's action and parameters, because a split is
 * a change of shape and not of intent — an operator splitting a gust wants two
 * gusts, and would be surprised to find the second one silent.
 *
 * Returns null when the cut is not inside the span, rather than producing a
 * zero length piece. A caller that offers "split" on an event the playhead is
 * not over should be disabling the menu item, and this is the backstop.
 */
export function splitCue(track: Track, cue: Cue, at: Seconds): Command | null {
  if (!isSpan(cue)) return null;
  const end = cueEnd(cue);
  if (at <= cue.t + MIN_PIECE || at >= end - MIN_PIECE) return null;

  const left: Cue = { ...cue, params: { ...cue.params }, t: cue.t, duration: round3(at - cue.t) };
  const right: Cue = { ...cue, params: { ...cue.params }, t: round3(at), duration: round3(end - at) };
  return batch('Split event', [
    removeCues(track, [cue]),
    insertCues(track, [left, right]),
  ]);
}

/**
 * Copy events forward by their own length, the way an editor duplicates a clip.
 *
 * Offsetting by the selection's own span rather than by a fixed amount means
 * the copy lands immediately after the original with no overlap and no gap,
 * which is almost always what was wanted and is otherwise a fiddly drag.
 */
export function duplicateCues(track: Track, cues: Cue[]): Command | null {
  if (!cues.length) return null;
  const start = Math.min(...cues.map((c) => c.t));
  const finish = Math.max(...cues.map(cueEnd));
  const shift = Math.max(finish - start, MIN_PIECE);
  const copies = cues.map((c) => ({
    ...c, params: { ...c.params }, t: round3(c.t + shift),
  }));
  return batch(cues.length > 1 ? `Duplicate ${cues.length} events` : 'Duplicate event',
    [insertCues(track, copies)]);
}

/** Move a selection by a fixed amount of time — the nudge keys. */
export function nudge(
  score: Score, selected: ReadonlySet<Cue | Point>, dt: Seconds,
): Command | null {
  const cmds: Command[] = [];
  for (const track of score.tracks ?? []) {
    const cues = (track.cues ?? []).filter((c) => selected.has(c));
    if (cues.length) {
      cmds.push(moveCues(cues.map((cue) => ({
        track, cue, from: cue.t, to: clamp(cue.t + dt, 0, score.duration),
      }))));
    }
    const points = (track.points ?? []).filter((p) => selected.has(p));
    if (points.length) {
      cmds.push(movePoints(points.map((point) => ({
        track, point, fromT: point.t, toT: clamp(point.t + dt, 0, score.duration),
      }))));
    }
  }
  if (!cmds.length) return null;
  return batch(dt < 0 ? 'Nudge back' : 'Nudge forward', cmds);
}

/**
 * Scale how hard a selection is, keeping its timing.
 *
 * Multiplicative rather than additive, so it behaves like a fader: the shape
 * of a passage survives and only its force changes. Adding a constant would
 * flatten the difference between the loudest and quietest moments, which is
 * the thing worth preserving.
 */
export function scaleAmplitude(
  score: Score, selected: ReadonlySet<Cue | Point>, factor: number,
): Command | null {
  const cmds: Command[] = [];
  for (const track of score.tracks ?? []) {
    const points = (track.points ?? []).filter((p) => selected.has(p));
    for (const point of points) {
      for (const channel of Object.keys(point.value ?? {})) {
        cmds.push(movePoints([{
          track, point, channel,
          fromT: point.t, toT: point.t,
          fromV: point.value[channel], toV: clamp01(point.value[channel] * factor),
        }]));
      }
    }
  }
  if (!cmds.length) return null;
  return batch(factor > 1 ? 'Stronger' : 'Softer', cmds);
}

/**
 * Smooth a run of points by averaging each with its neighbours.
 *
 * One pass only, and endpoints are left alone. Repeated smoothing walks a
 * curve towards a straight line, so making it an explicit action the operator
 * can press twice is honest about that; doing it in a loop until "smooth"
 * would quietly destroy the shape.
 */
export function smoothPoints(track: Track, points: Point[]): Command | null {
  const all = track.points ?? [];
  if (all.length < 3 || points.length < 1) return null;
  const chosen = new Set(points);
  const cmds: Command[] = [];

  for (let i = 1; i < all.length - 1; i++) {
    const p = all[i];
    if (!chosen.has(p)) continue;
    for (const channel of Object.keys(p.value ?? {})) {
      const a = all[i - 1].value?.[channel];
      const b = all[i + 1].value?.[channel];
      if (typeof a !== 'number' || typeof b !== 'number') continue;
      const to = clamp01((a + p.value[channel] * 2 + b) / 4);
      cmds.push(movePoints([{
        track, point: p, channel,
        fromT: p.t, toT: p.t,
        fromV: p.value[channel], toV: to,
      }]));
    }
  }
  if (!cmds.length) return null;
  return batch('Smooth', cmds);
}

/** Turn a momentary cue into a span, or a span back into an instant. */
export function toggleSpan(track: Track, cue: Cue, defaultLength = 1): Command {
  return isSpan(cue)
    ? batch('Make instant', [resizeCues([{ track, cue, from: cue.duration ?? 0, to: 0 }], 0)])
    : batch('Make a span', [resizeCues([{ track, cue, from: 0, to: defaultLength }])]);
}

/* --- adding a track ----------------------------------------------------- */

/**
 * Instruments the rig has that the score says nothing about.
 *
 * The composer only writes tracks for effects it found something to drive, so
 * a film with no smoke in it produces a score with no smoke track — and there
 * was then no way to add one by hand, which meant a rig capability the
 * analysis had not used was simply unreachable.
 */
export function missingInstruments(score: Score, rig: Rig | null): Instrument[] {
  const have = new Set((score.tracks ?? []).map((t) => t.instrument));
  return (rig?.instruments ?? []).filter((i) => !have.has(i.id));
}

/**
 * Add an empty track for an instrument.
 *
 * A curve, always, and empty rather than seeded. Empty is a legal curve — it
 * is how the format says "this instrument does nothing" — so the track can be
 * added and then shaped by double clicking, instead of arriving with invented
 * points somebody has to delete first. A cue track would need an action name
 * per event that only its author can supply.
 */
export function addTrack(score: Score, instrument: Instrument): Command {
  const track: Track = {
    instrument: instrument.id,
    type: 'curve',
    points: [],
  };
  return {
    k: 'addTrack',
    label: 'Add ' + instrument.id,
    score,
    track,
  };
}

/* --- the clipboard ------------------------------------------------------ */

export interface Clip {
  /** Times are stored relative to the earliest thing copied. */
  cues: Array<{ t: Seconds; cue: Cue }>;
  points: Array<{ t: Seconds; point: Point }>;
  /** What it came from, so pasting into an unrelated track can be refused. */
  fromKind: string;
}

export function copy(score: Score, selected: ReadonlySet<Cue | Point>): Clip | null {
  const cues: Clip['cues'] = [];
  const points: Clip['points'] = [];
  let origin = Infinity;
  let fromKind = '';

  for (const track of score.tracks ?? []) {
    for (const cue of track.cues ?? []) {
      if (!selected.has(cue)) continue;
      cues.push({ t: cue.t, cue });
      origin = Math.min(origin, cue.t);
      fromKind = fromKind || track.instrument.split('.')[0];
    }
    for (const point of track.points ?? []) {
      if (!selected.has(point)) continue;
      points.push({ t: point.t, point });
      origin = Math.min(origin, point.t);
      fromKind = fromKind || track.instrument.split('.')[0];
    }
  }
  if (!cues.length && !points.length) return null;

  /* Deep copies, taken now. Holding the live objects would mean a later edit
   * to the original silently changed what the clipboard pastes. */
  return {
    fromKind,
    cues: cues.map((c) => ({
      t: c.t - origin,
      cue: { ...c.cue, params: { ...c.cue.params } },
    })),
    points: points.map((p) => ({
      t: p.t - origin,
      point: { t: p.point.t, value: { ...p.point.value } },
    })),
  };
}

/**
 * Paste at a time, into a track that can hold it.
 *
 * A cue clipboard cannot go into a curve track and vice versa — the score
 * format forbids a track holding both, so pasting across would produce
 * something the parser rejects at save time, long after the mistake.
 */
export function paste(
  clip: Clip, track: Track, at: Seconds, score: Score, rig?: Rig | null,
): Command | null {
  const wantsCurve = track.type === 'curve';
  if (wantsCurve && !clip.points.length) return null;
  if (!wantsCurve && !clip.cues.length) return null;

  const cap = (t: Seconds) => round3(clamp(at + t, 0, score.duration));

  if (wantsCurve) {
    const channels = channelsOf(track, rig);
    const points: Point[] = clip.points.map((p) => {
      /* Keep whatever the destination already has for channels the copy does
       * not carry, so pasting a red curve into an RGB track does not zero
       * green and blue. */
      const base = valueAt(track.points ?? [], cap(p.t), channels, isHSI(track));
      return { t: cap(p.t), value: { ...base, ...p.point.value } };
    });
    return batch(`Paste ${points.length} points`, [insertPoints(track, points)]);
  }

  const cues: Cue[] = clip.cues.map((c) => ({
    ...c.cue, params: { ...c.cue.params }, t: cap(c.t),
  }));
  return batch(`Paste ${cues.length} events`, [insertCues(track, cues)]);
}

/** Remove a track and everything on it. */
export function removeTrack(score: Score, track: Track): Command {
  return {
    k: 'removeTrack',
    label: 'Remove ' + track.instrument,
    score,
    track,
    at: (score.tracks ?? []).indexOf(track),
  };
}
