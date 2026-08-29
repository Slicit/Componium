/* The timeline.
 *
 * One row per track, drawn against the film's own duration, with a playhead
 * that follows the video. Spans are blocks so their length is visible, which
 * matters more than it sounds: a four second fog burst and a momentary flash
 * look identical as ticks, and the difference is the whole reason spans exist.
 */

'use strict';

const NS = 'http://www.w3.org/2000/svg';
const VIEW_W = 1000;
const ROW_H = 46;

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
  }

  setScore(score, duration) {
    this.score = score;
    this.duration = Math.max(1, duration || score.duration || 1);
    this.render();
  }

  render() {
    const host = this.host;
    host.textContent = '';

    (this.score.tracks || []).forEach((track, ti) => {
      const row = document.createElement('div');
      row.className = 'trk';

      const name = document.createElement('div');
      name.className = 'trk-name';
      name.textContent = track.instrument;
      const kind = document.createElement('span');
      kind.textContent = track.type === 'curve'
        ? (track.points || []).length + ' points'
        : (track.cues || []).length + ' cues';
      name.appendChild(kind);
      row.appendChild(name);

      const lane = document.createElement('div');
      lane.className = 'trk-lane';
      const s = svg('svg', {
        viewBox: `0 0 ${VIEW_W} ${ROW_H}`, preserveAspectRatio: 'none',
      }, lane);

      if (track.type === 'curve') this.drawCurve(s, track);
      else this.drawCues(s, track, ti);

      /* Seeking by clicking the lane is the main way anyone will move around,
       * so it is on the whole lane rather than on a scrubber somewhere else. */
      lane.addEventListener('click', (e) => {
        const box = lane.getBoundingClientRect();
        this.onSeek(((e.clientX - box.left) / box.width) * this.duration);
      });

      row.appendChild(lane);
      host.appendChild(row);
    });

    this.playhead = document.createElement('div');
    this.playhead.className = 'playhead';
    host.appendChild(this.playhead);
    this.setTime(0);
  }

  x(t) { return (t / this.duration) * VIEW_W; }

  drawCurve(s, track) {
    const points = track.points || [];
    const channels = new Set();
    for (const p of points) for (const k of Object.keys(p.value || {})) channels.add(k);

    /* One line per channel: a colour curve is three signals, and averaging
     * them into one would hide exactly the thing being edited. */
    for (const channel of channels) {
      const coords = points
        .filter((p) => channel in (p.value || {}))
        .map((p) => `${this.x(p.t)},${ROW_H - 6 - clamp01(p.value[channel]) * (ROW_H - 14)}`);
      if (coords.length > 1) {
        svg('polyline', { class: 'curve ch-' + channel, points: coords.join(' ') }, s);
      }
    }
  }

  drawCues(s, track, ti) {
    (track.cues || []).forEach((cue, i) => {
      const x = this.x(cue.t);
      const w = cue.duration > 0 ? Math.max(2, this.x(cue.duration)) : 3;
      const rect = svg('rect', {
        x: x, y: 8, width: w, height: ROW_H - 16,
        class: 'cue' + (this.isSelected(ti, i) ? ' sel' : ''),
        rx: 2,
      }, s);
      rect.addEventListener('click', (e) => {
        e.stopPropagation();
        this.selected = { track: ti, index: i };
        this.onSelect(track, cue, ti, i);
        this.render();
      });
      svg('title', {}, rect).textContent =
        `${cue.action} at ${fmt(cue.t)}${cue.duration ? ' for ' + cue.duration + 's' : ''}` +
        (cue.source ? '\n' + cue.source : '');
    });
  }

  isSelected(ti, i) {
    return this.selected && this.selected.track === ti && this.selected.index === i;
  }

  setTime(t) {
    if (!this.playhead) return;
    const pct = Math.max(0, Math.min(1, t / this.duration)) * 100;
    this.playhead.style.left = `calc(150px + (100% - 150px) * ${pct / 100})`;
  }
}

function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }

function fmt(t) {
  const total = Math.round(t * 1000);
  const m = Math.floor(total / 60000);
  const s = Math.floor((total % 60000) / 1000);
  const ms = total % 1000;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(ms).padStart(3, '0')}`;
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { fmt, clamp01 };
}
