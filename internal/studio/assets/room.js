/* A room, in CSS 3D.
 *
 * No 3D engine. A home cinema is a box with a screen at one end, a seat at the
 * other, and a handful of devices at known positions; CSS transforms draw that
 * perfectly well and cost nothing to ship. The point of this view is not to
 * look photographic, it is to answer "which device is doing what, right now",
 * and for that, legibility beats realism.
 *
 * Coordinates come from the rig, in metres: origin at the centre of the screen
 * wall, x right, y up, z toward the audience.
 */

'use strict';

const PX_PER_M = 62;
const ROOM_W = 5.0;
const ROOM_H = 3.0;
const ROOM_D = 6.0;

/* Devices are drawn as flat markers, so each one is counter-rotated to face
 * the camera. Without this they lie down on the floor as the room tilts. */
const TILT = 16;

const KIND_GLYPH = {
  light: '●',
  wind: '✳',
  shake: '■',
  motion: '▬',
  mist: '⁂',
  fog: '☁',
  scent: '⚘',
};

function mk(tag, className, parent) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (parent) parent.appendChild(node);
  return node;
}

class Room {
  constructor(host) {
    this.host = host;
    this.devices = new Map();
    /* Shared with the timeline: muting a track there takes its device out
     * of the room too, which is what makes reviewing one effect possible. */
    this.muted = new Set();
    this.build();
  }

  build() {
    this.host.textContent = '';
    const stage = mk('div', 'stage', this.host);
    const room = mk('div', 'room', stage);
    room.style.transform =
      `rotateX(${TILT}deg) translateZ(${-ROOM_D * PX_PER_M * 0.45}px)`;
    this.room = room;

    const w = ROOM_W * PX_PER_M;
    const h = ROOM_H * PX_PER_M;
    const d = ROOM_D * PX_PER_M;

    const floor = mk('div', 'surface floor', room);
    floor.style.width = w + 'px';
    floor.style.height = d + 'px';
    floor.style.transform = `translate3d(${-w / 2}px, 0px, 0px) rotateX(90deg)`;

    const back = mk('div', 'surface wall', room);
    back.style.width = w + 'px';
    back.style.height = h + 'px';
    back.style.transform = `translate3d(${-w / 2}px, ${-h}px, 0px)`;

    /* The screen doubles as the ambient light preview: it takes the colour the
     * soft layer is driving, which is exactly what an Ambilight looks like
     * from a seat. */
    this.screen = mk('div', 'screen', room);
    this.screen.style.width = (w * 0.62) + 'px';
    this.screen.style.height = (h * 0.5) + 'px';
    this.screen.style.transform =
      `translate3d(${-w * 0.31}px, ${-h * 0.78}px, 6px)`;

    this.seat = mk('div', 'seat', room);
    this.seat.style.transform = this.seatTransform(0);
  }

  seatTransform(heave) {
    const z = 3.0 * PX_PER_M;
    const y = -(0.45 + heave) * PX_PER_M;
    return `translate3d(-45px, ${y}px, ${z}px) rotateX(${-TILT}deg)`;
  }

  /* Lay out the devices a rig declares. Called once; updates only change
   * style, never structure, because rebuilding forty nodes per frame is how a
   * preview becomes a slideshow. */
  setInstruments(instruments) {
    for (const [, node] of this.devices) node.remove();
    this.devices.clear();

    for (const inst of instruments) {
      const [x, y, z] = inst.position;
      const node = mk('div', 'device kind-' + inst.kind, this.room);
      node.style.transform =
        `translate3d(${x * PX_PER_M}px, ${-y * PX_PER_M}px, ${z * PX_PER_M}px) ` +
        `rotateX(${-TILT}deg)`;

      const dot = mk('div', 'dot', node);
      dot.textContent = KIND_GLYPH[inst.kind] || '○';
      const label = mk('div', 'label', node);
      label.textContent = inst.id;

      this.devices.set(inst.id, { node: node, dot: dot, label: label, kind: inst.kind });
    }
  }

  /* Draw one moment. state is what state.js produced. */
  update(state) {
    let ambient = null;

    for (const [id, device] of this.devices) {
      const off = this.muted.has(id);
      device.node.classList.toggle("muted", off);
      const { level, params } = deviceState(state, id, this.muted);
      device.node.classList.toggle('on', level > 0.02);

      if (device.kind === 'light') {
        const colour = cssColour(params);
        device.dot.style.color = colour;
        device.dot.style.textShadow =
          `0 0 ${6 + level * 46}px ${colour}, 0 0 ${2 + level * 14}px ${colour}`;
        if (!ambient || level > ambient.level) ambient = { colour: colour, level: level };
      } else {
        device.dot.style.opacity = String(0.28 + level * 0.72);
        /* Scale rather than colour, so intensity reads at a glance on the
         * devices that have no colour of their own. */
        device.dot.style.transform = `scale(${1 + level * 0.85})`;
      }

      if (device.kind === 'wind') {
        device.dot.style.animationDuration = level > 0.02
          ? (1.6 - level * 1.4).toFixed(2) + 's' : '0s';
      }
      if (device.kind === 'shake' || device.kind === 'motion') {
        device.node.style.setProperty('--shake', (level * 5).toFixed(2) + 'px');
      }
    }

    this.seat.style.transform = this.seatTransform(seatPose(state).heave * 0.5);

    if (ambient) {
      this.screen.style.background =
        `radial-gradient(circle at 50% 45%, ${ambient.colour}, #05070b 78%)`;
      this.screen.style.boxShadow =
        `0 0 ${20 + ambient.level * 90}px ${ambient.colour}`;
    }
  }
}

function findKind(state, kind) {
  for (const id of Object.keys(state)) {
    if (id.indexOf(kind + '.') === 0) return state[id];
  }
  return null;
}

/* What one device is doing, as both room views read it.
 *
 * Shared rather than duplicated on purpose. The flat view and the 3D view have
 * to agree about what "muted" and "active" mean, and the cheapest way to
 * guarantee that is to give them one function instead of two that look alike
 * today and drift apart in a month. A muted device is level 0, not hidden: it
 * still occupies its place in the room, it is just silent.
 */
function deviceState(state, id, muted) {
  const s = state[id];
  const off = muted ? muted.has(id) : false;
  return {
    level: (!off && s && s.active) ? s.level : 0,
    params: (s && s.params) || {},
    muted: off,
  };
}

/* Where the seat is, in metres and radians.
 *
 * The platform reports six axes and the room only shows what a seat can
 * actually do. Translations are scaled down hard: a rig with 90mm of heave
 * would be invisible at room scale, and the point of the view is to read the
 * motion, not to measure it. This is a preview, and it says so.
 */
function seatPose(state) {
  const motion = state['motion.platform'] || findKind(state, 'motion');
  const p = (motion && motion.params) || {};
  const n = (v) => (typeof v === 'number' && isFinite(v) ? v : 0);
  return {
    surge: n(p.surge), sway: n(p.sway), heave: n(p.heave),
    roll: n(p.roll), pitch: n(p.pitch), yaw: n(p.yaw),
  };
}

function cssColour(params) {
  const to255 = (v) => Math.round(Math.max(0, Math.min(1, v || 0)) * 255);
  if ('r' in params || 'g' in params || 'b' in params) {
    return `rgb(${to255(params.r)}, ${to255(params.g)}, ${to255(params.b)})`;
  }
  const i = to255(params.intensity);
  return `rgb(${i}, ${i}, ${i})`;
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { cssColour, deviceState, seatPose, PX_PER_M };
}

Room.prototype.setMuted = function (muted) {
  this.muted = muted;
};
