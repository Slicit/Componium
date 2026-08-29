/* Run with: node timeline.test.js
 *
 * The arithmetic behind dragging an event. The gesture itself needs a browser
 * and is checked there; these are the parts that decide what a gesture means,
 * and they are the parts that can be wrong in ways nobody notices until a
 * score has been saved.
 */

'use strict';

const assert = require('assert');
const { fmt, clamp01, clampTime, round3 } = require('./timeline.js');

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
