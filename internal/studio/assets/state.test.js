/* Run with: node state.test.js
 *
 * These rules must match internal/score and internal/conductor exactly. This
 * is a second implementation of the same semantics, so it is exactly the kind
 * of thing that drifts silently.
 */

'use strict';

const assert = require('assert');
const { evaluate, curveValueAt, activeCue, levelOf } = require('./state.js');

let failures = 0;
function check(name, fn) {
  try { fn(); console.log('ok   ' + name); }
  catch (e) { failures++; console.log('FAIL ' + name + ': ' + e.message); }
}

const curve = {
  instrument: 'light.ambient', type: 'curve', interpolation: 'linear',
  points: [
    { t: 0, value: { r: 0, g: 0, b: 0 } },
    { t: 10, value: { r: 1, g: 0.5, b: 0 } },
  ],
};

const cues = {
  instrument: 'wind.main', type: 'cue',
  cues: [
    { t: 5, action: 'gust', params: { intensity: 0.8 }, duration: 4 },
    { t: 20, action: 'flash', params: { intensity: 1 } },
  ],
};

check('curve interpolates linearly', () => {
  const v = curveValueAt(curve, 5);
  assert.strictEqual(v.r, 0.5);
  assert.strictEqual(v.g, 0.25);
});

check('curve holds at its endpoints rather than extrapolating', () => {
  assert.strictEqual(curveValueAt(curve, -100).r, 0);
  assert.strictEqual(curveValueAt(curve, 1e6).r, 1);
});

check('step interpolation holds the earlier value', () => {
  const stepped = Object.assign({}, curve, { interpolation: 'step' });
  assert.strictEqual(curveValueAt(stepped, 9.9).r, 0);
});

check('a channel that stops being mentioned holds', () => {
  const t = { type: 'curve', interpolation: 'linear', points: [
    { t: 0, value: { r: 0.4, g: 0.2 } },
    { t: 10, value: { r: 1.0 } },
  ] };
  assert.strictEqual(curveValueAt(t, 5).g, 0.2);
});

check('a span is active for its duration', () => {
  assert.strictEqual(activeCue(cues, 4.9), null);
  assert.ok(activeCue(cues, 5));
  assert.ok(activeCue(cues, 8.9));
  assert.strictEqual(activeCue(cues, 9.1), null);
});

check('a cue with no duration is momentary', () => {
  assert.ok(activeCue(cues, 20));
  assert.strictEqual(activeCue(cues, 20.5), null);
});

check('evaluate reports every instrument, active or not', () => {
  const state = evaluate({ tracks: [curve, cues] }, 12);
  assert.ok(state['light.ambient'].active);
  assert.strictEqual(state['wind.main'].active, false);
});

check('evaluate reports an active span with its action', () => {
  const state = evaluate({ tracks: [curve, cues] }, 6);
  assert.strictEqual(state['wind.main'].action, 'gust');
  assert.strictEqual(state['wind.main'].params.intensity, 0.8);
});

check('level averages colour rather than taking the peak', () => {
  // Deep blue is not full output just because one channel is.
  assert.ok(levelOf({ r: 0, g: 0, b: 1 }) < 0.5);
  assert.strictEqual(levelOf({ intensity: 0.6 }), 0.6);
  assert.strictEqual(levelOf({}), 0);
});

check('level is clamped', () => {
  assert.strictEqual(levelOf({ intensity: 5 }), 1);
});

if (failures) { console.log('\n' + failures + ' failing'); process.exit(1); }
console.log('\nall passing');
