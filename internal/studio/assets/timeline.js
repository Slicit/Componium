/* The timeline.
 *
 * One row per track, drawn against the film's own duration, with a playhead
 * that follows the video. Spans are blocks so their length is visible, which
 * matters more than it sounds: a four second fog burst and a momentary flash
 * look identical as ticks, and the difference is the whole reason spans exist.
 *
 * Everything here is an object you can take hold of. A curve used to be drawn
 * as a bare polyline, which showed the shape and hid the fact that it is made
 * of points somebody chose and can change; a cue was a block you could select
 * but not move. Both are now draggable, because the composer's output is a
 * first pass and the whole purpose of looking at it is to disagree with it in
 * places.
 */

'use strict';

const NS = 'http://www.w3.org/2000/svg';
const VIEW_W = 1000;
const ROW_H = 46;

/* Below this, a drag is a click. Without it, selecting a cue on a trackpad
 * nudges it a few milliseconds and marks the score dirty, and the edit you
 * did not mean to make is the one you will not notice. */
const DRAG_SLOP = 3;

function svg(name, attrs, parent) {
  const node = document.createElementNS(NS, name);
  for (const k of Object.keys(attrs || {})) node.setAttribute(k, attrs[k]);
  if (parent) parent.appendChild(node);
  return node;
}

class Timeline {
  constructor(host, callbacks) {
    this.host = host;
    this.onSeek = (callbacks && callbacks.onSeek) || function () {};
    this.onSelect = (callbacks && callbacks.onSelect) || function () {};
    this.duration = 1;
    this.selected = null;
    this.muted = new Set();
    this.onToggle = (callbacks && callbacks.onToggle) || function () {};
    this.onSolo = (callbacks && callbacks.onSolo) || function () {};
    /* Called after a drag changes the score, so the Save button lights up. */
    this.onEdit = (callbacks && callbacks.onEdit) || function () {};
  }

  setScore(score, duration) {
    this.score = score;
    this.duration = Math.max(1, duration || score.duration || 1);
    this.render();
  }

  render() {
    const host = this.host;
    host.textContent = '';
    /* Reset rather than accumulate: these hold references to nodes that this
     * render is about to throw away, and keeping them would leak a detached
     * SVG element per track per redraw. */
    this.lines = {};

    (this.score.tracks || []).forEach((track, ti) => {
      const row = document.createElement('div');
      row.className = 'trk';

      row.classList.toggle("muted", this.muted.has(track.instrument));

      const name = document.createElement("div");
      name.className = "trk-name";

      /* The checkbox is the same state the room reads, so muting a track here
       * takes its device out of the room too. Reviewing one effect at a time
       * is most of what previewing is for. */
      const box = document.createElement("input");
      box.type = "checkbox";
      box.checked = !this.muted.has(track.instrument);
      box.addEventListener("change", (e) => {
        e.stopPropagation();
        this.onToggle(track.instrument, !box.checked);
      });
      name.appendChild(box);

      const label = document.createElement("span");
      label.className = "trk-label";
      label.textContent = track.instrument;
      name.appendChild(label);

      const solo = document.createElement("button");
      solo.className = "solo";
      solo.textContent = "solo";
      solo.title = "mute everything else, or restore if this is already alone";
      solo.addEventListener("click", (e) => {
        e.stopPropagation();
        this.onSolo(track.instrument);
      });
      name.appendChild(solo);

      const kind = document.createElement("span");
      kind.className = "trk-count";
      kind.textContent = track.type === "curve"
        ? (track.points || []).length + " points"
        : (track.cues || []).length + " cues";
      name.appendChild(kind);
      row.appendChild(name);

      const lane = document.createElement('div');
      lane.className = 'trk-lane';
      const s = svg('svg', {
        viewBox: `0 0 ${VIEW_W} ${ROW_H}`, preserveAspectRatio: 'none',
      }, lane);

      if (track.type === 'curve') this.drawCurve(s, track, ti);
      else this.drawCues(s, track, ti);

      this.armDragging(s, track, ti);

      /* Seeking by clicking the lane is the main way anyone will move around,
       * so it is on the whole lane rather than on a scrubber somewhere else. */
      lane.addEventListener('click', (e) => {
        /* A drag that ended a moment ago also produces a click. Seeking on it
         * would yank the playhead away every time you finished moving a cue. */
        if (this.justDragged) { this.justDragged = false; return; }
        const box = lane.getBoundingClientRect();
        this.onSeek(((e.clientX - box.left) / box.width) * this.duration);
      });

      row.appendChild(lane);
      host.appendChild(row);
    });

    this.playhead = document.createElement('div');
    this.playhead.className = 'playhead';
    host.appendChild(this.playhead);
    this.setTime(this.time || 0);
  }

  x(t) { return (t / this.duration) * VIEW_W; }

  /* y for a curve value: 0 at the bottom of the lane, 1 at the top. */
  y(v) { return ROW_H - 6 - clamp01(v) * (ROW_H - 14); }

  drawCurve(s, track, ti) {
    const points = track.points || [];
    const channels = new Set();
    for (const p of points) for (const k of Object.keys(p.value || {})) channels.add(k);

    /* One line per channel: a colour curve is three signals, and averaging
     * them into one would hide exactly the thing being edited. */
    for (const channel of channels) {
      const indices = [];
      points.forEach((p, i) => { if (channel in (p.value || {})) indices.push(i); });

      if (indices.length > 1) {
        const line = svg('polyline', {
          class: 'curve ch-' + channel,
          points: indices.map((i) => this.pointXY(points[i], channel)).join(' '),
        }, s);
        this.lines[ti + '/' + channel] = {
          node: line, indices: indices, channel: channel, points: points,
        };
      }

      /* A handle per point. These are what make a curve editable rather than
       * merely visible; without them the shape is something that happened to
       * you rather than something you chose. */
      for (const i of indices) {
        const p = points[i];
        svg('circle', {
          class: 'pt ch-' + channel,
          cx: this.x(p.t), cy: this.y(p.value[channel]), r: 3.2,
          'data-kind': 'point', 'data-track': ti, 'data-index': i, 'data-channel': channel,
        }, s);
      }
    }
  }

  pointXY(p, channel) {
    return `${this.x(p.t)},${this.y(p.value[channel])}`;
  }

  drawCues(s, track, ti) {
    (track.cues || []).forEach((cue, i) => {
      const x = this.x(cue.t);
      const w = cue.duration > 0 ? Math.max(2, this.x(cue.duration)) : 3;
      const rect = svg('rect', {
        x: x, y: 8, width: w, height: ROW_H - 16,
        class: 'cue' + (this.isSelected(ti, i) ? ' sel' : ''),
        rx: 2,
        'data-kind': 'cue', 'data-track': ti, 'data-index': i, 'data-handle': 'body',
      }, s);
      rect.addEventListener('click', (e) => {
        e.stopPropagation();
        if (this.justDragged) { this.justDragged = false; return; }
        this.selected = { track: ti, index: i };
        this.onSelect(track, cue, ti, i);
        this.render();
      });
      svg('title', {}, rect).textContent =
        `${cue.action} at ${fmt(cue.t)}${cue.duration ? ' for ' + cue.duration + 's' : ''}` +
        (cue.source ? '\n' + cue.source : '') +
        '\ndrag to move, drag the right edge to change the length';

      /* A grip on the trailing edge for the length. Only on spans: a
       * momentary cue has no duration to pull, and offering a handle that
       * silently turns one into the other would be a trap. */
      if (cue.duration > 0) {
        svg('rect', {
          x: x + w - 3, y: 8, width: 6, height: ROW_H - 16,
          class: 'cue-grip',
          'data-kind': 'cue', 'data-track': ti, 'data-index': i, 'data-handle': 'edge',
        }, s);
      }
    });
  }

  /* One pointerdown listener per lane rather than one per object.
   *
   * A feature length score runs to thousands of curve points, and attaching a
   * listener to each is how a timeline becomes slow to build and slow to throw
   * away. The target carries what it is in data attributes, which is all the
   * handler needs. */
  armDragging(lane, track, ti) {
    lane.addEventListener('pointerdown', (e) => {
      const kind = e.target.getAttribute && e.target.getAttribute('data-kind');
      if (!kind) return;
      e.stopPropagation();
      e.preventDefault();

      const index = Number(e.target.getAttribute('data-index'));
      const handle = e.target.getAttribute('data-handle');
      const channel = e.target.getAttribute('data-channel');
      const box = lane.getBoundingClientRect();
      const startX = e.clientX;
      const startY = e.clientY;

      const cue = kind === 'cue' ? (track.cues || [])[index] : null;
      const point = kind === 'point' ? (track.points || [])[index] : null;
      if (!cue && !point) return;

      const wasT = cue ? cue.t : point.t;
      const wasDur = cue ? (cue.duration || 0) : 0;
      const wasValue = point ? clamp01(point.value[channel]) : 0;
      let moved = false;

      const onMove = (ev) => {
        if (!moved
          && Math.abs(ev.clientX - startX) < DRAG_SLOP
          && Math.abs(ev.clientY - startY) < DRAG_SLOP) return;
        moved = true;

        const dt = ((ev.clientX - startX) / box.width) * this.duration;

        if (cue && handle === 'edge') {
          cue.duration = Math.max(0.05, round3(wasDur + dt));
        } else if (cue) {
          cue.t = clampTime(wasT + dt, this.duration);
        } else {
          point.t = clampTime(wasT + dt, this.duration);
          /* Up is more. The lane is only forty pixels tall, so the vertical
           * gearing is deliberately gentle: a full-height drag is a full
           * swing, and anything finer is done in the inspector. */
          const dv = -((ev.clientY - startY) / box.height);
          point.value[channel] = round3(clamp01(wasValue + dv));
        }
        this.liveUpdate(e.target, ti, index, channel, cue, point);
      };

      const onUp = () => {
        lane.removeEventListener('pointermove', onMove);
        lane.removeEventListener('pointerup', onUp);
        lane.removeEventListener('pointercancel', onUp);
        if (!moved) return;
        this.justDragged = true;
        /* Points arrive in time order and a drag can carry one past its
         * neighbour. Leaving them out of order draws a curve that doubles
         * back on itself and, worse, is evaluated wrongly. */
        if (point) (track.points || []).sort((a, b) => a.t - b.t);
        if (cue) (track.cues || []).sort((a, b) => a.t - b.t);
        this.onEdit();
        this.render();
      };

      if (lane.setPointerCapture) {
        try { lane.setPointerCapture(e.pointerId); } catch (err) { /* not captured */ }
      }
      lane.addEventListener('pointermove', onMove);
      lane.addEventListener('pointerup', onUp);
      lane.addEventListener('pointercancel', onUp);
    });
  }

  /* Move just the thing being dragged, not the whole timeline.
   *
   * Re-rendering every frame of a drag is the obvious implementation and it
   * makes a feature length score unusable: it rebuilds thousands of nodes per
   * pointer move, and it also destroys the element the pointer is captured on
   * halfway through the gesture. */
  liveUpdate(node, ti, index, channel, cue, point) {
    if (cue) {
      const x = this.x(cue.t);
      const w = cue.duration > 0 ? Math.max(2, this.x(cue.duration)) : 3;
      const body = node.parentNode.querySelector(
        `[data-kind="cue"][data-index="${index}"][data-handle="body"]`);
      const grip = node.parentNode.querySelector(
        `[data-kind="cue"][data-index="${index}"][data-handle="edge"]`);
      if (body) { body.setAttribute('x', x); body.setAttribute('width', w); }
      if (grip) grip.setAttribute('x', x + w - 3);
      return;
    }
    node.setAttribute('cx', this.x(point.t));
    node.setAttribute('cy', this.y(point.value[channel]));
    const line = this.lines && this.lines[ti + '/' + channel];
    if (line) {
      line.node.setAttribute('points',
        line.indices.map((i) => this.pointXY(line.points[i], channel)).join(' '));
    }
  }

  isSelected(ti, i) {
    return this.selected && this.selected.track === ti && this.selected.index === i;
  }

  setTime(t) {
    this.time = t;
    if (!this.playhead) return;
    const pct = Math.max(0, Math.min(1, t / this.duration)) * 100;
    this.playhead.style.left = `calc(150px + (100% - 150px) * ${pct / 100})`;
  }
}

function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }

function clampTime(t, duration) {
  return round3(Math.max(0, Math.min(duration, t)));
}

/* Milliseconds. The score's own resolution, and it keeps a dragged value from
 * being written out as 0.30000000000000004. */
function round3(v) { return Math.round(v * 1000) / 1000; }

function fmt(t) {
  const total = Math.round(t * 1000);
  const m = Math.floor(total / 60000);
  const s = Math.floor((total % 60000) / 1000);
  const ms = total % 1000;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(ms).padStart(3, '0')}`;
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { fmt, clamp01, clampTime, round3 };
}

/* Attached after the class so the file reads top down. */
Timeline.prototype.setMuted = function (muted) {
  this.muted = muted;
  if (this.score) this.render();
};
