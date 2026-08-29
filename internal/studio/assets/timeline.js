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

/* A curve channel's own lane. Shorter than a cue lane because a track can have
 * three of them and a colour track would otherwise be taller than the video. */
const SUB_H = 26;

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

      if (track.type === 'curve') {
        this.drawCurveLanes(lane, track, ti);
      } else {
        const s = svg('svg', {
          viewBox: `0 0 ${VIEW_W} ${ROW_H}`, preserveAspectRatio: 'none',
        }, lane);
        this.drawCues(s, track, ti);
        this.armDragging(s, track, ti, ROW_H);
      }

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

  /* y for a curve value, in a lane h units tall: 0 at the bottom, 1 at the
   * top. Curve lanes and cue lanes are different heights, so the height is a
   * parameter rather than a constant. */
  y(v, h) { return h - 4 - clamp01(v) * (h - 8); }

  /* Which channels a curve track has.
   *
   * Normally the union of what its points carry, in a fixed order so red is
   * always the top lane. A track with no points has nothing to read, and that
   * is not a rare case any more now that deleting every point is a legal way
   * to say the instrument does nothing — so fall back to what the rig says
   * this instrument is, and finally to a single unnamed level. Without that,
   * emptying a track would make its lanes vanish and leave nowhere to click to
   * start a new one.
   */
  channelsOf(track) {
    const seen = new Set();
    for (const p of (track.points || [])) {
      for (const k of Object.keys(p.value || {})) seen.add(k);
    }
    if (seen.size) {
      const order = ['r', 'g', 'b'];
      const first = order.filter((c) => seen.has(c));
      const rest = Array.from(seen).filter((c) => order.indexOf(c) < 0).sort();
      return first.concat(rest);
    }
    const kind = this.kindOf(track.instrument);
    return kind === 'light' ? ['r', 'g', 'b'] : ['intensity'];
  }

  kindOf(instrument) {
    for (const inst of ((this.rig && this.rig.instruments) || [])) {
      if (inst.id === instrument) return inst.kind;
    }
    /* The id is conventionally kind.name, so this is a decent guess when there
     * is no rig loaded. */
    return String(instrument || '').split('.')[0];
  }

  /* One lane per channel.
   *
   * They used to share a lane, which drew three lines on top of each other and,
   * worse, stacked their handles: wherever r, g and b held the same value —
   * every moment the light is off, which is most of a film — the three points
   * sat at exactly the same pixel and only the topmost could be grabbed. A lane
   * each costs vertical space and makes every point reachable.
   */
  drawCurveLanes(lane, track, ti) {
    for (const channel of this.channelsOf(track)) {
      const sub = document.createElement('div');
      sub.className = 'sub';

      const tag = document.createElement('span');
      tag.className = 'sub-tag ch-' + channel;
      tag.textContent = channel;
      sub.appendChild(tag);

      /* Two layers, and the reason is geometry.
       *
       * The line needs a viewBox so it can be drawn in seconds and stretched
       * to whatever width the lane happens to be — but preserveAspectRatio
       * "none" squashes the x axis by whatever that stretch factor is, which
       * for a typical lane is about 0.55. A circle drawn in that space is not
       * a circle, it is a two pixel wide sliver, and it cannot be hit. The
       * handles therefore live in a second, unscaled layer on top, positioned
       * with percentages so they still track the lane's width, and are round
       * and clickable at any size.
       */
      const s = svg('svg', {
        viewBox: `0 0 ${VIEW_W} ${SUB_H}`, preserveAspectRatio: 'none', class: 'sub-lane',
      }, sub);
      const hit = svg('svg', { class: 'sub-hit' }, sub);

      this.drawChannel(s, hit, track, ti, channel);
      this.armDragging(hit, track, ti, SUB_H);
      this.armPointEditing(hit, track, ti, channel);

      lane.appendChild(sub);
    }
  }

  drawChannel(s, hit, track, ti, channel) {
    const points = track.points || [];
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
     * merely visible; without them the shape is something that happened to you
     * rather than something you chose. */
    for (const i of indices) {
      const p = points[i];
      svg('circle', {
        class: 'pt ch-' + channel,
        cx: this.xPct(p.t), cy: this.y(p.value[channel], SUB_H), r: 4,
        'data-kind': 'point', 'data-track': ti, 'data-index': i, 'data-channel': channel,
      }, hit);
    }

    if (!points.length) {
      /* An empty channel says so, and says what to do about it. An empty lane
       * with no explanation reads as a bug rather than as a decision. */
      svg('text', {
        x: 8, y: SUB_H - 8, class: 'lane-hint',
      }, hit).textContent = 'no points — double click to start a curve';
    }
  }

  /* Handles are positioned as a percentage of the lane, because their layer is
   * unscaled and so cannot use the viewBox's seconds. */
  xPct(t) { return ((t / this.duration) * 100).toFixed(4) + '%'; }

  pointXY(p, channel) {
    return `${this.x(p.t)},${this.y(p.value[channel], SUB_H)}`;
  }

  /* Double click to add a point, double click a point to remove it.
   *
   * The target says which: a circle is on top of the lane, so a double click
   * that lands on one is unambiguously about that point.
   */
  armPointEditing(s, track, ti, channel) {
    s.addEventListener('dblclick', (e) => {
      e.preventDefault();
      e.stopPropagation();
      const onPoint = e.target.getAttribute
        && e.target.getAttribute('data-kind') === 'point';
      if (onPoint) {
        this.removePoint(track, Number(e.target.getAttribute('data-index')));
      } else {
        const box = s.getBoundingClientRect();
        const t = clampTime(((e.clientX - box.left) / box.width) * this.duration, this.duration);
        const v = clamp01(1 - (e.clientY - box.top) / box.height);
        this.addPoint(track, ti, channel, t, v);
      }
      this.onEdit();
      this.render();
    });
  }

  /* Insert a point without changing the curve anywhere else.
   *
   * The clicked channel takes the clicked value, because that is plainly what
   * the click meant. Every other channel takes whatever the curve is already
   * worth at that instant, so adding a point to the red lane does not put a
   * kink in green and blue.
   */
  addPoint(track, ti, channel, t, v) {
    const points = track.points || (track.points = []);
    const value = valueAt(points, t, this.channelsOf(track));
    value[channel] = round3(v);

    if (!points.length) {
      /* Two, or none. A single point is not a curve: it pins the channel to
       * one value for the whole film with no second point to move away from.
       * The score refuses it, and rightly, so the first click on an empty
       * track lays down a short flat segment to shape rather than an orphan
       * that could not be saved.
       */
      const gap = Math.max(1, Math.min(this.duration - t, this.duration * 0.1));
      points.push({ t: round3(t), value: value });
      points.push({ t: round3(Math.min(this.duration, t + gap)), value: Object.assign({}, value) });
    } else {
      points.push({ t: round3(t), value: value });
    }
    points.sort((a, b) => a.t - b.t);
  }

  /* Remove a point, and refuse to leave an orphan behind.
   *
   * Going from two points to one would leave a curve the score will not
   * accept, so the second one goes too and the track becomes empty — which is
   * the meaningful state it was heading for anyway: no points, no light.
   */
  removePoint(track, index) {
    const points = track.points || [];
    if (index < 0 || index >= points.length) return;
    points.splice(index, 1);
    if (points.length === 1) points.length = 0;
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
        /* Wide in viewBox units because the x axis is squashed when the lane
         * is narrower than a thousand: six units is under four pixels on a
         * typical lane, which is not a grip, it is a dare. */
        svg('rect', {
          x: x + w - 7, y: 8, width: 14, height: ROW_H - 16,
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
  armDragging(lane, track, ti, laneH) {
    lane.addEventListener('pointerdown', (e) => {
      const kind = e.target.getAttribute && e.target.getAttribute('data-kind');
      if (!kind) return;
      e.stopPropagation();
      /* Deliberately no preventDefault().
       *
       * Calling it here suppresses the compatibility mouse events the browser
       * synthesises from pointer events — which means click and dblclick never
       * fire on anything draggable. That silently broke both double clicking a
       * handle to delete it and clicking a cue to open the inspector, while
       * dragging itself kept working perfectly, so nothing looked wrong.
       * Scrolling and text selection during a drag are prevented with
       * touch-action and user-select in the stylesheet instead, which is what
       * those properties are for.
       */

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

        if (!moved) {
          moved = true;
          /* Capture the pointer only once this is definitely a drag.
           *
           * Taking capture on pointerdown instead looks harmless and quietly
           * breaks every click on anything draggable: while a pointer is
           * captured the browser fires the following click and dblclick at the
           * *capturing* element rather than at what was under the cursor. So
           * double clicking a handle to delete it arrived as a double click on
           * the empty lane and added a point, and clicking a cue stopped
           * opening the inspector — while dragging itself worked perfectly,
           * which is why neither looked like the same bug.
           */
          if (lane.setPointerCapture) {
            try { lane.setPointerCapture(e.pointerId); } catch (err) { /* not captured */ }
          }
        }

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
        this.liveUpdate(e.target, ti, index, channel, cue, point, laneH);
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
  liveUpdate(node, ti, index, channel, cue, point, laneH) {
    if (cue) {
      const x = this.x(cue.t);
      const w = cue.duration > 0 ? Math.max(2, this.x(cue.duration)) : 3;
      const body = node.parentNode.querySelector(
        `[data-kind="cue"][data-index="${index}"][data-handle="body"]`);
      const grip = node.parentNode.querySelector(
        `[data-kind="cue"][data-index="${index}"][data-handle="edge"]`);
      if (body) { body.setAttribute('x', x); body.setAttribute('width', w); }
      if (grip) grip.setAttribute('x', x + w - 7);
      return;
    }
    /* One point appears once in every channel lane, because a point carries
     * all the channels at once. Moving it in time therefore has to move its
     * handle in the other lanes too, or they disagree with each other until
     * the next full render. Only the lane being dragged changes height. */
    const row = node.closest ? node.closest('.trk-lane') : null;
    const twins = row
      ? row.querySelectorAll(`[data-kind="point"][data-index="${index}"]`)
      : [node];
    for (const twin of twins) {
      twin.setAttribute('cx', this.xPct(point.t));
      const ch = twin.getAttribute('data-channel');
      if (ch === channel) twin.setAttribute('cy', this.y(point.value[ch], laneH));
    }

    /* And every channel's line, for the same reason. */
    for (const ch of Object.keys(this.lines || {})) {
      if (ch.indexOf(ti + '/') !== 0) continue;
      const line = this.lines[ch];
      line.node.setAttribute('points',
        line.indices.map((i) => this.pointXY(line.points[i], line.channel)).join(' '));
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

/* What a curve is already worth at a time, for every channel.
 *
 * The same rule the player uses: hold before the first point and after the
 * last rather than extrapolating, and interpolate linearly in between. It is
 * duplicated here rather than shared because the player's copy is in Go, and
 * the consequence of the two disagreeing is a point that jumps the moment it
 * is inserted — so it is worth keeping the shapes identical and saying why.
 */
function valueAt(points, t, channels) {
  const out = {};
  for (const c of (channels || [])) out[c] = 0;
  if (!points.length) return out;

  if (t <= points[0].t) return Object.assign(out, points[0].value);
  const last = points[points.length - 1];
  if (t >= last.t) return Object.assign(out, last.value);

  let hi = 0;
  for (let i = 0; i < points.length; i++) {
    if (points[i].t > t) { hi = i; break; }
  }
  const a = points[hi - 1];
  const b = points[hi];
  const span = b.t - a.t;
  const f = span > 0 ? (t - a.t) / span : 0;

  Object.assign(out, a.value);
  for (const k of Object.keys(b.value || {})) {
    const av = (a.value || {})[k];
    out[k] = av === undefined ? b.value[k] : round3(av + (b.value[k] - av) * f);
  }
  return out;
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
  module.exports = { fmt, clamp01, clampTime, round3, valueAt };
}

/* Attached after the class so the file reads top down. */
Timeline.prototype.setMuted = function (muted) {
  this.muted = muted;
  if (this.score) this.render();
};

/* The rig, so an emptied curve track still knows which channels it ought to
 * have. Without it a track with no points has nothing to derive them from. */
Timeline.prototype.setRig = function (rig) {
  this.rig = rig;
  if (this.score) this.render();
};
