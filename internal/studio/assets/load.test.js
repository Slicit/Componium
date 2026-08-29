/* Run with: node load.test.js
 *
 * Loads every script into one shared global context, the way a browser does
 * with classic script tags, and asserts the application actually starts.
 *
 * Two bugs are behind this file, and both passed `node --check`:
 *
 *   room.js declared `function el(...)` and app.js declared `const el = ...`.
 *   Separately valid; together in one global scope the const collides with the
 *   function's global binding and throws before a line of app.js runs.
 *
 *   Later, a bad splice removed the last section of app.js, taking the call to
 *   load() with it. Every function was defined and none of them ever ran. That
 *   is a perfectly valid script and a completely dead application, and no
 *   syntax check will ever object to it.
 *
 * Hence the last check below. Defining things is not starting.
 */

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const vm = require('vm');

/* A DOM stub, kept as small as the scripts allow. It is not pretending to be a
 * browser; it exists so top level code runs to completion, and it records what
 * was asked of it so the test can tell whether anything happened. */
function freshContext() {
  const calls = { fetched: [], frames: 0, listeners: 0 };

  function node() {
    const self = {
      style: { setProperty() {} },
      classList: { add() {}, remove() {}, toggle() {} },
      children: [],
      className: '', textContent: '', value: '', title: '',
      hidden: false, checked: false, type: '',
      appendChild(child) { self.children.push(child); return child; },
      remove() {},
      addEventListener() { calls.listeners++; },
      setAttribute() {},
      getBoundingClientRect() { return { left: 0, width: 100 }; },
    };
    return self;
  }

  const context = {
    console: console,
    Set: Set, Map: Map, Object: Object, Array: Array, Math: Math,
    Number: Number, String: String, JSON: JSON, Promise: Promise,
    document: {
      createElement: node,
      createElementNS: node,
      getElementById: node,
      addEventListener() { calls.listeners++; },
    },
    window: { addEventListener() { calls.listeners++; } },
    performance: { now: () => 0 },
    requestAnimationFrame() { calls.frames++; },
    setTimeout() {},
    /* Fails fast, so load() stops before it needs a real server. Reaching it
     * at all is the point. */
    fetch(url) {
      calls.fetched.push(url);
      return Promise.resolve({ ok: false, json: () => Promise.resolve({}) });
    },
    __calls: calls,
  };
  context.globalThis = context;
  return context;
}

const dir = __dirname;
const scripts = ['state.js', 'room.js', 'timeline.js', 'library.js', 'app.js'];

function loadAll() {
  const ctx = vm.createContext(freshContext());
  for (const name of scripts) {
    const src = fs.readFileSync(path.join(dir, name), 'utf8');
    new vm.Script(src, { filename: name }).runInContext(ctx);
  }
  return ctx;
}

let failures = 0;
function check(name, fn) {
  try { fn(); console.log('ok   ' + name); }
  catch (e) { failures++; console.log('FAIL ' + name + ': ' + e.message); }
}

check('every script loads together without colliding', () => {
  loadAll();
});

check('no two scripts declare the same top level name', () => {
  const seen = new Map();
  const decl = /^(?:function|const|let|var|class)\s+([A-Za-z_$][\w$]*)/;
  for (const name of scripts) {
    const src = fs.readFileSync(path.join(dir, name), 'utf8');
    for (const line of src.split('\n')) {
      const m = decl.exec(line);
      if (!m) continue;
      if (seen.has(m[1]) && seen.get(m[1]) !== name) {
        throw new Error(`${m[1]} is declared in both ${seen.get(m[1])} and ${name}`);
      }
      seen.set(m[1], name);
    }
  }
});

check('the pieces the page depends on are defined', () => {
  const ctx = loadAll();
  for (const wanted of ['evaluate', 'Room', 'Timeline', 'fmt']) {
    assert.notStrictEqual(vm.runInContext('typeof ' + wanted, ctx), 'undefined',
                          wanted + ' is not defined after loading every script');
  }
});

/* The check the earlier version was missing. Defining things is not starting:
 * app.js once lost its final section, kept every function, called none of
 * them, and produced a page that loaded six files perfectly and did nothing. */
check('the application starts', () => {
  const calls = loadAll().__calls;

  assert.ok(calls.fetched.length > 0,
            'nothing was fetched: the page loaded but never asked for a score');
  assert.ok(calls.fetched.some((u) => String(u).includes('/api/score')),
            'the score was never requested, so the timeline can never appear');
  assert.ok(calls.fetched.some((u) => String(u).includes('/api/rig')),
            'the rig was never requested, so the room can never appear');
  assert.ok(calls.frames > 0,
            'no animation frame was requested, so nothing would ever update');
  assert.ok(calls.listeners > 0,
            'no event listeners were attached, so no control would work');
});

if (failures) { console.log('\n' + failures + ' failing'); process.exit(1); }
console.log('\nall passing');
