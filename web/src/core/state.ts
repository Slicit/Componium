/* What every device is doing at a given moment.
 *
 * This mirrors the conductor: curve tracks interpolate, cue tracks are spans
 * that begin at their time and last for their duration. Deliberately a second
 * implementation rather than an API call, because the room redraws on every
 * animation frame and a round trip per frame would be absurd.
 *
 * Being a second implementation it can drift from the Go one, so the rules it
 * has to match are stated here and tested:
 *
 *   A curve holds at its endpoints rather than extrapolating.
 *   Linear interpolates between points; step holds the earlier value.
 *   A channel that stops being mentioned holds its last value.
 *   A cue with no duration is momentary.
 */

import { clamp01, type Seconds } from './time';
import {
  amplitudeOf, isHSI, valueAt, channelsOf,
  type Params, type Rig, type Score, type Track,
} from './score';

/**
 * How long a momentary cue is shown lit.
 *
 * Nothing in the score says, because nothing in the score needs to: a flash is
 * an instant, and the conductor sends it as one. The room still has to draw it
 * for long enough to be seen by a person rather than by an oscilloscope.
 */
export const MOMENTARY_SECONDS = 0.18;

export interface DeviceState {
  id: string;
  active: boolean;
  params: Params;
  action: string;
  level: number;
  source?: string;
}

export type SceneState = Record<string, DeviceState>;

export function activeCue(track: Track, t: Seconds) {
  for (const cue of track.cues ?? []) {
    const length = (cue.duration ?? 0) > 0 ? cue.duration! : MOMENTARY_SECONDS;
    if (t >= cue.t && t < cue.t + length) return cue;
  }
  return null;
}

/** Evaluate the whole score: instrument id to what it is doing. */
export function evaluate(score: Score, t: Seconds, rig?: Rig | null): SceneState {
  const out: SceneState = {};
  for (const track of score.tracks ?? []) {
    const id = track.instrument;

    if (track.type === 'curve') {
      const points = track.points ?? [];
      if (!points.length) {
        /* An empty curve is how the format says an instrument does nothing,
         * so it reports as idle rather than being left out — the room still
         * has to draw the device sitting there. */
        out[id] = { id, active: false, params: {}, action: '', level: 0 };
        continue;
      }
      const value = valueAt(points, t, channelsOf(track, rig), isHSI(track));
      out[id] = {
        id, active: true, params: value, action: 'set', level: levelOf(value),
      };
      continue;
    }

    const cue = activeCue(track, t);
    if (cue) {
      out[id] = {
        id, active: true, params: cue.params ?? {},
        action: cue.action, level: levelOf(cue.params ?? {}),
        source: cue.source ?? '',
      };
    } else if (!(id in out)) {
      out[id] = { id, active: false, params: {}, action: '', level: 0 };
    }
  }
  return out;
}

/**
 * How hard something is being driven, for the room to scale a glow or a shake
 * by.
 *
 * This is not the same question the timeline asks. There, amplitude answers
 * "how strong is this event" and a saturated red counts as a light at full,
 * because that is what it is. Here it answers "how much is this device
 * emitting", and a deep blue really is emitting less than white — one channel
 * lit rather than three. So colours are averaged here and taken at their peak
 * there, and the difference is deliberate rather than an oversight.
 */
export function levelOf(params: Params | undefined): number {
  const keys = Object.keys(params ?? {});
  if (!keys.length) return 0;
  const p = params!;

  /* Authored as hue: intensity already is the answer, and it is the same
   * number the duty cycle and the rest budget read. */
  if (typeof p.i === 'number' && (typeof p.h === 'number' || typeof p.s === 'number')) {
    return clamp01(p.i);
  }

  const colour = ['r', 'g', 'b', 'w'].filter((k) => k in p);
  if (colour.length) {
    let total = 0;
    for (const k of colour) total += Math.abs(p[k]);
    return clamp01(total / colour.length);
  }
  return clamp01(amplitudeOf(p) ?? 0);
}
