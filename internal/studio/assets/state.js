/* What every device is doing at a given moment.
 *
 * This mirrors the conductor: curve tracks interpolate, cue tracks are spans
 * that begin at their time and last for their duration. It is deliberately a
 * second implementation rather than an API call, because the room redraws on
 * every animation frame and a round trip per frame would be absurd.
 *
 * Being a second implementation, it can drift from the Go one, so the rules it
 * must match are stated here and tested:
 *
 *   A curve holds at its endpoints rather than extrapolating.
 *   Linear interpolates between points; step holds the earlier value.
 *   A channel that stops being mentioned holds its last value.
 *   A cue with no duration is momentary.
 */

'use strict';

/* How long a momentary cue is shown lit. Nothing in the score says, because
 * nothing in the score needs to: a flash is an instant. The room still has to
 * draw it for long enough to be seen. */
const MOMENTARY_SECONDS = 0.18;

function curveValueAt(track, t) {
  const points = track.points || [];
  if (points.length === 0) return null;
  if (t <= points[0].t) return Object.assign({}, points[0].value);
  const last = points[points.length - 1];
  if (t >= last.t) return Object.assign({}, last.value);

  let hi = 0;
  for (let i = 0; i < points.length; i++) {
    if (points[i].t > t) { hi = i; break; }
  }
  const a = points[hi - 1];
  const b = points[hi];

  if (track.interpolation === 'step') return Object.assign({}, a.value);

  const span = b.t - a.t;
  if (span <= 0) return Object.assign({}, b.value);
  const f = (t - a.t) / span;

  const out = {};
  for (const k of Object.keys(a.value)) {
    /* A channel that stops being mentioned holds its last value rather than
     * snapping to zero, which would be a different effect entirely. */
    out[k] = (k in b.value) ? a.value[k] + (b.value[k] - a.value[k]) * f : a.value[k];
  }
  for (const k of Object.keys(b.value)) {
    if (!(k in a.value)) out[k] = b.value[k];
  }
  return out;
}

function activeCue(track, t) {
  for (const cue of (track.cues || [])) {
    const length = cue.duration > 0 ? cue.duration : MOMENTARY_SECONDS;
    if (t >= cue.t && t < cue.t + length) return cue;
  }
  return null;
}

/* Evaluate the whole score. Returns a map of instrument id to its state. */
function evaluate(score, t) {
  const out = {};
  for (const track of (score.tracks || [])) {
    const id = track.instrument;
    if (track.type === 'curve') {
      const value = curveValueAt(track, t);
      if (value) {
        out[id] = { id: id, active: true, params: value, action: 'set', level: levelOf(value) };
      }
      continue;
    }
    const cue = activeCue(track, t);
    if (cue) {
      out[id] = {
        id: id, active: true, params: cue.params || {},
        action: cue.action, level: levelOf(cue.params || {}),
        source: cue.source || '',
      };
    } else if (!(id in out)) {
      out[id] = { id: id, active: false, params: {}, action: '', level: 0 };
    }
  }
  return out;
}

/* How hard something is being driven, for the room to scale a glow or a shake
 * by. Colour channels are averaged rather than maxed: a device showing deep
 * blue is not at full output just because one channel is. */
function levelOf(params) {
  const keys = Object.keys(params || {});
  if (keys.length === 0) return 0;
  const colour = ['r', 'g', 'b', 'w'].filter((k) => k in params);
  if (colour.length > 0) {
    let total = 0;
    for (const k of colour) total += Math.abs(params[k]);
    return Math.min(1, total / colour.length);
  }
  let peak = 0;
  for (const k of keys) peak = Math.max(peak, Math.abs(params[k]));
  return Math.min(1, peak);
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { evaluate, curveValueAt, activeCue, levelOf, MOMENTARY_SECONDS };
}
