/* The library: every film, whether it has a score, and what is being analysed.
 *
 * Analysis is slow. A fifteen minute film takes about thirty five seconds and
 * a feature takes about five minutes, so it cannot happen inline while
 * somebody waits on a click. It runs as a background job on the server and
 * this polls it.
 *
 * Names here are deliberately unusual, because every script on this page
 * shares one global scope and a collision kills the application silently.
 */

'use strict';

const LIB_POLL_MS = 700;

function libEl(tag, className, parent) {
  const n = document.createElement(tag);
  if (className) n.className = className;
  if (parent) parent.appendChild(n);
  return n;
}

/* Whether anything is happening to this film, counting both kinds of job.
 *
 * Both, because the poll loop uses this to decide whether to keep asking, and
 * a version that looked only at the analysis would let a preview build run
 * completely invisibly: no progress bar, no completion, just a file that
 * appears if you happen to reload. */
function libBusy(entry) {
  const jobs = [entry.job, entry.prepare];
  for (const job of jobs) {
    if (job && (job.state === 'queued' || job.state === 'running')) return true;
  }
  return false;
}

function libSize(bytes) {
  return (bytes / (1024 * 1024)).toFixed(0) + ' MB';
}

function libClock(seconds) {
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m + 'm' + String(s).padStart(2, '0');
}

class Library {
  constructor(host, callbacks) {
    this.host = host;
    this.onOpen = (callbacks && callbacks.onOpen) || function () {};
    this.polling = false;
    this.data = null;
  }

  async refresh() {
    const res = await fetch('/api/library');
    if (!res.ok) return;
    this.data = await res.json();
    this.render();
    this.maybePoll();
  }

  /* Poll only while something is actually running. A studio sitting idle
   * should not be asking the server a question every second forever. */
  maybePoll() {
    const busy = (this.data.entries || []).some(libBusy);
    if (!busy || this.polling) return;
    this.polling = true;
    const tick = async () => {
      const res = await fetch('/api/library');
      if (res.ok) {
        this.data = await res.json();
        this.render();
      }
      const stillBusy = (this.data.entries || []).some(libBusy);
      if (stillBusy) {
        setTimeout(tick, LIB_POLL_MS);
      } else {
        this.polling = false;
      }
    };
    setTimeout(tick, LIB_POLL_MS);
  }

  async build(film) {
    await fetch('/api/build?file=' + encodeURIComponent(film), { method: 'POST' });
    await this.refresh();
  }

  async buildAll() {
    await fetch('/api/build?all=1', { method: 'POST' });
    await this.refresh();
  }

  async prepare(film) {
    await fetch('/api/prepare?file=' + encodeURIComponent(film), { method: 'POST' });
    await this.refresh();
  }

  async prepareAll() {
    await fetch('/api/prepare?all=1', { method: 'POST' });
    await this.refresh();
  }

  render() {
    const host = this.host;
    host.textContent = '';
    const data = this.data || { entries: [] };

    const head = libEl('div', 'lib-head', host);
    const where = libEl('span', 'muted small', head);
    where.textContent = data.canBuild
      ? 'scores in ' + (data.scores || '(none)')
      : 'no composer found, so films cannot be analysed here';
    if (data.free) {
      where.textContent += '  ·  ' + libSize(data.free) + ' free';
    }

    const headActions = libEl('div', 'lib-actions', head);

    if (data.canUpload) {
      /* A hidden input driven by a button, because the native file control is
       * unstyleable and says "no file chosen" forever after an upload. */
      const input = libEl('input', '', headActions);
      input.type = 'file';
      input.accept = 'video/*,.mkv,.mp4,.webm,.mov,.m4v';
      input.hidden = true;
      input.addEventListener('change', () => {
        if (input.files && input.files[0]) this.upload(input.files[0]);
        input.value = '';
      });

      const pick = libEl('button', '', headActions);
      pick.textContent = 'Upload film';
      pick.addEventListener('click', () => input.click());
    }

    if (data.canPrepare) {
      const prepAll = libEl('button', '', headActions);
      prepAll.textContent = 'Prepare all';
      prepAll.title = 'Make a browser-playable copy of every film that has not '
        + 'got one. They run one at a time.';
      prepAll.addEventListener('click', () => this.prepareAll());
    }

    if (data.canBuild) {
      const all = libEl('button', '', headActions);
      all.textContent = 'Rebuild all';
      all.title = 'Queue every film. They run one at a time.';
      all.addEventListener('click', () => this.buildAll());
    }

    for (const entry of (data.entries || [])) {
      const row = libEl('div', 'lib-row', host);
      if (entry.scoreName === data.current) row.classList.add('current');

      const name = libEl('div', 'lib-name', row);
      name.textContent = entry.film;
      const size = libEl('span', 'muted', name);
      size.textContent = ' ' + libSize(entry.size);

      const status = libEl('div', 'lib-status', row);
      const job = entry.job;

      if (job && (job.state === 'queued' || job.state === 'running')) {
        const bar = libEl('div', 'bar', status);
        const fill = libEl('div', 'fill', bar);
        fill.style.width = Math.round((job.progress || 0) * 100) + '%';
        const label = libEl('span', 'muted small', status);
        label.textContent = job.state === 'queued'
          ? 'queued' : job.label + '  ' + Math.round((job.progress || 0) * 100) + '%';
      } else if (job && job.state === 'failed') {
        const failed = libEl('span', 'failed small', status);
        /* The composer's own last words, not a generic failure. Knowing it was
         * ffmpeg that objected is most of the diagnosis. */
        failed.textContent = 'failed: ' + (job.error || 'unknown');
      } else if (job && job.state === 'interrupted') {
        /* Say it was killed rather than saying nothing. Silence here reads as
         * "it never started", which is the opposite of the truth and sends
         * somebody looking for a button they already pressed. */
        const stopped = libEl('span', 'muted small', status);
        stopped.textContent = 'analysis stopped at '
          + Math.round((job.progress || 0) * 100) + '% when the studio restarted';
      } else if (entry.hasScore) {
        const detail = libEl('span', 'muted small', status);
        detail.textContent = entry.tracks + ' tracks, ' + entry.cues + ' cues, ' +
          libClock(entry.duration);
        if (job && job.state === 'done' && job.seconds) {
          detail.textContent += '  (analysed in ' + Math.round(job.seconds) + 's)';
        }
      } else {
        const none = libEl('span', 'muted small', status);
        none.textContent = 'no score yet';
      }

      /* The preview line, separate from the analysis line, because the two are
       * independent and can both be true: a film can be analysing and building
       * its browser copy on different days. */
      const prep = entry.prepare;
      if (prep && (prep.state === 'queued' || prep.state === 'running')) {
        const pstatus = libEl('div', 'lib-status', row);
        const bar = libEl('div', 'bar', pstatus);
        libEl('div', 'fill', bar).style.width =
          Math.round((prep.progress || 0) * 100) + '%';
        const label = libEl('span', 'muted small', pstatus);
        label.textContent = prep.state === 'queued'
          ? 'preview queued'
          : 'preview ' + Math.round((prep.progress || 0) * 100) + '%  ' + (prep.label || '');
      } else if (prep && prep.state === 'failed') {
        const failed = libEl('span', 'failed small', status);
        failed.textContent = ' preview failed: ' + (prep.error || 'unknown');
      } else if (prep && prep.state === 'interrupted') {
        const stopped = libEl('span', 'muted small', status);
        stopped.textContent = ' preview stopped by a restart';
      } else if (entry.preview) {
        const ok = libEl('span', 'muted small', status);
        ok.textContent = '  · browser copy ready';
        ok.title = 'This film is played from a prepared MP4 rather than the original.';
      }

      const actions = libEl('div', 'lib-actions', row);
      if (entry.hasScore) {
        const open = libEl('button', '', actions);
        open.textContent = 'Open';
        open.addEventListener('click', () => this.onOpen(entry.film));
      }
      if (data.canBuild) {
        const build = libEl('button', '', actions);
        build.textContent = entry.hasScore ? 'Rebuild' : 'Analyse';
        const running = job && (job.state === 'queued' || job.state === 'running');
        build.disabled = !!running;
        build.addEventListener('click', () => this.build(entry.film));
      }
      if (data.canPrepare && !entry.preview) {
        const prepare = libEl('button', '', actions);
        prepare.textContent = 'Prepare';
        prepare.title = 'Make a copy this browser can play. Usually quick: the '
          + 'video is only re-encoded when it has to be.';
        prepare.disabled = !!(prep && (prep.state === 'queued' || prep.state === 'running'));
        prepare.addEventListener('click', () => this.prepare(entry.film));
      }
      if (data.canUpload) {
        const del = libEl('button', 'danger', actions);
        del.textContent = 'Delete';
        del.title = 'Remove the film from disk';
        del.addEventListener('click', () => this.remove(entry));
      }
    }
  }
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { libSize, libClock };
}

/* Upload with XMLHttpRequest rather than fetch, because fetch reports no
 * progress on the way *up* and a film is gigabytes. Watching a browser sit
 * silent for four minutes is indistinguishable from it having hung. */
Library.prototype.upload = function (file) {
  const host = this.host;
  const row = libEl('div', 'lib-row uploading', host);
  const name = libEl('div', 'lib-name', row);
  name.textContent = file.name;
  const status = libEl('div', 'lib-status', row);
  const bar = libEl('div', 'bar', status);
  const fill = libEl('div', 'fill', bar);
  const label = libEl('span', 'muted small', status);
  label.textContent = 'uploading ' + libSize(file.size);

  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/upload?name=' + encodeURIComponent(file.name));
  xhr.upload.onprogress = function (e) {
    if (!e.lengthComputable) return;
    fill.style.width = Math.round((e.loaded / e.total) * 100) + '%';
    label.textContent = libSize(e.loaded) + ' of ' + libSize(e.total);
  };
  xhr.onload = () => {
    if (xhr.status === 200) {
      this.refresh();
      return;
    }
    let message = 'upload failed';
    try { message = JSON.parse(xhr.responseText).error || message; } catch (e) {}
    label.className = 'failed small';
    label.textContent = message;
  };
  xhr.onerror = function () {
    label.className = 'failed small';
    label.textContent = 'upload failed';
  };
  xhr.send(file);
};

Library.prototype.remove = async function (entry) {
  /* Deleting a film is the only thing here that cannot be undone, so it asks,
   * and it says what else goes with it. */
  const alsoScore = entry.hasScore;
  const question = alsoScore
    ? 'Delete ' + entry.film + ' and its score? This cannot be undone.'
    : 'Delete ' + entry.film + '? This cannot be undone.';
  if (!window.confirm(question)) return;

  const url = '/api/delete?file=' + encodeURIComponent(entry.film) +
              (alsoScore ? '&score=1' : '');
  const res = await fetch(url, { method: 'DELETE' });
  if (!res.ok) {
    const body = await res.json().catch(function () { return {}; });
    window.alert('Could not delete: ' + (body.error || res.status));
  }
  await this.refresh();
};
