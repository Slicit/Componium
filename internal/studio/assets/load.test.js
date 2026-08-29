/* Run with: node load.test.js
 *
 * Loads every script into one shared global context, the way a browser does
 * with classic script tags, and asserts the page comes up.
 *
 * This exists because of a bug that node --check could not see. room.js
 * declared `function el(...)` and app.js declared `const el = ...`. Separately
 * both are valid. Together, in one global scope, the const collides with the
 * function's global binding and throws before a line of app.js runs, so the
 * whole application was dead: no picker, no room, no timeline, and no error
 * anywhere except a browser console nobody was looking at.
 *
 * Checking files one at a time cannot find that. Only loading them together
 * can.
 */

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const vm = require('vm');

/* A DOM stub, kept as small as the scripts allow. It is not pretending to be a
 * browser; it exists so that top level code runs to completion. */
function node() {
  const self = {
    style: { setProperty() {} },
    classList: { add() {}, remove() {}, toggle() {} },
    children: [],
    className: '', textContent: '', value: '', title: '',
    hidden: false, checked: false, type: '',
    appendChild(child) { self.children.push(child); return child; },
    remove() {},
    addEventListener() {},
    setAttribute() {},
    getBoundingClientRect() { return { left: 0, width: 100 }; },
  };
  return self;
}

/* A fresh context per run: vm.createContext mutates the object it is given, so
 * reusing one would collide the scripts with themselves on a second load. */
function freshContext() {
  const context = {
    console: console,
    Set: Set, Map: Map, Object: Object, Array: Array, Math: Math,
    Number: Number, String: String, JSON: JSON, Promise: Promise,
    document: {
      createElement: node,
      createElementNS: node,
      getElementById: node,
      addEventListener() {},
    },
    window: { addEventListener() {} },
    performance: { now: () => 0 },
    requestAnimationFrame() {},
    /* Fails fast, so load() stops before it needs a real server. Getting that
     * far is the point: everything above it has already been parsed. */
    fetch: () => Promise.resolve({ ok: false, json: () => Promise.resolve({}) }),
  };
  context.globalThis = context;
  return context;
}

const dir = __dirname;
const scripts = ['state.js', 'room.js', 'timeline.js', 'app.js'];

let failures = 0;
function check(name, fn) {
  try { fn(); console.log('ok   ' + name); }
  catch (e) { failures++; console.log('FAIL ' + name + ': ' + e.message); }
}

check('every script loads together without colliding', () => {
  const ctx = vm.createContext(freshContext());
  for (const name of scripts) {
    const src = fs.readFileSync(path.join(dir, name), 'utf8');
    /* The same order the page loads them in. A collision shows up here as the
     * SyntaxError a browser would have thrown. */
    new vm.Script(src, { filename: name }).runInContext(ctx);
  }
});

check('no two scripts declare the same top level name', () => {
  const seen = new Map();
  const decl = /^(?:function|const|let|var|class)\s+([A-Za-z_$][\w$]*)/;
  for (const name of scripts) {
    const src = fs.readFileSync(path.join(dir, name), 'utf8');
    for (const line of src.split('\n')) {
      const m = decl.exec(line);
      if (!m) continue;
      const ident = m[1];
      if (seen.has(ident) && seen.get(ident) !== name) {
        throw new Error(
          `${ident} is declared in both ${seen.get(ident)} and ${name}`);
      }
      seen.set(ident, name);
    }
  }
});

check('the pieces the page depends on are defined', () => {
  const ctx = vm.createContext(freshContext());
  for (const name of scripts) {
    new vm.Script(fs.readFileSync(path.join(dir, name), 'utf8'),
                  { filename: name }).runInContext(ctx);
  }
  for (const wanted of ['evaluate', 'Room', 'Timeline', 'fmt']) {
    assert.ok(vm.runInContext('typeof ' + wanted, ctx) !== 'undefined',
              wanted + ' is not defined after loading every script');
  }
});

if (failures) { console.log('\n' + failures + ' failing'); process.exit(1); }
console.log('\nall passing');
