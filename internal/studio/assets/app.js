/* Componium studio.
 *
 * No framework and no build step. The whole editor is a score in memory, a
 * render function, and a handful of event handlers. If it ever needs more than
 * that it will need a toolchain, and that trade can be made then rather than
 * imposed on everyone who wants to fix a cue time.
 */

'use strict';

let score = null;
let selected = null;   /* { track, kind: 'cue'|'point', index } */
let dirty = false;

const el = (id) => document.getElementById(id);

/* Timecode and clamping live in timecode.js, which node can test without a
 * DOM. They are plain globals here, loaded by a script tag before this file. */

/* --------------------------------------------------------------- render */

function duration() {
  let d = score && score.duration ? score.duration : 0;
  for (const t of (score ? score.tracks : [])) {
    for (const c of t.cues || []) d = Math.max(d, c.t);
    for (const p of t.points || []) d = Math.max(d, p.t);
  }
  return d > 0 ? d : 1;
}

function svgEl(name, attrs) {
  const node = document.createElementNS('http://www.w3.org/2000/svg', name);
  for (const k of Object.keys(attrs)) node.setAttribute(k, attrs[k]);
  return node;
}

function renderRuler() {
  const total = duration();
  const ruler = el('ruler');
  ruler.textContent = '';
  const steps = 8;
  for (let i = 0; i <= steps; i++) {
    const span = document.createElement('span');
    span.style.left = ((i / steps) * 100) + '%';
    span.textContent = toTimecode((total * i) / steps).slice(0, 8);
    ruler.appendChild(span);
  }
}

function cueLane(track, ti, total) {
  const svg = svgEl('svg', { viewBox: '0 0 1000 56', preserveAspectRatio: 'none' });
  (track.cues || []).forEach(function (cue, i) {
    const x = (cue.t / total) * 1000;
    const mark = svgEl('rect', {
      x: Math.max(0, x - 2), y: 10, width: 4, height: 36,
      class: isSelected(ti, 'cue', i) ? 'cue selected' : 'cue',
    });
    mark.addEventListener('click', function () { select(ti, 'cue', i); });
    svg.appendChild(mark);
  });
  return svg;
}

function curveLane(track, ti, total) {
  const svg = svgEl('svg', { viewBox: '0 0 1000 56', preserveAspectRatio: 'none' });
  const points = track.points || [];
  const channels = new Set();
  for (const p of points) for (const k of Object.keys(p.value || {})) channels.add(k);

  /* One polyline per channel, so a colour curve reads as three lines rather
   * than as one meaningless average. */
  for (const channel of channels) {
    const coords = points
      .filter(function (p) { return channel in (p.value || {}); })
      .map(function (p) {
        return ((p.t / total) * 1000) + ',' + (48 - clamp01(p.value[channel]) * 40);
      });
    if (coords.length > 1) {
      svg.appendChild(svgEl('polyline', { class: 'curveline', points: coords.join(' ') }));
    }
  }
  points.forEach(function (p, i) {
    const first = Object.values(p.value || {})[0] || 0;
    const dot = svgEl('circle', {
      cx: (p.t / total) * 1000, cy: 48 - clamp01(first) * 40, r: 3.5,
      class: isSelected(ti, 'point', i) ? 'point selected' : 'point',
    });
    dot.addEventListener('click', function () { select(ti, 'point', i); });
    svg.appendChild(dot);
  });
  return svg;
}

function render() {
  renderRuler();
  const host = el('tracks');
  host.textContent = '';
  const total = duration();

  score.tracks.forEach(function (track, ti) {
    const row = document.createElement('div');
    row.className = 'track';

    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = track.instrument;
    const kind = document.createElement('span');
    kind.className = 'kind muted';
    kind.textContent = track.type === 'curve'
      ? 'curve, ' + (track.points || []).length + ' points'
      : 'cue, ' + (track.cues || []).length + ' cues';
    name.appendChild(kind);
    row.appendChild(name);

    const lane = document.createElement('div');
    lane.className = 'lane';
    lane.appendChild(track.type === 'curve'
      ? curveLane(track, ti, total)
      : cueLane(track, ti, total));
    row.appendChild(lane);
    host.appendChild(row);
  });
}

/* ------------------------------------------------------------ selection */

function isSelected(ti, kind, index) {
  return selected && selected.track === ti && selected.kind === kind && selected.index === index;
}

function current() {
  if (!selected) return null;
  const track = score.tracks[selected.track];
  const list = selected.kind === 'cue' ? track.cues : track.points;
  return { track: track, item: list[selected.index], list: list };
}

function select(ti, kind, index) {
  selected = { track: ti, kind: kind, index: index };
  render();
  showInspector();
}

function markDirty() {
  dirty = true;
  el('save').disabled = false;
  el('status').textContent = 'unsaved changes';
}

function textField(label, value, onChange) {
  const wrap = document.createElement('label');
  wrap.textContent = label;
  const input = document.createElement('input');
  input.type = 'text';
  input.value = value;
  input.addEventListener('change', function () { onChange(input.value); });
  wrap.appendChild(input);
  return wrap;
}

function showInspector() {
  const found = current();
  const panel = el('inspector');
  if (!found) { panel.hidden = true; return; }
  panel.hidden = false;

  el('insp-title').textContent = found.track.instrument + ' · ' + selected.kind;
  el('insp-t').value = toTimecode(found.item.t);

  const fields = el('insp-fields');
  fields.textContent = '';
  const values = selected.kind === 'cue'
    ? (found.item.params || {})
    : (found.item.value || {});

  if (selected.kind === 'cue') {
    fields.appendChild(textField('action', found.item.action, function (v) {
      found.item.action = v;
      markDirty();
    }));
  }
  for (const key of Object.keys(values)) {
    fields.appendChild(textField(key, values[key], function (v) {
      const n = Number(v);
      if (Number.isFinite(n)) { values[key] = n; markDirty(); render(); }
    }));
  }
}

/* ----------------------------------------------------------- load, save */

async function load() {
  const res = await fetch('/api/score');
  if (!res.ok) { el('status').textContent = 'could not load the score'; return; }
  score = await res.json();
  el('title').textContent = score.title || '(untitled)';
  el('path').textContent = score.path;
  el('status').textContent = 'loaded';
  render();
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

/* ---------------------------------------------------------------- wiring */

el('save').addEventListener('click', save);

el('insp-close').addEventListener('click', function () {
  selected = null;
  el('inspector').hidden = true;
  render();
});

el('insp-t').addEventListener('change', function () {
  const found = current();
  if (!found) return;
  const t = fromTimecode(el('insp-t').value);
  if (t === null) { el('status').textContent = 'that is not a timecode'; return; }
  found.item.t = t;
  found.list.sort(function (a, b) { return a.t - b.t; });
  selected.index = found.list.indexOf(found.item);
  markDirty();
  render();
});

el('insp-delete').addEventListener('click', function () {
  const found = current();
  if (!found) return;
  found.list.splice(selected.index, 1);
  selected = null;
  el('inspector').hidden = true;
  markDirty();
  render();
});

window.addEventListener('beforeunload', function (e) {
  if (dirty) { e.preventDefault(); e.returnValue = ''; }
});

load();
