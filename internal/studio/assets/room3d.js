/* The room, in three.js.
 *
 * The flat CSS view answers "which device is doing what". This one answers a
 * question the flat view cannot: "what would that feel like from the seat".
 * Wind has a direction and a spread. Mist hangs in the air and falls. A light
 * spike throws colour onto the walls behind you. None of that is legible as a
 * marker on a plan, and all of it is what a contributor needs to see before
 * trusting a cue to a machine that moves a person.
 *
 * It does not replace the flat view. Anything with a GPU gets this; anything
 * without falls back, and the fallback is a real view rather than an apology.
 *
 * Coordinates are the rig's, unchanged and in metres: origin at the centre of
 * the screen wall, x right, y up, z toward the audience. three.js uses the
 * same handedness, so there is no conversion anywhere in this file. That is
 * deliberate — a coordinate transform is a place for a sign error to hide, and
 * the rig file is the thing an operator edits by hand.
 */

/* Bare specifiers, resolved by the import map in index.html. The map is what
 * carries the cache-busting version onto the vendored files, and it is also
 * what makes OrbitControls' own `import ... from 'three'` resolve to the same
 * module instance this file uses rather than a second copy. */
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

const ROOM_W = 5.0;
const ROOM_H = 3.0;
const ROOM_D = 6.0;
const SEAT_Z = 3.0;

/* Above this, a device is drawn as doing something. Below it, the cue is
 * either finished or too weak to matter, and drawing it anyway makes an idle
 * room look busy. Same threshold as the flat view, on purpose. */
const ON = 0.02;

/* Real point lights are the expensive part of this scene, and past a handful
 * they stop adding information: eight coloured sources already wash the room.
 * Beyond the cap a light still glows and still drives the ambient wash, it
 * just does not cast. */
const MAX_LIGHTS = 8;

/* Shared with the flat view via the global scope, so the two cannot disagree
 * about what a cue means. See room.js. */
const readDevice = globalThis.deviceState;
const readSeat = globalThis.seatPose;
const colourOf = globalThis.cssColour;

/* --- materials and textures ------------------------------------------- */

/* A soft round blob, drawn once and reused by every particle in the scene.
 * Generated rather than shipped: it is nine lines of canvas, and a binary
 * asset in a repository is a thing nobody can review in a diff. */
function softSprite() {
  const size = 64;
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = size;
  const g = canvas.getContext('2d');
  const grad = g.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
  grad.addColorStop(0, 'rgba(255,255,255,1)');
  grad.addColorStop(0.4, 'rgba(255,255,255,0.35)');
  grad.addColorStop(1, 'rgba(255,255,255,0)');
  g.fillStyle = grad;
  g.fillRect(0, 0, size, size);
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/* Additive and depth-write off: the cones and particles are volumes of light,
 * not surfaces. Writing depth would let one invisible cone punch a hole in the
 * one behind it. */
function glowMaterial(colour, opacity) {
  return new THREE.MeshBasicMaterial({
    color: colour,
    transparent: true,
    opacity: opacity,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
    side: THREE.DoubleSide,
  });
}

/* --- device builders --------------------------------------------------- */

/* Each builder returns { group, apply(level, params, dt) }. The apply function
 * is called every frame with that device's current level, and owns everything
 * that moves. Adding a kind means adding one entry here and nothing else. */
const BUILDERS = {
  light(colour) {
    const group = new THREE.Group();

    const bulb = new THREE.Mesh(
      new THREE.SphereGeometry(0.06, 16, 12),
      new THREE.MeshBasicMaterial({ color: colour })
    );
    group.add(bulb);

    const halo = new THREE.Mesh(new THREE.SphereGeometry(0.16, 16, 12), glowMaterial(colour, 0));
    group.add(halo);

    /* A short cone toward the room, so a wall washer reads as pointing
     * somewhere rather than floating. */
    const cone = new THREE.Mesh(new THREE.ConeGeometry(0.5, 1.4, 20, 1, true), glowMaterial(colour, 0));
    cone.position.y = -0.7;
    group.add(cone);

    let light = null;
    return {
      group: group,
      /* Given a real light, drive it too. */
      attach(l) { light = l; },
      apply(level, params) {
        const c = new THREE.Color(colourOf(params));
        bulb.material.color.copy(c);
        halo.material.color.copy(c);
        cone.material.color.copy(c);
        /* The bulb never goes fully black: an off lamp is still a lamp, and a
         * device you cannot see is a device you cannot click. */
        bulb.material.color.multiplyScalar(0.25 + level * 0.75);
        halo.material.opacity = level * 0.55;
        cone.material.opacity = level * 0.16;
        halo.scale.setScalar(1 + level * 1.6);
        if (light) {
          light.color.copy(c);
          light.intensity = level * 9;
          light.visible = level > ON;
        }
      },
    };
  },

  wind(colour) {
    const group = new THREE.Group();
    const cone = new THREE.Mesh(new THREE.ConeGeometry(0.55, 2.2, 24, 1, true), glowMaterial(0x9fd8ff, 0));
    /* ConeGeometry points up the y axis with its apex at the top. Rotating by
     * -90° about x aims it down -z, and the group is then turned to face the
     * seat, so the apex sits at the fan and the mouth opens toward the
     * audience. */
    cone.rotation.x = -Math.PI / 2;
    cone.position.z = 1.1;
    group.add(cone);

    const streaks = particles(70, 0x9fd8ff, 0.07);
    group.add(streaks.points);

    return {
      group: group,
      apply(level, params, dt) {
        cone.material.opacity = level * 0.24;
        cone.scale.set(1, 1, 0.6 + level * 0.9);
        streaks.material.opacity = level * 0.75;
        /* Speed reads as speed. The stream is what tells you whether this is a
         * breeze or a gust, and a static cone tells you neither. */
        streaks.drift(dt, 0, 0, 1.2 + level * 6.5, 2.4);
      },
    };
  },

  mist(colour) {
    const group = new THREE.Group();
    const cloud = particles(150, 0xdfefff, 0.16, 1.1, 0.7, 1.1);
    group.add(cloud.points);
    return {
      group: group,
      apply(level, params, dt) {
        cloud.material.opacity = level * 0.5;
        /* Mist falls and spreads; it does not blow away. */
        cloud.drift(dt, 0, -0.22, 0.16, 1.6);
      },
    };
  },

  fog(colour) {
    const group = new THREE.Group();
    const cloud = particles(220, 0xc8d4e6, 0.34, 1.6, 0.5, 1.6);
    group.add(cloud.points);
    return {
      group: group,
      apply(level, params, dt) {
        cloud.material.opacity = level * 0.34;
        cloud.drift(dt, 0.05, -0.05, 0.1, 2.6);
      },
    };
  },

  water(colour) {
    const group = new THREE.Group();
    const drops = particles(90, 0x7fd0ff, 0.05, 0.7, 0.2, 0.7);
    group.add(drops.points);
    return {
      group: group,
      apply(level, params, dt) {
        drops.material.opacity = level * 0.9;
        drops.drift(dt, 0, -1.6, 0.5, 1.2);
      },
    };
  },

  scent(colour) {
    const group = new THREE.Group();
    const puff = particles(60, 0xd8c0ff, 0.13, 0.5, 0.5, 0.5);
    group.add(puff.points);
    return {
      group: group,
      apply(level, params, dt) {
        puff.material.opacity = level * 0.45;
        puff.drift(dt, 0, 0.14, 0.06, 2.0);
      },
    };
  },

  shake(colour) {
    const group = new THREE.Group();
    const box = new THREE.Mesh(
      new THREE.BoxGeometry(0.26, 0.1, 0.26),
      new THREE.MeshStandardMaterial({ color: 0xff9a5c, roughness: 0.5, emissive: 0xff9a5c })
    );
    group.add(box);
    return {
      group: group,
      apply(level) {
        box.material.emissive.setScalar(level * 0.6);
        const a = level * 0.05;
        box.position.set((Math.random() - 0.5) * a, (Math.random() - 0.5) * a, (Math.random() - 0.5) * a);
      },
    };
  },
};

/* The platform is not a device marker; it is the seat, handled separately. */
BUILDERS.motion = BUILDERS.shake;

/* A drifting cloud of soft sprites, recycled rather than reallocated: the
 * points are created once and wrap when they leave the box, so a two hour
 * preview allocates nothing after the first frame. */
function particles(count, colour, size, sx, sy, sz) {
  const spreadX = sx || 0.35;
  const spreadY = sy || 0.35;
  const spreadZ = sz || 0.35;
  const positions = new Float32Array(count * 3);
  const seeds = new Float32Array(count);
  for (let i = 0; i < count; i++) {
    positions[i * 3] = (Math.random() - 0.5) * spreadX * 2;
    positions[i * 3 + 1] = (Math.random() - 0.5) * spreadY * 2;
    positions[i * 3 + 2] = Math.random() * spreadZ * 2;
    seeds[i] = 0.5 + Math.random();
  }
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));

  const material = new THREE.PointsMaterial({
    color: colour,
    size: size,
    map: SPRITE,
    transparent: true,
    opacity: 0,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
    sizeAttenuation: true,
  });
  const points = new THREE.Points(geometry, material);

  return {
    points: points,
    material: material,
    drift(dt, vx, vy, vz, range) {
      if (material.opacity <= 0.001) return;
      const p = geometry.attributes.position.array;
      for (let i = 0; i < count; i++) {
        const s = seeds[i];
        p[i * 3] += vx * s * dt;
        p[i * 3 + 1] += vy * s * dt;
        p[i * 3 + 2] += vz * s * dt;
        if (Math.abs(p[i * 3]) > range) p[i * 3] = -Math.sign(p[i * 3]) * range;
        if (Math.abs(p[i * 3 + 1]) > range) p[i * 3 + 1] = -Math.sign(p[i * 3 + 1]) * range;
        if (p[i * 3 + 2] > range || p[i * 3 + 2] < -range) p[i * 3 + 2] = 0;
      }
      geometry.attributes.position.needsUpdate = true;
    },
  };
}

let SPRITE = null;

/* --- the room ---------------------------------------------------------- */

class Room3D {
  constructor(host) {
    this.host = host;
    this.devices = new Map();
    this.muted = new Set();
    this.lights = 0;
    this.last = 0;
    this.width = 0;
    this.height = 0;
    this.state = {};
    this.build();
  }

  build() {
    this.host.textContent = '';
    if (!SPRITE) SPRITE = softSprite();

    const scene = new THREE.Scene();
    scene.background = new THREE.Color(0x05070b);
    /* Distance haze, which is what stops a big empty box reading as flat. */
    scene.fog = new THREE.Fog(0x05070b, 6, 20);
    this.scene = scene;

    const camera = new THREE.PerspectiveCamera(46, 16 / 9, 0.1, 100);
    camera.position.set(3.8, 2.3, 9.2);
    this.camera = camera;

    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
    renderer.setPixelRatio(Math.min(globalThis.devicePixelRatio || 1, 2));
    /* Filmic tone mapping earns its place here: a bright event spike is meant
     * to be brighter than the soft wash can go, and without it every spike
     * clips to the same white and the two ambilight layers look identical. */
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = 1.05;
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.renderer = renderer;
    this.host.appendChild(renderer.domElement);

    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.set(0, 1.25, 2.6);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.minDistance = 1.5;
    controls.maxDistance = 18;
    /* Stop just short of the floor plane. Orbiting under the room shows you
     * the underside of a box and loses the horizon that makes it readable. */
    controls.maxPolarAngle = Math.PI * 0.495;
    controls.update();
    this.controls = controls;

    this.buildShell();
    this.resize();

    /* Damping and particle drift need frames of their own. update() also
     * renders directly, so the view is still correct if this never runs. */
    this.running = true;
    const loop = () => {
      if (!this.running) return;
      globalThis.requestAnimationFrame(loop);
      this.frame();
    };
    globalThis.requestAnimationFrame(loop);
  }

  buildShell() {
    const scene = this.scene;

    /* One inside-out box is the whole room. BackSide means the camera sees the
     * far walls and never the near ones, so the room is never occluded by the
     * wall you are looking through. */
    const shell = new THREE.Mesh(
      new THREE.BoxGeometry(ROOM_W, ROOM_H, ROOM_D),
      new THREE.MeshStandardMaterial({ color: 0x161a21, roughness: 0.95, metalness: 0, side: THREE.BackSide })
    );
    shell.position.set(0, ROOM_H / 2, ROOM_D / 2);
    scene.add(shell);

    const floor = new THREE.Mesh(
      new THREE.PlaneGeometry(ROOM_W, ROOM_D),
      new THREE.MeshStandardMaterial({ color: 0x0d1015, roughness: 0.8 })
    );
    floor.rotation.x = -Math.PI / 2;
    floor.position.set(0, 0.002, ROOM_D / 2);
    scene.add(floor);

    /* The screen is the ambient preview. It takes the colour of whatever the
     * soft layer is driving, which is what an Ambilight looks like from a
     * seat, and it is also the brightest thing in the room by default so the
     * space has an obvious front. */
    this.screen = new THREE.Mesh(
      new THREE.PlaneGeometry(ROOM_W * 0.66, ROOM_H * 0.42),
      new THREE.MeshBasicMaterial({ color: 0x1b2733 })
    );
    this.screen.position.set(0, 1.55, 0.03);
    scene.add(this.screen);

    this.screenGlow = new THREE.PointLight(0x30506e, 2.2, 12, 2);
    this.screenGlow.position.set(0, 1.55, 0.6);
    scene.add(this.screenGlow);

    scene.add(new THREE.AmbientLight(0x30384a, 0.5));
    scene.add(new THREE.HemisphereLight(0x50607a, 0x0a0c10, 0.5));

    /* The seat, and the thing that moves. A chair is enough geometry to read
     * pitch and roll; anything more detailed is decoration. */
    const seat = new THREE.Group();
    const cushion = new THREE.Mesh(
      new THREE.BoxGeometry(0.92, 0.16, 0.86),
      new THREE.MeshStandardMaterial({ color: 0x3a2f2c, roughness: 0.85 })
    );
    cushion.position.y = 0.45;
    seat.add(cushion);
    const back = new THREE.Mesh(
      new THREE.BoxGeometry(0.92, 0.72, 0.16),
      new THREE.MeshStandardMaterial({ color: 0x3a2f2c, roughness: 0.85 })
    );
    back.position.set(0, 0.85, 0.4);
    seat.add(back);
    seat.position.set(0, 0, SEAT_Z);
    scene.add(seat);
    this.seat = seat;
  }

  setMuted(muted) {
    this.muted = muted;
  }

  /* Lay out the devices a rig declares. Structure is built once here and only
   * material values change per frame, for the same reason the flat view does
   * it: rebuilding the scene graph every tick is how a preview becomes a
   * slideshow. */
  setInstruments(instruments) {
    for (const [, d] of this.devices) {
      this.scene.remove(d.group);
      disposeTree(d.group);
      if (d.light) this.scene.remove(d.light);
    }
    this.devices.clear();
    this.lights = 0;

    for (const inst of instruments || []) {
      const build = BUILDERS[inst.kind] || BUILDERS.shake;
      const device = build(0xffffff);
      const [x, y, z] = inst.position || [0, 0, 0];
      device.group.position.set(x, y, z);

      /* Emitters aim at the seat. A fan bolted to the back wall blowing at the
       * wall behind it would be drawn exactly that way otherwise, and it is
       * the kind of rig mistake this view exists to make obvious. */
      if (inst.kind === 'wind') {
        device.group.lookAt(new THREE.Vector3(0, 1.0, SEAT_Z));
      }

      let light = null;
      if (inst.kind === 'light' && this.lights < MAX_LIGHTS) {
        light = new THREE.PointLight(0xffffff, 0, 9, 2);
        light.position.set(x, y, z);
        this.scene.add(light);
        this.lights++;
        if (device.attach) device.attach(light);
      }

      this.scene.add(device.group);
      this.devices.set(inst.id, { group: device.group, apply: device.apply, light: light, kind: inst.kind });
    }
  }

  /* Draw one moment. Renders immediately rather than waiting for the animation
   * loop, so scrubbing the timeline updates the room even when the page is not
   * being given frames. */
  update(state) {
    this.state = state || {};
    this.frame();
  }

  frame() {
    const now = (globalThis.performance && globalThis.performance.now()) || 0;
    /* Clamped: a backgrounded tab returns and hands you a two second delta,
     * which would teleport every particle out of its box at once. */
    const dt = this.last ? Math.min((now - this.last) / 1000, 0.1) : 0.016;
    this.last = now;

    this.resize();

    const state = this.state;
    let ambient = null;

    for (const [id, device] of this.devices) {
      const { level, params, muted } = readDevice(state, id, this.muted);
      device.apply(level, params, dt);

      /* A muted device is shrunk, not hidden and not dimmed.
       *
       * Silence already comes from deviceState, which reports level 0 for a
       * muted device, so everything here has to do is say *why* it is quiet —
       * otherwise a muted device and an idle one look identical. Shrinking is
       * the right tool because it is idempotent: it can be recomputed every
       * frame from the mute set alone. The obvious alternative, scaling down
       * the materials' opacity, is not — it multiplies against last frame's
       * value, and any material apply() does not reset walks to zero and
       * never comes back. It did.
       */
      device.group.scale.setScalar(muted ? 0.55 : 1);

      if (device.kind === 'light' && level > (ambient ? ambient.level : 0)) {
        ambient = { colour: colourOf(params), level: level };
      }
    }

    const pose = readSeat(this.state);
    /* Scaled down: real platform travel is centimetres, and centimetres at
     * room scale is a seat that appears not to move at all. */
    this.seat.position.set(pose.sway * 0.5, pose.heave * 0.5, SEAT_Z + pose.surge * 0.5);
    this.seat.rotation.set(pose.pitch * 0.6, pose.yaw * 0.6, pose.roll * 0.6);

    if (ambient) {
      const c = new THREE.Color(ambient.colour);
      this.screen.material.color.copy(c).multiplyScalar(0.35 + ambient.level * 0.65);
      this.screenGlow.color.copy(c);
      this.screenGlow.intensity = 1.2 + ambient.level * 7;
    }

    if (this.controls) this.controls.update();
    this.renderer.render(this.scene, this.camera);
  }

  /* Sized from the host on every frame rather than from a resize event.
   * ResizeObserver is the right tool and is not reliably delivered in every
   * context this runs in; comparing two integers is cheap enough that not
   * depending on an event is the better trade. */
  resize() {
    const w = this.host.clientWidth || 640;
    const h = this.host.clientHeight || 360;
    if (w === this.width && h === this.height) return;
    this.width = w;
    this.height = h;
    this.camera.aspect = w / h;
    this.camera.updateProjectionMatrix();
    this.renderer.setSize(w, h, false);
  }

  /* Giving up the GPU context matters: browsers allow a small number of live
   * WebGL contexts per page, and toggling between views a dozen times without
   * this would lose the oldest one and blank the canvas. */
  dispose() {
    this.running = false;
    if (this.controls) this.controls.dispose();
    for (const [, d] of this.devices) disposeTree(d.group);
    disposeTree(this.scene);
    this.renderer.dispose();
    if (this.renderer.domElement.parentNode) {
      this.renderer.domElement.parentNode.removeChild(this.renderer.domElement);
    }
  }
}

function disposeTree(root) {
  root.traverse((o) => {
    if (o.geometry) o.geometry.dispose();
    if (o.material) {
      const list = Array.isArray(o.material) ? o.material : [o.material];
      for (const m of list) m.dispose();
    }
  });
}

/* --- availability ------------------------------------------------------ */

/* Ask the browser, do not assume. WebGL is missing on more machines than the
 * "everything has a GPU" reflex suggests: remote sessions, blocklisted
 * drivers, headless contexts, and a browser that has already lost too many
 * contexts. All of those get the flat view, which works. */
function webglAvailable() {
  try {
    const canvas = document.createElement('canvas');
    return !!(canvas.getContext('webgl2') || canvas.getContext('webgl'));
  } catch (err) {
    return false;
  }
}

if (webglAvailable()) {
  globalThis.Room3D = Room3D;
  globalThis.dispatchEvent(new Event('componium-room3d'));
}
