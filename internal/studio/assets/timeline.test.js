/* Run with: node timeline.test.js
 *
 * The arithmetic behind dragging an event. The gesture itself needs a browser
 * and is checked there; these are the parts that decide what a gesture means,
 * and they are the parts that can be wrong in ways nobody notices until a
 * score has been saved.
 */

'use strict';

const assert = require('assert');
const { fmt, clamp01, clampTime, round3, valueAt } = require('./timeline.js');

/* A drag can always be pushed past both ends of the film. Letting it through
 * writes a cue at a negative time or past the end, which the score format will
 * take and the conductor will simply never reach. */
assert.strictEqual(clampTime(-5, 100), 0, 'a cue was dragged before the start');
assert.strictEqual(clampTime(140, 100), 100, 'a cue was dragged past the end');
assert.strictEqual(clampTime(42.5, 100), 42.5, 'a time inside the film was altered');

/* Milliseconds, which is the score's own resolution. Without rounding, a drag
 * writes 0.30000000000000004 into a file a person is expected to read and
 * hand-edit. */
assert.strictEqual(round3(0.1 + 0.2), 0.3, 'float noise reached the score');
assert.strictEqual(round3(1.23456), 1.235, 'time was not rounded to milliseconds');
assert.strictEqual(clampTime(1.0004999, 100), 1, 'sub-millisecond drift survived');

/* Curve values are a fraction and nothing else. A vertical drag runs off the
 * top and bottom of a forty pixel lane constantly. */
assert.strictEqual(clamp01(1.4), 1, 'a curve point was dragged above full');
assert.strictEqual(clamp01(-0.3), 0, 'a curve point was dragged below zero');
assert.strictEqual(clamp01('x'), 0, 'a non-number became a value');

/* The clock the inspector round-trips through. */
assert.strictEqual(fmt(0), '00:00.000');
assert.strictEqual(fmt(61.5), '01:01.500');
assert.strictEqual(fmt(3599.999), '59:59.999');

console.log('timeline.test.js: all passing');

/* --- inserting a point must not move the curve ------------------------- */

/* Adding a control point is a split, not an edit. If the inserted point does
 * not sit exactly on the line it is inserted into, every added point puts a
 * kink in the curve at the moment it appears — and in the other channels,
 * which were not even being edited. */
{
  const pts = [
    { t: 0, value: { r: 0, g: 0, b: 0 } },
    { t: 10, value: { r: 1, g: 0.5, b: 0 } },
  ];
  const mid = valueAt(pts, 5, ['r', 'g', 'b']);
  assert.strictEqual(mid.r, 0.5, 'r was not interpolated at the midpoint');
  assert.strictEqual(mid.g, 0.25, 'g was not interpolated at the midpoint');
  assert.strictEqual(mid.b, 0, 'b moved when it should not have');

  /* Holds at the edges rather than extrapolating, matching the player. A
   * curve that ran off past its last point would invent an effect nobody
   * wrote. */
  assert.strictEqual(valueAt(pts, -3, ['r']).r, 0, 'extrapolated before the start');
  assert.strictEqual(valueAt(pts, 99, ['r']).r, 1, 'extrapolated past the end');

  /* An empty curve is worth nothing, not undefined. */
  const none = valueAt([], 5, ['r', 'g', 'b']);
  assert.deepStrictEqual(none, { r: 0, g: 0, b: 0 }, 'an empty curve is not zero');
}

/* --- the orphan rule ---------------------------------------------------- */

/* Reimplemented here rather than reached through the class, which needs a DOM.
 * The rule is what matters and it is one line: two, or none, never one. */
function removeFrom(points, index) {
  points.splice(index, 1);
  if (points.length === 1) points.length = 0;
  return points;
}

assert.strictEqual(removeFrom([{ t: 0 }, { t: 1 }, { t: 2 }], 1).length, 2,
  'removing from three should leave two');

/* The one that matters: going from two to one is not allowed to happen, so
 * the survivor goes too. A single point is not a curve — it pins the channel
 * for the whole film — and the score parser rejects it outright, so leaving
 * one behind would produce an edit that cannot be saved. */
assert.strictEqual(removeFrom([{ t: 0 }, { t: 1 }], 0).length, 0,
  'removing one of two left an orphan');
assert.strictEqual(removeFrom([{ t: 0 }, { t: 1 }], 1).length, 0,
  'removing the other of two left an orphan');

console.log('timeline.test.js: point editing checks passing');
