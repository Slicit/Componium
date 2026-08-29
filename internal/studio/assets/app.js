/* Componium studio.
 *
 * Three views of one score: the film, the timeline, and the room. All three
 * follow a single clock, which is the video when there is one and a synthetic
 * transport when there is not, because previewing a score without having the
 * film to hand is a normal thing to want.
 */

'use strict';

const el = (id) => document.getElementById(id);

let score = null;
let rig = null;
let room = null;
let timeline = null;
let dirty = false;
let selected = null;

/* Which instruments are silenced in the preview. One set, shared by the
 * timeline and the room: a toggle that affected only one of them would be
 * telling you something untrue about the other. */
const muted = new Set();

function applyMuted() {
  if (room) room.setMuted(muted);
  if (timeline) timeline.setMuted(muted);
}

function toggleMuted(id, off) {
  if (off) muted.add(id); else muted.delete(id);
  applyMuted();
}

/* --- forcing a device by hand -------------------------------------------
 *
 * One slider per instrument, overriding whatever the score says.
 *
 * This is the answer to "what does 40% of that fan actually look like", which
 * until now could only be found by hunting for a cue that happened to be that
 * strong, or by editing the score and undoing it afterwards. It is a preview
 * control and nothing else: it never touches the score, it is not saved, and
 * it does not survive a reload.
 *
 * Zero means release, not force-off. A slider cannot express "no opinion" on
 * its own, and rather than bolt a checkbox onto every row, the bottom of the
 * travel hands the device back to the score. Force-off already exists and is
 * called mute.
 */
const forced = new Map();

function applyForced() {
  if (room && room.setForced) room.setForced(forced);
}

function buildForcePanel() {
  const host = el('force');
  if (!host) return;
  const list = (rig && rig.instruments) || [];
  host.textContent = '';
  host.hidden = list.length === 0;
  if (!list.length) return;

  const head = document.createElement('div');
  head.className = 'force-head';
  const title = document.createElement('span');
  title.className = 'muted small';
  title.textContent = 'Force a device, 0 releases it back to the score';
  head.appendChild(title);
  const clear = document.createElement('button');
  clear.type = 'button';
  clear.className = 'small-btn';
  clear.textContent = 'Release all';
  clear.addEventListener('click', function () {
    forced.clear();
    applyForced();
    buildForcePanel();
  });
  head.appendChild(clear);
  host.appendChild(head);

  for (const inst of list) {
    const row = document.createElement('div');
    row.className = 'force-row';

    const name = document.createElement('span');
    name.className = 'force-name';
    name.textContent = inst.id;
    name.title = inst.kind;
    row.appendChild(name);

    const slider = document.createElement('input');
    slider.type = 'range';
    slider.min = '0';
    slider.max = '100';
    slider.step = '1';
    slider.value = String(Math.round((forced.get(inst.id) || 0) * 100));
    row.appendChild(slider);

    const readout = document.createElement('span');
    readout.className = 'force-value';
    row.appendChild(readout);

    const paint = function () {
      const v = Number(slider.value);
      readout.textContent = v > 0 ? v + '%' : 'auto';
      row.classList.toggle('forcing', v > 0);
    };
    slider.addEventListener('input', function () {
      const v = Number(slider.value);
      if (v > 0) forced.set(inst.id, v / 100); else forced.delete(inst.id);
      applyForced();
      paint();
    });
    paint();

    host.appendChild(row);
  }
}

/* --- which room is on screen ------------------------------------------
 *
 * Two implementations of one interface: Room draws in CSS and always works,
 * Room3D draws in WebGL and appears only once its module has loaded and found
 * a GPU. Swapping is a matter of building the other one into the same host and
 * handing it the same instruments and mutes, because they take the same four
 * calls. The choice is remembered, and it is a preference rather than a
 * capability: asking for 3D on a machine that cannot do it gets the flat view
 * and a note saying why, not an empty box.
 */
let roomKind = null;

function preferredRoom() {
  try {
    return localStorage.getItem('componium.room') || '3d';
  } catch (err) {
    return '3d';
  }
}

function useRoomView(want) {
  const kind = (want === '3d' && globalThis.Room3D) ? '3d' : 'flat';
  if (room && kind === roomKind) return;

  /* Hand back the WebGL context before taking another. Browsers cap how many
   * a page may hold, and toggling without this eventually blanks the canvas. */
  if (room && room.dispose) room.dispose();

  room = kind === '3d' ? new globalThis.Room3D(el('room')) : new Room(el('room'));
  roomKind = kind;
  room.setInstruments((rig && rig.instruments) || []);
  room.setMuted(muted);
  if (room.setForced) room.setForced(forced);

  try { localStorage.setItem('componium.room', want); } catch (err) { /* private mode */ }
  paintRoomToggle(want);
  applyLumen(preferredLumen());
}

/* Room brightness, 0 to 100, remembered like the view choice.
 *
 * Only the 3D room has any lighting to adjust, so the control hides itself in
 * the flat view rather than sitting there doing nothing. */
function preferredLumen() {
  try {
    /* Test for the missing key before converting. Number(null) is 0, which is
     * a perfectly valid brightness, so a plain Number() conversion turns
     * "nothing stored" into "turn the lights off" — and the room comes up
     * black on a first visit for a reason that looks like a rendering bug. */
    const stored = localStorage.getItem('componium.lumen');
    if (stored === null || stored === '') return 50;
    const v = Number(stored);
    return Number.isFinite(v) && v >= 0 && v <= 100 ? v : 50;
  } catch (err) {
    return 50;
  }
}

function applyLumen(v) {
  const slider = el('room-lumen');
  if (slider && slider.parentNode) {
    slider.parentNode.hidden = roomKind !== '3d';
  }
  if (room && room.setBrightness) room.setBrightness(v / 100);
  try { localStorage.setItem('componium.lumen', String(v)); } catch (err) { /* private mode */ }
}

function paintRoomToggle(want) {
  const a = el('room-3d');
  const b = el('room-flat');
  if (a && a.classList) a.classList.toggle('on', roomKind === '3d');
  if (b && b.classList) b.classList.toggle('on', roomKind === 'flat');
  const note = el('room-note');
  if (!note) return;
  /* Deliberately vague about the cause, because this genuinely does not know
   * it. Room3D is absent both when the browser has no WebGL and when the
   * module failed to load at all — a missing import map entry did exactly that
   * once, and the page confidently blamed the GPU. */
  note.textContent = (want === '3d' && roomKind === 'flat')
    ? '3D is not available here, showing the flat room'
    : '';
}

/* Solo is a toggle, not a mode. Pressing it on the only audible track
 * restores everything, so the same button gets you in and out. */
function soloOnly(id) {
  const all = (score.tracks || []).map(function (t) { return t.instrument; });
  const alreadyAlone = all.every(function (other) {
    return other === id ? !muted.has(other) : muted.has(other);
  });
  muted.clear();
  if (!alreadyAlone) {
    for (const other of all) if (other !== id) muted.add(other);
  }
  applyMuted();
}

/* --- transport ---------------------------------------------------------- */

/* Stands in for the video when there is no film loaded. Same three members, so
 * nothing downstream has to know which one it is talking to. */
const synthetic = {
  t: 0, playing: false, last: 0,
  get currentTime() { return this.t; },
  set currentTime(v) { this.t = Math.max(0, v); },
  play() { this.playing = true; this.last = performance.now(); },
  pause() { this.playing = false; },
  get paused() { return !this.playing; },
  tick() {
    if (!this.playing) return;
    const now = performance.now();
    this.t += (now - this.last) / 1000;
    this.last = now;
  },
};

let transport = synthetic;

function duration() {
  if (transport !== synthetic && transport.duration) return transport.duration;
  return (score && score.duration) || 60;
}

/* --- rendering ---------------------------------------------------------- */

function renderActive(state) {
  const host = el('active');
  /* A forced device is not in the score, so it would otherwise be doing
   * something visible in the room while this line said nothing was playing.
   * Forced chips are marked as such: the room is showing you something the
   * film will not do. */
  const on = Object.keys(state)
    .filter((id) => state[id].active && state[id].level > 0.02 && !forced.has(id));
  host.textContent = '';

  if (on.length === 0 && forced.size === 0) {
    const none = document.createElement('span');
    none.className = 'muted';
    none.textContent = 'nothing playing';
    host.appendChild(none);
    return;
  }

  for (const id of Array.from(forced.keys()).sort()) {
    const chip = document.createElement('span');
    chip.className = 'chip forced';
    chip.textContent = id + ' forced ' + (forced.get(id) * 100).toFixed(0) + '%';
    host.appendChild(chip);
  }
  for (const id of on.sort()) {
    const s = state[id];
    const chip = document.createElement('span');
    chip.className = 'chip';
    chip.textContent = id + ' ' + (s.action || 'set') + ' ' + (s.level * 100).toFixed(0) + '%';
    host.appendChild(chip);
  }
}

function frame() {
  if (transport === synthetic) synthetic.tick();
  const t = transport.currentTime || 0;

  if (score) {
    const state = evaluate(score, t);
    if (room) room.update(state);
    if (timeline) timeline.setTime(t);
    el('clock').textContent = fmt(t);
    renderActive(state);
  }
  requestAnimationFrame(frame);
}

/* --- inspector ---------------------------------------------------------- */

function markDirty() {
  dirty = true;
  el('save').disabled = false;
  el('status').textContent = 'unsaved changes';
}

function showInspector(track, cue, ti, index) {
  selected = { track: track, cue: cue, ti: ti, index: index };
  el('inspector').hidden = false;
  el('insp-title').textContent = track.instrument + ' · ' + cue.action;
  el('insp-t').value = fmt(cue.t);
  el('insp-dur').value = cue.duration || 0;
  el('insp-src').textContent = cue.source || 'hand written';

  const fields = el('insp-fields');
  fields.textContent = '';
  for (const key of Object.keys(cue.params || {})) {
    const wrap = document.createElement('label');
    wrap.textContent = key;
    const input = document.createElement('input');
    input.type = 'text';
    input.value = cue.params[key];
    input.addEventListener('change', function () {
      const n = Number(input.value);
      if (Number.isFinite(n)) { cue.params[key] = n; markDirty(); }
    });
    wrap.appendChild(input);
    fields.appendChild(wrap);
  }
}

/* --- loading and saving ------------------------------------------------- */

async function load() {
  const [scoreRes, rigRes] = await Promise.all([fetch('/api/score'), fetch('/api/rig')]);
  if (!scoreRes.ok) { el('status').textContent = 'could not load the score'; return; }
  score = await scoreRes.json();
  rig = rigRes.ok ? await rigRes.json() : { instruments: [] };

  el('title').textContent = score.title || '(untitled)';
  el('rigname').textContent = rig.name || '';
  el('status').textContent = 'loaded';

  useRoomView(preferredRoom());
  buildForcePanel();

  timeline = new Timeline(el('timeline'), {
    onSeek: function (t) { transport.currentTime = t; },
    onSelect: showInspector,
    onToggle: toggleMuted,
    onSolo: soloOnly,
    /* Dragging an event on the timeline edits the score exactly as typing in
     * the inspector does, so it has to arm Save the same way. */
    onEdit: markDirty,
  });
  timeline.setScore(score, duration());

  if (rig.hasMedia) await loadMediaList();
  await loadLibrary();
}

/* The picker. Pointing the studio at a directory rather than a single file
 * is what makes this useful: choose a film without restarting anything. */
async function loadMediaList() {
  const res = await fetch("/api/media");
  if (!res.ok) return;
  const files = await res.json();
  const picker = el("media-picker");
  picker.textContent = "";
  if (!files || files.length === 0) { picker.hidden = true; return; }

  for (const f of files) {
    const option = document.createElement("option");
    option.value = f.name;
    option.textContent = f.name + "  (" + megabytes(f.size) + ")";
    picker.appendChild(option);
  }
  picker.hidden = false;
  picker.addEventListener("change", function () { openFilm(picker.value); });
  attachMedia(files[0].name);
}

function megabytes(bytes) {
  return (bytes / (1024 * 1024)).toFixed(0) + " MB";
}

function attachMedia(name) {
  const video = el('video');
  video.src = '/media?file=' + encodeURIComponent(name);
  video.hidden = false;
  el('no-media').hidden = true;

  video.onloadedmetadata = function () {
    transport = video;
    timeline.setScore(score, duration());
  };

  /* A file the browser cannot decode is common with mkv, and silently showing
   * a black rectangle would be worse than saying so. */
  video.onerror = function () {
    video.hidden = true;
    transport = synthetic;
    const note = el('no-media');
    note.hidden = false;
    note.textContent =
      'this browser cannot decode ' + name + '. Press Prepare next to it in ' +
      'the library to build a copy that plays here — usually quick, because ' +
      'the video is only re-encoded when it has to be. Meanwhile the timeline ' +
      'and room still work, driven by the score alone.';
  };
}

async function save() {
  el('status').textContent = 'saving';
  const res = await fetch('/api/score', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(score),
  });
  const body = await res.json().catch(function () { return {}; });
  if (!res.ok) {
    /* The server validates by round tripping through the real parser, so a
     * refusal here is the same refusal componium play would give. */
    el('status').textContent = 'refused: ' + (body.error || res.status);
    return;
  }
  dirty = false;
  el('save').disabled = true;
  el('status').textContent = 'saved, ' + body.cues + ' cues';
}

/* --- wiring ------------------------------------------------------------- */

function parseTime(text) {
  text = String(text).trim();
  if (!text) return null;
  if (text.indexOf(':') === -1) {
    const n = Number(text);
    return Number.isFinite(n) && n >= 0 ? n : null;
  }
  let total = 0;
  for (const part of text.split(':')) {
    const v = Number(part);
    if (!Number.isFinite(v) || v < 0) return null;
    total = total * 60 + v;
  }
  return total;
}

el('save').addEventListener('click', save);

el('room-lumen').value = String(preferredLumen());
el('room-lumen').addEventListener('input', function () {
  applyLumen(Number(el('room-lumen').value));
});

el('room-3d').addEventListener('click', function () { useRoomView('3d'); });
el('room-flat').addEventListener('click', function () { useRoomView('flat'); });

/* room3d.js is a module and therefore always arrives after this script has
 * run. Whichever order it lands in relative to load(), the flat room is on
 * screen first and this upgrades it in place. */
window.addEventListener('componium-room3d', function () {
  if (room && preferredRoom() === '3d') useRoomView('3d');
});

el('play').addEventListener('click', function () {
  if (transport.paused) transport.play(); else transport.pause();
  el('play').textContent = transport.paused ? 'Play' : 'Pause';
});

el('insp-close').addEventListener('click', function () {
  el('inspector').hidden = true;
  selected = null;
});

el('insp-t').addEventListener('change', function () {
  if (!selected) return;
  const t = parseTime(el('insp-t').value);
  if (t === null) { el('status').textContent = 'that is not a timecode'; return; }
  selected.cue.t = t;
  (selected.track.cues || []).sort(function (a, b) { return a.t - b.t; });
  markDirty();
  timeline.setScore(score, duration());
});

el('insp-dur').addEventListener('change', function () {
  if (!selected) return;
  const d = Number(el('insp-dur').value);
  if (!Number.isFinite(d) || d < 0) return;
  selected.cue.duration = d;
  markDirty();
  timeline.setScore(score, duration());
});

document.addEventListener('keydown', function (e) {
  if (e.target && e.target.tagName === 'INPUT') return;
  if (e.code === 'Space') { e.preventDefault(); el('play').click(); }
});

window.addEventListener('beforeunload', function (e) {
  if (dirty) { e.preventDefault(); e.returnValue = ''; }
});

/* Start. Without these two lines every function above is defined and none of
 * them ever runs, which is a perfectly valid script and a completely dead
 * application. */
load();
requestAnimationFrame(frame);

/* --- library ------------------------------------------------------------ */

let library = null;

/* Selecting a film switches both the picture and the score. Changing only the
 * picture is what made a fifteen minute film look empty after the first
 * minute: it was playing against a three cue demo score. */
async function openFilm(name) {
  attachMedia(name);

  const res = await fetch('/api/score?film=' + encodeURIComponent(name));
  if (!res.ok) {
    /* Say so, and clear the timeline. Leaving the previous film's score on
     * screen under this film's name is the confusion this whole feature
     * exists to end. */
    score = { title: name, duration: 0, tracks: [] };
    el('title').textContent = name;
    el('status').textContent = 'no score yet; use Analyse in the library';
    timeline.setScore(score, duration());
    if (room) room.setInstruments((rig && rig.instruments) || []);
    if (library) library.refresh();
    return;
  }
  score = await res.json();
  el('title').textContent = score.title || '(untitled)';
  timeline.setScore(score, duration());
  applyMuted();
  if (library) library.refresh();
}

async function loadLibrary() {
  library = new Library(el('library'), {
    onOpen: function (film) {
      const picker = el('media-picker');
      if (picker) picker.value = film;
      openFilm(film);
    },
  });
  await library.refresh();

  /* Line the picker up with whichever score is actually open, so the two
   * panes never disagree about what is being previewed. */
  const data = library.data;
  const picker = el('media-picker');
  if (!data || !picker) return;
  for (const entry of (data.entries || [])) {
    if (entry.scoreName === data.current) {
      picker.value = entry.film;
      attachMedia(entry.film);
      return;
    }
  }
}
