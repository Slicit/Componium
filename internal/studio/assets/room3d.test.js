/* Run with: node room3d.test.js
 *
 * Drives the 3D room against a stub of three.js.
 *
 * This file exists because of a limitation worth writing down: the sandboxed
 * browser this project is developed against never delivers animation frames,
 * so a WebGL view can be loaded there, report a clean console, and still be
 * drawing nothing at all. "The page loaded" has already once been mistaken for
 * "the application works" in this repository, and once was enough.
 *
 * So the scene graph is built for real, against a stub that records what was
 * asked of it. Everything below is a property of the room's own logic —
 * placement, orientation, the level-to-opacity mapping, muting, the seat pose,
 * the light cap, teardown — and none of it needs a GPU to be true. What this
 * cannot check is whether the result looks good. Nothing automated can.
 */

'use strict';

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

/* --- a three.js small enough to reason about --------------------------- */

const STUB = `
export const AdditiveBlending = 'additive';
export const DoubleSide = 'double';
export const BackSide = 'back';
export const SRGBColorSpace = 'srgb';
export const ACESFilmicToneMapping = 'aces';

class V3 {
  constructor(x, y, z) { this.x = x || 0; this.y = y || 0; this.z = z || 0; }
  set(x, y, z) { this.x = x; this.y = y; this.z = z; return this; }
  setScalar(v) { return this.set(v, v, v); }
  copy(o) { return this.set(o.x, o.y, o.z); }
}
export { V3 as Vector3 };

export class Color {
  constructor(v) { this.r = 1; this.g = 1; this.b = 1; if (v !== undefined) this.set(v); }
  set(v) {
    if (typeof v === 'string') {
      const m = v.match(/rgb\\((\\d+),\\s*(\\d+),\\s*(\\d+)\\)/);
      if (m) { this.r = +m[1] / 255; this.g = +m[2] / 255; this.b = +m[3] / 255; }
    } else if (typeof v === 'number') {
      this.r = ((v >> 16) & 255) / 255;
      this.g = ((v >> 8) & 255) / 255;
      this.b = (v & 255) / 255;
    }
    return this;
  }
  copy(o) { this.r = o.r; this.g = o.g; this.b = o.b; return this; }
  multiplyScalar(s) { this.r *= s; this.g *= s; this.b *= s; return this; }
  addScalar(s) { this.r += s; this.g += s; this.b += s; return this; }
  setScalar(s) { this.r = this.g = this.b = s; return this; }
}

class Object3D {
  constructor() {
    this.position = new V3();
    this.rotation = new V3();
    this.scale = new V3(1, 1, 1);
    this.children = [];
    this.visible = true;
    this.lookedAt = null;
  }
  add(c) { this.children.push(c); return this; }
  remove(c) { this.children = this.children.filter((x) => x !== c); return this; }
  lookAt(v) { this.lookedAt = v; }
  traverse(fn) { fn(this); for (const c of this.children) c.traverse(fn); }
}
export { Object3D as Group };

export class Scene extends Object3D {}

export class PerspectiveCamera extends Object3D {
  constructor(fov, aspect) { super(); this.fov = fov; this.aspect = aspect; this.projections = 0; }
  updateProjectionMatrix() { this.projections++; }
}

export class WebGLRenderer {
  constructor() {
    this.domElement = globalThis.document.createElement('canvas');
    this.renders = 0; this.disposed = 0; this.size = [0, 0];
  }
  setPixelRatio() {}
  setSize(w, h) { this.size = [w, h]; }
  render() { this.renders++; }
  dispose() { this.disposed++; }
}

const geom = (name) => class extends Object {
  constructor() { super(); this.type = name; this.disposed = 0; this.attributes = {}; }
  setAttribute(k, v) { this.attributes[k] = v; }
  dispose() { this.disposed++; }
};
export const SphereGeometry = geom('sphere');
export const ConeGeometry = geom('cone');
export const BoxGeometry = geom('box');
export const PlaneGeometry = geom('plane');
export const BufferGeometry = geom('buffer');

export class BufferAttribute {
  constructor(array, size) { this.array = array; this.itemSize = size; this.needsUpdate = false; }
}

class Material {
  constructor(o) { Object.assign(this, o || {}); this.color = new Color(this.color); this.disposed = 0; }
  dispose() { this.disposed++; }
}
export class MeshBasicMaterial extends Material {}
export class PointsMaterial extends Material {}
export class MeshStandardMaterial extends Material {
  constructor(o) { super(o); this.emissive = new Color(o && o.emissive); }
}

export class Mesh extends Object3D {
  constructor(g, m) { super(); this.geometry = g; this.material = m; }
}
export class Points extends Mesh {}
export class SpriteMaterial extends Material {}
export class Sprite extends Object3D {
  constructor(m) { super(); this.material = m; }
}

export class CanvasTexture { constructor(c) { this.image = c; this.disposed = 0; } dispose() { this.disposed++; } }
export class Fog { constructor(c, n, f) { this.near = n; this.far = f; } }

class Light extends Object3D {
  constructor(colour, intensity) { super(); this.color = new Color(colour); this.intensity = intensity || 0; }
}
export class PointLight extends Light {}
export class AmbientLight extends Light {}
export class HemisphereLight extends Light {}

export class PMREMGenerator {
  constructor(renderer) { this.renderer = renderer; this.disposed = 0; }
  fromScene() { return { texture: { dispose() {} } }; }
  dispose() { this.disposed++; }
}
`;

const ENVIRONMENT = `
export class RoomEnvironment {}
`;

const CONTROLS = `
export class OrbitControls {
  constructor(camera, dom) {
    this.camera = camera; this.dom = dom;
    this.target = { set() {} };
    this.updates = 0; this.disposed = 0;
  }
  update() { this.updates++; }
  dispose() { this.disposed++; }
}
`;

/* --- a DOM small enough to reason about -------------------------------- */

function canvasNode() {
  const node = {
    width: 0, height: 0,
    clientWidth: 800, clientHeight: 400,
    style: {}, textContent: '', parentNode: null,
    children: [],
    appendChild(c) { node.children.push(c); c.parentNode = node; return c; },
    removeChild(c) { node.children = node.children.filter((x) => x !== c); c.parentNode = null; },
    getContext(kind) {
      if (kind === '2d') {
        return {
          createRadialGradient: () => ({ addColorStop() {} }),
          fillRect() {},
          set fillStyle(v) {},
        };
      }
      return {};
    },
  };
  return node;
}

globalThis.document = { createElement: canvasNode };
globalThis.requestAnimationFrame = function () { /* never fires, as in the sandbox */ };
globalThis.performance = { now: () => Date.now() };
globalThis.devicePixelRatio = 1;
globalThis.Event = class { constructor(type) { this.type = type; } };
globalThis.dispatchEvent = function () {};

/* room.js is a classic script; take the helpers the module reads off the
 * global scope, exactly as the browser hands them over. */
const room = require('./room.js');
globalThis.deviceState = room.deviceState;
globalThis.seatPose = room.seatPose;
globalThis.cssColour = room.cssColour;

/* --- load room3d.js with its imports pointed at the stub --------------- */

/* Every bare specifier the page will actually ask for must be in the import
 * map, and every entry must point at a file that exists.
 *
 * This is here because the suite below cannot catch it: it rewrites the
 * imports to reach the stub, which is exactly the step that hides a missing
 * map entry. Adding the RoomEnvironment import without adding its mapping
 * passed every test and shipped a page where the whole module failed to
 * resolve, so the room silently fell back to the flat view and blamed the
 * browser for having no GPU.
 *
 * The vendored files are scanned too, because their own `from 'three'` is
 * resolved by the same map and is just as capable of being missing from it.
 */
function checkImportMap() {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  const block = html.match(/<script type="importmap">([\s\S]*?)<\/script>/);
  assert.ok(block, 'index.html has no import map');
  const imports = JSON.parse(block[1]).imports;

  for (const [spec, url] of Object.entries(imports)) {
    const file = url.split('?')[0].replace(/^\.\//, '');
    assert.ok(fs.existsSync(path.join(__dirname, file)),
      `import map sends '${spec}' to ${file}, which does not exist`);
  }

  const sources = ['room3d.js', 'vendor/OrbitControls.js', 'vendor/RoomEnvironment.js'];
  for (const name of sources) {
    const src = fs.readFileSync(path.join(__dirname, name), 'utf8');
    for (const m of src.matchAll(/^\s*import\s[\s\S]*?from\s+'([^']+)';/gm)) {
      const spec = m[1];
      if (spec.startsWith('.') || spec.startsWith('/')) continue;
      assert.ok(imports[spec],
        `${name} imports '${spec}' but the import map in index.html has no entry for it`);
    }
  }
}

async function loadRoom3D() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'componium-room3d-'));
  fs.writeFileSync(path.join(dir, 'three.mjs'), STUB);
  fs.writeFileSync(path.join(dir, 'controls.mjs'), CONTROLS);
  fs.writeFileSync(path.join(dir, 'env.mjs'), ENVIRONMENT);

  /* Anchored to the start of a line, because the prose above the imports
   * mentions the specifiers too and a plain string replace rewrites the
   * comment and leaves the real import alone. It did exactly that once. */
  const source = fs.readFileSync(path.join(__dirname, 'room3d.js'), 'utf8')
    .replace(/^import \* as THREE from 'three';$/m, "import * as THREE from './three.mjs';")
    .replace(/^import \{ OrbitControls \} from '[^']*';$/m, "import { OrbitControls } from './controls.mjs';")
    .replace(/^import \{ RoomEnvironment \} from '[^']*';$/m, "import { RoomEnvironment } from './env.mjs';");
  assert.ok(!/from 'three/.test(source.replace(/^\s*\*.*$/gm, '')),
    'import rewriting missed a specifier');
  const file = path.join(dir, 'room3d.mjs');
  fs.writeFileSync(file, source);

  await import('file://' + file.split(path.sep).join('/'));
  assert.ok(globalThis.Room3D, 'room3d.js did not publish Room3D');
  return globalThis.Room3D;
}

const RIG = [
  { id: 'light.left', kind: 'light', position: [-2.4, 1.6, 1.0] },
  { id: 'light.right', kind: 'light', position: [2.4, 1.6, 1.0] },
  { id: 'wind.main', kind: 'wind', position: [0, 2.2, 0.4] },
  { id: 'mist.ceiling', kind: 'mist', position: [0, 2.8, 3.0] },
  { id: 'motion.platform', kind: 'motion', position: [0, 0.2, 3.0] },
];

function run(Room3D) {
  const host = canvasNode();
  const view = new Room3D(host);

  /* Built at all. */
  assert.ok(view.renderer, 'no renderer');
  assert.deepStrictEqual(view.renderer.size, [800, 400], 'canvas not sized from the host');

  view.setInstruments(RIG);
  assert.strictEqual(view.devices.size, 5, 'wrong device count');

  /* Placed where the rig says, in metres, with no conversion. */
  const wind = view.devices.get('wind.main');
  assert.deepStrictEqual(
    [wind.group.position.x, wind.group.position.y, wind.group.position.z],
    [0, 2.2, 0.4], 'wind device is not at its rig position');

  /* And aimed at the seat rather than left pointing down the y axis. */
  assert.ok(wind.group.lookedAt, 'wind emitter was never aimed');
  assert.strictEqual(wind.group.lookedAt.z, 3.4, 'wind emitter is not aimed at the seat');

  /* Off means off. */
  view.update({});
  const cone = wind.group.children[0];
  assert.strictEqual(cone.material.opacity, 0, 'idle wind cone is visible');

  /* On means visibly on, and harder means more. */
  view.update({ 'wind.main': { active: true, level: 0.4, params: {} } });
  const soft = cone.material.opacity;
  view.update({ 'wind.main': { active: true, level: 1.0, params: {} } });
  assert.ok(cone.material.opacity > soft && soft > 0,
    'wind cone does not track level (' + soft + ' then ' + cone.material.opacity + ')');

  /* A light takes the cue's colour, not a fixed one. */
  view.update({ 'light.left': { active: true, level: 1, params: { r: 1, g: 0, b: 0 } } });
  const bulb = view.devices.get('light.left').group.children[0];
  /* Dominance rather than exact channels: every lamp carries a small floor in
   * all three so an unlit one is still visible in the room, which means a pure
   * red cue is legitimately (1, 0.1, 0.1) and not (1, 0, 0). */
  assert.ok(bulb.material.color.r > bulb.material.color.g * 3
    && bulb.material.color.r > bulb.material.color.b * 3,
    'red cue did not make a red light: '
    + JSON.stringify([bulb.material.color.r, bulb.material.color.g, bulb.material.color.b]));
  assert.ok(view.devices.get('light.left').light.intensity > 0, 'point light stayed dark');

  /* The brightest light drives the screen wash, which is the ambient preview. */
  assert.ok(view.screenGlow.intensity > 1.2, 'screen did not pick up the ambient colour');

  /* Muting silences a device without deleting it: still placed, still there to
   * click, just not doing anything, and visibly marked as the reason. */
  const lit = { 'light.left': { active: true, level: 1, params: { r: 1, g: 0, b: 0 } } };
  view.setMuted(new Set(['light.left']));
  view.update(lit);
  assert.strictEqual(view.devices.get('light.left').light.visible, false,
    'muted light still casts');
  assert.ok(view.devices.has('light.left'), 'muting removed the device');
  assert.ok(view.devices.get('light.left').group.scale.x < 1, 'muted device is not marked');

  /* And it must be recoverable. The mute treatment has to be recomputed from
   * the mute set every frame, never accumulated onto last frame's values: the
   * first version of this multiplied opacity down by a factor per frame, so a
   * device muted for a few seconds faded to nothing and stayed invisible after
   * it was unmuted. Holding the mute for many frames is the whole test. */
  for (let i = 0; i < 40; i++) view.update(lit);
  view.setMuted(new Set());
  view.update(lit);
  assert.strictEqual(view.devices.get('light.left').group.scale.x, 1,
    'unmuting did not restore the device');
  assert.ok(view.devices.get('light.left').light.intensity > 0,
    'device stayed dark after a long mute');

  /* The seat follows the platform. */
  view.update({ 'motion.platform': { active: true, level: 1, params: { heave: 0.4, surge: 0.2, roll: 0.3 } } });
  assert.ok(view.seat.position.y > 0, 'heave did not lift the seat');
  assert.ok(view.seat.position.z > view.seatRest, 'surge did not move the seat forward');
  assert.ok(view.seat.rotation.z !== 0, 'roll did not tilt the seat');

  view.update({ 'motion.platform': { active: false, level: 0, params: {} } });
  assert.strictEqual(view.seat.position.y, 0, 'seat did not return to rest');

  /* --- forcing a device by hand ---
   *
   * The whole point is that this works with no cue at the playhead and no
   * score behind it, so every check here runs against an empty state. */
  view.setForced(new Map([['wind.main', 0.7]]));
  view.update({});
  assert.ok(cone.material.opacity > 0, 'forcing the wind did nothing');

  /* A forced light with nothing to borrow from is white, not the black that
   * cssColour returns for empty parameters. Getting this wrong makes the
   * slider look broken: the light is at full level and invisible. */
  view.setForced(new Map([['light.left', 1]]));
  view.update({});
  const forcedBulb = view.devices.get('light.left').group.children[0];
  assert.ok(forcedBulb.material.color.r > 0.5 && forcedBulb.material.color.b > 0.5,
    'a forced light with no cue is not lit');
  assert.ok(view.devices.get('light.left').light.intensity > 0,
    'a forced light does not cast');

  /* A forced platform has no pose to report, so it invents one — and it has to
   * actually move, or the slider says nothing about what the rig would do. */
  view.setForced(new Map([['motion.platform', 1]]));
  const seen = new Set();
  for (let i = 0; i < 6; i++) {
    globalThis.performance = { now: () => 1000 + i * 220 };
    view.update({});
    seen.add(view.seat.position.y.toFixed(4));
  }
  assert.ok(seen.size > 1, 'a forced platform sits still');
  globalThis.performance = { now: () => Date.now() };

  /* Releasing hands the device back to the score rather than pinning it at
   * zero, which is the difference between this control and mute. */
  view.setForced(new Map());
  view.update({ 'wind.main': { active: true, level: 0.9, params: {} } });
  assert.ok(cone.material.opacity > 0, 'releasing a forced device silenced it');
  view.update({});
  assert.strictEqual(cone.material.opacity, 0, 'released device did not follow the score');

  /* Mute beats force, so one control cannot defeat the other. */
  view.setForced(new Map([['wind.main', 1]]));
  view.setMuted(new Set(['wind.main']));
  view.update({});
  assert.strictEqual(cone.material.opacity, 0, 'muting did not beat forcing');
  view.setMuted(new Set());
  view.setForced(new Map());

  /* Every update paints, without waiting for an animation frame that this
   * environment is never going to deliver. That is the whole reason the view
   * renders from update() instead of only from the loop. */
  const before = view.renderer.renders;
  view.update({});
  assert.strictEqual(view.renderer.renders, before + 1, 'update() did not render');

  /* A garbage pose must not throw or produce NaN geometry. */
  view.update({ 'motion.platform': { active: true, level: 1, params: { heave: null, roll: 'x' } } });
  assert.ok(Number.isFinite(view.seat.position.y), 'seat position went non-finite');

  /* The light cap holds, and the devices past it still exist. */
  const many = [];
  for (let i = 0; i < 14; i++) many.push({ id: 'light.' + i, kind: 'light', position: [0, 1, i] });
  view.setInstruments(many);
  let cast = 0;
  for (const [, d] of view.devices) if (d.light) cast++;
  assert.strictEqual(cast, 8, 'point light cap is not being enforced, got ' + cast);
  assert.strictEqual(view.devices.size, 14, 'devices past the cap were dropped');

  /* An unknown kind is drawn as something rather than crashing the room. */
  view.setInstruments([{ id: 'kraken.1', kind: 'kraken', position: [0, 1, 1] }]);
  view.update({ 'kraken.1': { active: true, level: 1, params: {} } });
  assert.strictEqual(view.devices.size, 1, 'unknown kind was dropped');

  /* Teardown gives the context back and stops the loop. */
  view.dispose();
  assert.strictEqual(view.renderer.disposed, 1, 'renderer was not disposed');
  assert.strictEqual(view.running, false, 'animation loop still running after dispose');
  assert.strictEqual(view.renderer.domElement.parentNode, null, 'canvas left in the page');
}

checkImportMap();

loadRoom3D().then((Room3D) => {
  run(Room3D);
  console.log('room3d.test.js: ok');
}).catch((err) => {
  console.error(err);
  process.exit(1);
});
