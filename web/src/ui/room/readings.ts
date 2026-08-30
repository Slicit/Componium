/* How the room reads a cue.
 *
 * The old studio had these on the global scope so its two room views could
 * share them. They are a module now, and the reason for sharing them has not
 * changed: two views that disagree about what "muted" or "forced" means tell
 * you two different things about the same score, and the cheapest way to stop
 * that is one function rather than two that look alike today.
 */

import { clamp01, type Seconds } from '../../core/time';
import { colourOf, type Params } from '../../core/score';
import type { SceneState } from '../../core/state';

export interface Reading {
  level: number;
  params: Params;
  muted: boolean;
  forced: boolean;
}

/**
 * What one device is doing.
 *
 * A forced level overrides the score outright — that is the preview control,
 * answering "what does 40% of that fan look like" without needing a cue at the
 * playhead. Mute still wins, because mute is how you take something out of the
 * picture and it would be confusing for one control to defeat the other.
 */
export function deviceState(
  state: SceneState,
  id: string,
  muted?: ReadonlySet<string>,
  forced?: ReadonlyMap<string, number>,
): Reading {
  const s = state[id];
  const off = muted?.has(id) ?? false;
  const override = forced?.has(id) ? forced.get(id)! : null;

  if (override !== null) {
    return {
      level: off ? 0 : override,
      /* Borrow the live cue's parameters when there is one, so forcing a light
       * during a red scene previews red rather than white. With no cue there
       * is nothing to borrow and plain white at the forced level is the honest
       * default. */
      params: (s?.active && s.params) ? s.params : { intensity: 1 },
      muted: off,
      forced: true,
    };
  }

  return {
    level: (!off && s?.active) ? s.level : 0,
    params: s?.params ?? {},
    muted: off,
    forced: false,
  };
}

export interface Pose {
  surge: number; sway: number; heave: number;
  roll: number; pitch: number; yaw: number;
}

/**
 * Where the seat is.
 *
 * Reads whichever axes the score actually carries. A three axis score has
 * heave, roll and pitch and nothing else, and the missing three simply stay at
 * zero — the room does not need to know which kind it was given.
 */
export function seatPose(
  state: SceneState,
  forced?: ReadonlyMap<string, number>,
  nowMs?: number,
): Pose {
  const n = (v: unknown) => (typeof v === 'number' && isFinite(v) ? v : 0);

  /* A forced platform has no pose to read, because there is no cue driving
   * one. A static displacement would be the easy answer and a poor one: a
   * chair sitting still one centimetre higher says nothing about what 40% of a
   * motion rig feels like. So force generates a slow wallow instead, on
   * incommensurate periods so it never settles into a loop. This is the one
   * place in the room where movement is invented rather than reported. */
  const id = forcedMotionID(forced);
  if (id !== null) {
    const level = forced!.get(id)!;
    const t = n(nowMs) / 1000;
    return {
      surge: 0, sway: 0, yaw: 0,
      heave: level * 0.34 * Math.sin(t * 2.3),
      roll: level * 0.20 * Math.sin(t * 1.7),
      pitch: level * 0.14 * Math.sin(t * 3.1),
    };
  }

  const motion = state['motion.platform'] ?? findKind(state, 'motion');
  const p = motion?.params ?? {};
  return {
    surge: n(p.surge), sway: n(p.sway), heave: n(p.heave),
    roll: n(p.roll), pitch: n(p.pitch), yaw: n(p.yaw),
  };
}

function findKind(state: SceneState, kind: string) {
  for (const id of Object.keys(state)) {
    if (id.indexOf(kind + '.') === 0) return state[id];
  }
  return null;
}

/** Motion and shake both move the audience, so both steer the seat. */
function forcedMotionID(forced?: ReadonlyMap<string, number>): string | null {
  if (!forced?.size) return null;
  for (const [id, level] of forced) {
    if (level > 0 && (id.startsWith('motion.') || id.startsWith('shake.'))) return id;
  }
  return null;
}

/** A CSS colour for a parameter set, or black when it describes none. */
export function cssColour(params: Params | undefined): string {
  const c = colourOf(params);
  if (c) return c;
  const i = Math.round(clamp01(params?.intensity ?? params?.i ?? 0) * 255);
  return `rgb(${i}, ${i}, ${i})`;
}

export type { Seconds };
