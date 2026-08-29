/* Run with: node timecode.test.js
 *
 * No test framework, for the same reason the studio has no build step.
 */

'use strict';

const assert = require('assert');
const { toTimecode, fromTimecode, clamp01 } = require('./timecode.js');

let failures = 0;
function check(name, fn) {
  try { fn(); console.log('ok   ' + name); }
  catch (e) { failures++; console.log('FAIL ' + name + ': ' + e.message); }
}

check('formats whole values', () => {
  assert.strictEqual(toTimecode(0), '00:00:00.000');
  assert.strictEqual(toTimecode(3661.5), '01:01:01.500');
});

check('rounds without producing 60 seconds', () => {
  // The bug this exists to prevent: decomposing before rounding gives
  // 00:00:60.000, which is not a timecode.
  assert.strictEqual(toTimecode(59.9995), '00:01:00.000');
});

check('negative times clamp to zero', () => {
  assert.strictEqual(toTimecode(-5), '00:00:00.000');
  assert.strictEqual(toTimecode(NaN), '00:00:00.000');
});

check('parses the forms the Go parser accepts', () => {
  assert.strictEqual(fromTimecode('01:04:22.100'), 3862.1);
  assert.strictEqual(fromTimecode('04:22.5'), 262.5);
  assert.strictEqual(fromTimecode('22'), 22);
  assert.strictEqual(fromTimecode('0'), 0);
});

check('rejects what is not a timecode', () => {
  for (const bad of ['', '   ', 'banana', '1:2:3:4', '-5:00', '1::2']) {
    assert.strictEqual(fromTimecode(bad), null, 'accepted ' + JSON.stringify(bad));
  }
});

check('round trips', () => {
  for (const s of ['00:00:00.000', '01:04:22.100', '02:35:12.000']) {
    assert.strictEqual(toTimecode(fromTimecode(s)), s);
  }
});

check('clamps values to the unit range', () => {
  assert.strictEqual(clamp01(1.5), 1);
  assert.strictEqual(clamp01(-0.5), 0);
  assert.strictEqual(clamp01(0.25), 0.25);
  assert.strictEqual(clamp01('nonsense'), 0);
});

if (failures) { console.log('\n' + failures + ' failing'); process.exit(1); }
console.log('\nall passing');
