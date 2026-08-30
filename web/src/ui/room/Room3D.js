/* The room, in three.js.
 *
 * The flat CSS view answers "which device is doing what". This one answers a
 * question the flat view cannot: "what would that feel like from the seat".
 * Wind has a direction and a spread. Fog pools on the floor and drifts. A
 * light spike throws colour onto the walls behind you and glints off the
 * furniture. None of that is legible as a marker on a plan, and all of it is
 * what a contributor needs to see before trusting a cue to a machine that
 * moves a person.
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

import * as THREE from 'three';
import { containScale, aspectOf, SCREEN_ASPECT } from '../../core/picture';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import { RoomEnvironment } from 'three/examples/jsm/environments/RoomEnvironment.js';
import { deviceState, seatPose, cssColour } from './readings';

const ROOM_W = 5.0;
const ROOM_H = 3.0;
const ROOM_D = 6.0;
const SEAT_Z = 3.4;

/* Above this, a device is drawn as doing something. Below it, the cue is
 * either finished or too weak to matter, and drawing it anyway makes an idle
 * room look busy. Same threshold as the flat view, on purpose. */
const ON = 0.02;

/* Real point lights are the expensive part of this scene, and past a handful
 * they stop adding information: eight coloured sources already wash the room.
 * Beyond the cap a light still glows and still drives the ambient wash, it
 * just does not cast. */
const MAX_LIGHTS = 8;

/* Exposure at the middle of the brightness slider. */
const BASE_EXPOSURE = 1.12;

/* One reading of what a cue means, shared with everything else that asks.
 * The old studio put these on the global scope; they are a module now. */
const readDevice = deviceState;
const readSeat = seatPose;
const colourOf = cssColour;

let SPRITE = null;

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

function surface(colour, roughness, metalness, envIntensity) {
  return new THREE.MeshStandardMaterial({
    color: colour,
    roughness: roughness,
    metalness: metalness === undefined ? 0.05 : metalness,
    envMapIntensity: envIntensity === undefined ? 1 : envIntensity,
  });
}

function box(w, h, d, material) {
  return new THREE.Mesh(new THREE.BoxGeometry(w, h, d), material);
}

function place(mesh, x, y, z) {
  mesh.position.set(x, y, z);
  return mesh;
}

/* --- device builders --------------------------------------------------- */

/* Each builder returns { group, apply(level, params, dt) }. The apply function
 * is called every frame with that device's current level, and owns everything
 * that moves. Adding a kind means adding one entry here and nothing else. */
const BUILDERS = {
  light() {
    const group = new THREE.Group();
    const colour = 0xffffff;

    const bulb = new THREE.Mesh(
      new THREE.SphereGeometry(0.09, 16, 12),
      new THREE.MeshBasicMaterial({ color: colour })
    );
    group.add(bulb);

    /* The glow is a camera-facing sprite, not a sphere.
     *
     * A sphere with an additive material is a flat disc of constant colour: it
     * saturates to white and gives the glow a hard circular edge, which at any
     * real intensity looks like a hole cut in the picture rather than a bright
     * lamp. The sprite carries the same soft radial falloff the particles use,
     * so it reads as light instead of as geometry. */
    const halo = new THREE.Sprite(new THREE.SpriteMaterial({
      map: SPRITE, color: colour, transparent: true, opacity: 0,
      blending: THREE.AdditiveBlending, depthWrite: false,
    }));
    halo.scale.setScalar(0.25);
    group.add(halo);

    /* A wide soft cone, so a wall washer reads as throwing light somewhere
     * rather than floating. */
    const cone = new THREE.Mesh(new THREE.ConeGeometry(0.75, 1.9, 24, 1, true), glowMaterial(colour, 0));
    cone.position.y = -0.95;
    group.add(cone);

    let light = null;
    return {
      group: group,
      attach(l) { light = l; },
      apply(level, params) {
        const c = new THREE.Color(colourOf(params));
        halo.material.color.copy(c);
        cone.material.color.copy(c);
        /* The bulb never goes fully black: an off lamp is still a lamp, and a
         * device you cannot see is a device you cannot click. */
        bulb.material.color.copy(c).multiplyScalar(0.3 + level * 0.7).addScalar(0.1);
        halo.material.opacity = level * 0.3;
        cone.material.opacity = level * 0.2;
        halo.scale.setScalar(0.2 + level * 0.8);
        if (light) {
          light.color.copy(c).addScalar(0.05);
          light.intensity = level * 15;
          light.visible = level > ON;
        }
      },
    };
  },

  wind() {
    const group = new THREE.Group();
    const cone = new THREE.Mesh(new THREE.ConeGeometry(0.62, 2.6, 28, 1, true), glowMaterial(0xa8dcff, 0));
    /* ConeGeometry points up the y axis with its apex at the top. Rotating by
     * -90° about x aims it down -z, and the group is then turned to face the
     * couch, so the apex sits at the fan and the mouth opens toward it. */
    cone.rotation.x = -Math.PI / 2;
    cone.position.z = 1.3;
    group.add(cone);

    const streaks = particles(110, 0xbfe6ff, 0.075, 0.5, 0.5, 1.4);
    group.add(streaks.points);

    return {
      group: group,
      apply(level, params, dt) {
        cone.material.opacity = level * 0.26;
        cone.scale.set(0.7 + level * 0.5, 1, 0.55 + level * 1.0);
        streaks.material.opacity = level * 0.85;
        /* Speed reads as speed. The stream is what tells you whether this is a
         * breeze or a gust, and a static cone tells you neither. */
        streaks.drift(dt, 0, 0, 1.4 + level * 8.0, 2.6);
      },
    };
  },

  mist() {
    const group = new THREE.Group();
    const cloud = particles(260, 0xe6f2ff, 0.34, 1.3, 0.8, 1.3);
    group.add(cloud.points);
    return {
      group: group,
      apply(level, params, dt) {
        cloud.material.opacity = level * 0.3;
        /* Mist falls and spreads; it does not blow away. */
        cloud.drift(dt, 0, -0.26, 0.18, 1.8);
      },
    };
  },

  fog() {
    const group = new THREE.Group();
    /* Fog is the one effect that should look like it has volume rather than
     * like a cluster of dots: many large, very faint sprites, low and wide,
     * moving slowly. Small and bright reads as smoke from a machine; big and
     * dim reads as air you would have to walk through. */
    const cloud = particles(420, 0xd6e2f2, 1.05, 2.2, 0.4, 2.2);
    group.add(cloud.points);
    return {
      group: group,
      apply(level, params, dt) {
        cloud.material.opacity = level * 0.13;
        cloud.drift(dt, 0.07, -0.04, 0.13, 3.0);
      },
    };
  },

  water() {
    const group = new THREE.Group();
    const drops = particles(120, 0x9fe0ff, 0.055, 0.8, 0.25, 0.8);
    group.add(drops.points);
    return {
      group: group,
      apply(level, params, dt) {
        drops.material.opacity = level * 0.95;
        drops.drift(dt, 0, -1.9, 0.55, 1.3);
      },
    };
  },

  scent() {
    const group = new THREE.Group();
    const puff = particles(80, 0xe0c8ff, 0.16, 0.55, 0.55, 0.55);
    group.add(puff.points);
    return {
      group: group,
      apply(level, params, dt) {
        puff.material.opacity = level * 0.5;
        puff.drift(dt, 0, 0.16, 0.07, 2.2);
      },
    };
  },

  shake() {
    const group = new THREE.Group();
    const unit = box(0.3, 0.12, 0.3, new THREE.MeshStandardMaterial({
      color: 0xff9a5c, roughness: 0.4, metalness: 0.5, emissive: 0xff9a5c, emissiveIntensity: 0,
    }));
    group.add(unit);
    return {
      group: group,
      apply(level) {
        unit.material.emissiveIntensity = level * 1.4;
        const a = level * 0.055;
        unit.position.set(
          (Math.random() - 0.5) * a, (Math.random() - 0.5) * a, (Math.random() - 0.5) * a);
      },
    };
  },
};

/* The platform is not a device marker; it is the couch, handled separately. */
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

/* Where the camera stands before anybody moves it.
 *
 * Named, because it is now two things: the position the room opens at, and
 * the position "reset" means. Those have to stay the same value or resetting
 * lands somewhere the room has never been.
 */
export const HOME_VIEW = {
  pos: [3.4, 2.1, 9.4],
  target: [0, 1.2, 2.2],
};

/* --- the room ---------------------------------------------------------- */

export class Room3D {
  constructor(host) {
    this.host = host;
    this.devices = new Map();
    /* The film on the television, when someone has asked for it. Null is the
     * normal state: the screen's job is to preview the ambient layer. */
    this.picture = null;
    this.pictureTexture = null;
    this.muted = new Set();
    this.forced = new Map();
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
    scene.background = new THREE.Color(0x0d1017);
    this.scene = scene;

    const camera = new THREE.PerspectiveCamera(48, 16 / 9, 0.1, 100);
    camera.position.set(...HOME_VIEW.pos);
    this.camera = camera;

    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
    renderer.setPixelRatio(Math.min(globalThis.devicePixelRatio || 1, 2));
    /* Filmic tone mapping earns its place here: a bright event spike is meant
     * to be brighter than the soft wash can go, and without it every spike
     * clips to the same white and the two ambilight layers look identical. */
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = BASE_EXPOSURE;
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.renderer = renderer;
    this.host.appendChild(renderer.domElement);

    /* Image based lighting, generated rather than downloaded.
     *
     * This is what puts a highlight on the edge of the television and a sheen
     * on the floor, and it is why the furniture reads as objects rather than
     * as flat shapes. RoomEnvironment is a handful of emissive boxes that
     * three.js prefilters into an environment map; no HDR file, no download,
     * nothing to license. */
    const pmrem = new THREE.PMREMGenerator(renderer);
    this.environment = pmrem.fromScene(new RoomEnvironment(), 0.04);
    scene.environment = this.environment.texture;
    pmrem.dispose();

    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.set(...HOME_VIEW.target);
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
    this.buildFurniture();
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
    const shell = box(ROOM_W, ROOM_H, ROOM_D, new THREE.MeshStandardMaterial({
      color: 0x3b424e, roughness: 0.86, metalness: 0.02,
      side: THREE.BackSide, envMapIntensity: 0.55,
    }));
    place(shell, 0, ROOM_H / 2, ROOM_D / 2);
    scene.add(shell);

    /* The floor is its own mesh so it can be a little glossy without making
     * the walls into mirrors. A faint reflection is most of what stops a room
     * looking like a cardboard box, and it is also where a light cue shows up
     * second, after the wall it is pointed at. */
    const floor = new THREE.Mesh(
      new THREE.PlaneGeometry(ROOM_W, ROOM_D),
      surface(0x3b4250, 0.36, 0.22, 1.15)
    );
    floor.rotation.x = -Math.PI / 2;
    place(floor, 0, 0.002, ROOM_D / 2);
    scene.add(floor);

    const rug = new THREE.Mesh(new THREE.PlaneGeometry(3.7, 2.7), surface(0x3b3340, 0.97, 0));
    rug.rotation.x = -Math.PI / 2;
    place(rug, 0, 0.006, 2.5);
    scene.add(rug);

    /* Bright enough to read the room at a glance, which is the whole job.
     *
     * These numbers are far larger than most three.js examples on the web
     * suggest, because most of them predate r155, when lighting moved to
     * physical units and every intensity in the ecosystem changed meaning.
     * An earlier version of this file used the old figures and rendered a
     * black box that was, technically, a correct scene graph. */
    const ambient = new THREE.AmbientLight(0xaebbd0, 1.3);
    const hemi = new THREE.HemisphereLight(0xb8cbe4, 0x343a45, 1.3);
    scene.add(ambient);
    scene.add(hemi);
    this.fill = [ambient, hemi];

    /* Two ceiling sources. They exist as much for the specular highlights they
     * put on the television, the floor and the couch as for the light itself:
     * without a source to reflect, physically based materials look matte no
     * matter what their roughness says. */
    for (const x of [-1.35, 1.35]) {
      const lamp = new THREE.PointLight(0xfff0dc, 11, 13, 2);
      lamp.position.set(x, ROOM_H - 0.16, 2.6);
      scene.add(lamp);
      this.fill.push(lamp);
      const fitting = new THREE.Mesh(
        new THREE.SphereGeometry(0.07, 12, 10),
        new THREE.MeshBasicMaterial({ color: 0xfff4e4 })
      );
      place(fitting, x, ROOM_H - 0.14, 2.6);
      scene.add(fitting);
    }
  }

  buildFurniture() {
    const scene = this.scene;

    /* A big flat television, on a stand. The screen doubles as the ambient
     * light preview: it takes the colour the soft layer is driving, which is
     * what an Ambilight looks like from a seat, and it gives the room an
     * obvious front. */
    const tv = new THREE.Group();
    const bezel = box(3.34, 1.94, 0.09, surface(0x0f1216, 0.32, 0.65, 1.3));
    tv.add(bezel);
    this.screen = new THREE.Mesh(
      new THREE.PlaneGeometry(3.22, 1.82),
      new THREE.MeshBasicMaterial({ color: 0x9dc4e8 })
    );
    place(this.screen, 0, 0, 0.048);
    tv.add(this.screen);
    place(tv, 0, 1.62, 0.09);
    scene.add(tv);

    /* A soft glow behind the panel, so the wall around the television picks up
     * the wash rather than the screen floating on a dark wall. */
    this.screenGlow = new THREE.PointLight(0x4a7cad, 16, 17, 2);
    this.screenGlow.position.set(0, 1.62, 0.7);
    scene.add(this.screenGlow);

    const stand = new THREE.Group();
    stand.add(place(box(2.5, 0.44, 0.46, surface(0x272c36, 0.28, 0.4, 1.2)), 0, 0.24, 0));
    stand.add(place(box(2.58, 0.035, 0.52, surface(0x333a46, 0.16, 0.55, 1.4)), 0, 0.475, 0));
    for (const x of [-1.1, 1.1]) {
      stand.add(place(box(0.05, 0.06, 0.05, surface(0x1b1f26, 0.3, 0.7)), x, 0.03, 0));
    }
    place(stand, 0, 0, 0.38);
    scene.add(stand);

    /* The couch, and the thing that moves. Built from boxes rather than a
     * loaded model: a mesh file is a binary asset with a licence, a download
     * and a loader, and the point of this view is legibility, not upholstery.
     * Cushions are separate pieces mostly so the shape survives being tilted —
     * a single slab reads as a crate the moment the platform rolls. */
    const couch = new THREE.Group();
    const fabric = surface(0x474252, 0.92, 0.02, 0.7);
    const fabricLight = surface(0x554f61, 0.92, 0.02, 0.7);
    const leg = surface(0x23262d, 0.3, 0.65, 1.2);

    couch.add(place(box(2.7, 0.34, 1.08, fabric), 0, 0.34, 0));
    for (const x of [-0.66, 0.66]) {
      couch.add(place(box(1.28, 0.2, 0.98, fabricLight), x, 0.58, -0.02));
      couch.add(place(box(1.24, 0.58, 0.19, fabricLight), x, 0.82, 0.44));
    }
    couch.add(place(box(2.7, 0.8, 0.24, fabric), 0, 0.76, 0.54));
    for (const x of [-1.34, 1.34]) {
      couch.add(place(box(0.28, 0.34, 1.08, fabric), x, 0.66, 0));
    }
    for (const x of [-1.2, 1.2]) {
      for (const z of [-0.44, 0.44]) {
        couch.add(place(box(0.07, 0.17, 0.07, leg), x, 0.085, z));
      }
    }
    place(couch, 0, 0, SEAT_Z);
    scene.add(couch);
    this.seat = couch;
    this.seatRest = SEAT_Z;
  }

  /**
   * Show a film on the screen, or stop showing one.
   *
   * Takes the video element the picture pane is already using rather than
   * making a second one. There is only one film, one decode and one clock;
   * two would drift apart the moment either was scrubbed, and the drift would
   * be worst exactly where the room is most useful — on a cue you are trying
   * to place against a frame.
   *
   * Passing null puts the screen back to being the ambient preview.
   */
  setPicture(video) {
    if (this.picture === video) return;
    if (this.pictureTexture) {
      this.pictureTexture.dispose();
      this.pictureTexture = null;
    }
    this.picture = video || null;
    const material = this.screen.material;
    if (!this.picture) {
      material.map = null;
      material.needsUpdate = true;
      this.screen.scale.set(1, 1, 1);
      return;
    }
    const texture = new THREE.VideoTexture(this.picture);
    texture.colorSpace = THREE.SRGBColorSpace;
    /* VideoTexture turns mipmaps off, which is right for its usual job of
     * filling the viewport and wrong here. The screen is a few hundred pixels
     * of canvas showing a frame 1920 wide, so a drawn pixel covers around five
     * texels, and taking four of them is what makes fine detail crawl as the
     * camera moves. The renderer's own anti-aliasing cannot help: it samples
     * geometry edges, not the inside of a texture.
     *
     * Anisotropy is the half that matters when the screen is seen from a seat
     * rather than square on, and it does nothing without the mip chain to
     * sample along — the two go together or neither is worth setting. */
    texture.generateMipmaps = true;
    texture.minFilter = THREE.LinearMipmapLinearFilter;
    texture.magFilter = THREE.LinearFilter;
    texture.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
    this.pictureTexture = texture;
    material.map = texture;
    material.needsUpdate = true;
  }

  setMuted(muted) {
    this.muted = muted;
  }

  /* How brightly the room itself is lit, 0 to 1, with 0.5 meaning the level
   * the room was built at.
   *
   * Only the fill lighting moves — the ambient, the sky and the two ceiling
   * fittings. The lamps a cue drives and the wash off the television are left
   * exactly where the score put them, because those are the thing being
   * previewed: scaling them with this slider would make the preview agree with
   * whatever brightness happened to be selected, which is the one property it
   * must not have. Turning the room down therefore does not dim an effect, it
   * makes the effect the brightest thing in the picture, which is what a dark
   * scene actually looks like.
   *
   * The curve is exponential rather than linear because perceived brightness
   * is: a linear slider spends most of its travel in a range that all looks
   * the same and then falls off a cliff at the end. */
  setBrightness(v) {
    const level = Math.max(0, Math.min(1, Number(v)));
    this.brightness = level;
    const factor = Math.pow(16, level - 0.5);
    for (const light of this.fill || []) {
      if (light.baseIntensity === undefined) light.baseIntensity = light.intensity;
      light.intensity = light.baseIntensity * factor;
    }
    /* Exposure moves too, but far less. It affects the cue-driven lights as
     * well, so leaning on it would break the rule above; a little of it stops
     * the two ends of the slider looking flat. */
    this.renderer.toneMappingExposure = BASE_EXPOSURE * Math.pow(2, level - 0.5);
    this.frame();
  }

  /* Forced levels: id -> 0..1, overriding whatever the score says. See the
   * force panel in app.js. */
  setForced(forced) {
    this.forced = forced || new Map();
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
      const device = build();
      const [x, y, z] = inst.position || [0, 0, 0];
      device.group.position.set(x, y, z);

      /* Emitters aim at the couch. A fan bolted to the back wall blowing at
       * the wall behind it would be drawn exactly that way otherwise, and it
       * is the kind of rig mistake this view exists to make obvious. */
      if (inst.kind === 'wind') {
        device.group.lookAt(new THREE.Vector3(0, 1.0, SEAT_Z));
      }

      let light = null;
      if (inst.kind === 'light' && this.lights < MAX_LIGHTS) {
        light = new THREE.PointLight(0xffffff, 0, 11, 2);
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
      const { level, params, muted } = readDevice(state, id, this.muted, this.forced);
      device.apply(level, params, dt);

      /* A muted device is shrunk, not hidden and not dimmed.
       *
       * Silence already comes from deviceState, which reports level 0 for a
       * muted device, so all this has to do is say *why* it is quiet —
       * otherwise a muted device and an idle one look identical. Shrinking is
       * the right tool because it is idempotent: it can be recomputed every
       * frame from the mute set alone. The obvious alternative, scaling down
       * the materials' opacity, is not — it multiplies against last frame's
       * value, and any material apply() does not reset walks to zero and never
       * comes back. It did.
       */
      device.group.scale.setScalar(muted ? 0.55 : 1);

      if (device.kind === 'light' && level > (ambient ? ambient.level : 0)) {
        ambient = { colour: colourOf(params), level: level };
      }
    }

    const pose = readSeat(this.state, this.forced, now);
    /* Scaled down: real platform travel is centimetres, and centimetres at
     * room scale is a couch that appears not to move at all. */
    this.seat.position.set(pose.sway * 0.5, pose.heave * 0.5, this.seatRest + pose.surge * 0.5);
    this.seat.rotation.set(pose.pitch * 0.6, pose.yaw * 0.6, pose.roll * 0.6);

    /* The picture, if there is one.
     *
     * Only the fit is maintained here. Marking the texture dirty is
     * VideoTexture's own job and it already does it properly: it registers
     * requestVideoFrameCallback, which fires on every presented frame
     * including a seek completed while paused, and falls back to readyState
     * where that callback does not exist. Marking it again every render
     * uploaded an unchanged frame sixty times a second, and now that there is
     * a mip chain it would rebuild that too. */
    if (this.picture && this.pictureTexture) {
      const fit = containScale(SCREEN_ASPECT, aspectOf(this.picture));
      this.screen.scale.set(fit.x, fit.y, 1);
    }

    if (ambient) {
      /* Both of these have a floor, and the floor is not cosmetic.
       *
       * The screen shows the soft ambilight layer's own colour, which during a
       * dark scene is very nearly black — correctly so. But the screen is also
       * much of the light in the room, and a room lit by a black screen loses
       * the geometry, and with it any way to see where the devices are or what
       * the couch is doing. A real television is never truly black either. So
       * the wash bottoms out somewhere dim rather than at nothing. */
      const c = new THREE.Color(ambient.colour);
      /* With a film on it the screen carries its own light and must not be
       * tinted by the layer it is no longer previewing — a white multiplier
       * leaves the frame as shot. The glow behind the panel keeps doing the
       * ambient job, so the room still shows what the soft layer is up to. */
      if (this.picture) {
        this.screen.material.color.setScalar(1);
      } else {
        this.screen.material.color.copy(c)
          .multiplyScalar(0.35 + ambient.level * 0.65).addScalar(0.09);
      }
      this.screenGlow.color.copy(c).addScalar(0.18);
      this.screenGlow.intensity = 9 + ambient.level * 30;
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

  /* --- where the camera is standing ------------------------------------ */

  /**
   * Report the camera whenever it moves.
   *
   * OrbitControls fires "change" per frame of a drag and once more as the
   * damping settles, so this is chatty by design; smoothing it is the caller's
   * problem, because only the caller knows what it is going to do with it.
   */
  onView(fn) {
    this.viewListener = fn;
    if (this.controls && !this.viewWired) {
      this.viewWired = true;
      this.controls.addEventListener('change', () => {
        if (this.viewListener) this.viewListener(this.getView());
      });
    }
  }

  /** Where the camera is, in the shape setView takes back. */
  getView() {
    const p = this.camera.position;
    const t = this.controls.target;
    /* Rounded, because this is written to storage on a timer and six decimal
     * places of a camera position is noise that makes every write different. */
    const r = (v) => Math.round(v * 1000) / 1000;
    return { pos: [r(p.x), r(p.y), r(p.z)], target: [r(t.x), r(t.y), r(t.z)] };
  }

  /**
   * Put the camera somewhere. Null means home.
   *
   * The two update() calls around the move are the whole trick, and they are
   * not defensive: OrbitControls carries the rest of a drag as momentum, and
   * that momentum is still pending when a viewport is recalled a moment later.
   * Setting the position and handing control back would apply the leftover
   * rotation from the new position, and the camera would slide off to
   * somewhere it was never asked to be — measured at around sixty degrees
   * away, which looks like the recall picking a different angle rather than
   * like drift.
   *
   * So: turn damping off, update once to spend and clear whatever is pending,
   * then move, then update again from a standstill.
   */
  setView(view) {
    const want = view || HOME_VIEW;
    if (!Array.isArray(want.pos) || !Array.isArray(want.target)) return;
    const damping = this.controls.enableDamping;
    this.controls.enableDamping = false;
    this.controls.update();
    this.camera.position.set(want.pos[0], want.pos[1], want.pos[2]);
    this.controls.target.set(want.target[0], want.target[1], want.target[2]);
    this.controls.update();
    this.controls.enableDamping = damping;
    /* Draw once now rather than waiting for the loop. The loop will come
     * round on its own, but a preset that takes a frame to land looks like a
     * click that did not register. */
    this.renderer.render(this.scene, this.camera);
  }

  dispose() {
    this.running = false;
    if (this.controls) this.controls.dispose();
    for (const [, d] of this.devices) disposeTree(d.group);
    disposeTree(this.scene);
    if (this.pictureTexture) this.pictureTexture.dispose();
    if (this.environment) this.environment.texture.dispose();
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

/* Ask the browser, do not assume. WebGL is missing on more machines than the
 * "everything has a GPU" reflex suggests: remote sessions, blocklisted
 * drivers, headless contexts, and a browser that has already lost too many
 * contexts. All of those get told so, rather than shown an empty box. */
export function webglAvailable() {
  try {
    const canvas = document.createElement('canvas');
    return !!(canvas.getContext('webgl2') || canvas.getContext('webgl'));
  } catch (err) {
    return false;
  }
}

