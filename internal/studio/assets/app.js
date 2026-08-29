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
  const on = Object.keys(state).filter((id) => state[id].active && state[id].level > 0.02);
  if (on.length === 0) {
    host.textContent = '';
    const none = document.createElement('span');
    none.className = 'muted';
    none.textContent = 'nothing playing';
    host.appendChild(none);
    return;
  }
  host.textContent = '';
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

  room = new Room(el('room'));
  room.setInstruments(rig.instruments || []);

  timeline = new Timeline(el('timeline'), {
    onSeek: function (t) { transport.currentTime = t; },
    onSelect: showInspector,
    onToggle: toggleMuted,
    onSolo: soloOnly,
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
      'this browser cannot decode ' + name + '. The timeline and room still ' +
      'work, driven by the score alone. Try one of the mp4 files.';
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
  if (!res.ok) return;
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
