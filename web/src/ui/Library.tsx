/* The films, their scores, and the work running over them.
 *
 * Analysis is slow — a feature is tens of minutes — so nothing here happens
 * inline while somebody waits on a click. It is queued on the server, one at a
 * time, and this polls while anything is running and stops when nothing is.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Icon } from './Icon';
import { ALL, DEFAULT_PAGE_SIZE, PAGE_SIZES, matches, paginate } from '../core/paging';
import { Steps, howLong, type Step } from './Steps';
import { Progress } from './Progress';
import { Vision } from './Vision';
import type { Fps } from '../core/time';

const POLL_MS = 700;

interface Chunk {
  index: number;
  from: number;
  to: number;
  state: 'queued' | 'running' | 'done' | 'failed';
  error?: string;
  seconds?: number;
}

interface Job {
  kind: string;
  state: 'queued' | 'running' | 'done' | 'failed' | 'interrupted';
  progress: number;
  label: string;
  error?: string;
  seconds?: number;
  /* The analysis broken into ranges. Absent until a film has been planned,
   * and the whole reason an interrupted feature is worth resuming rather than
   * starting again. */
  chunks?: Chunk[];
  /* What this run did and what each part cost. An analysis is several passes
   * with very different costs, so which part took the time is the question
   * actually asked. */
  steps?: Step[];
}

interface Build {
  id: string;
  label: string;
  note?: string;
  cues: number;
  seconds?: number;
  steps?: Step[];
}

/** How many pieces of this film are finished and will not be redone. */
const done = (job?: Job) => (job?.chunks ?? []).filter((c) => c.state === 'done').length;

/** Is there finished work here worth continuing from. */
const resumable = (job?: Job) =>
  !!job && job.state !== 'running' && job.state !== 'queued' && done(job) > 0
  && done(job) < (job.chunks?.length ?? 0);

interface Entry {
  film: string;
  size: number;
  hasScore: boolean;
  scoreName?: string;
  tracks?: number;
  cues?: number;
  duration?: number;
  preview?: boolean;
  /* Whether a description is kept for this film — what the model said, which
   * a rebuild reuses unless it is told otherwise. */
  seen?: boolean;
  job?: Job;
  prepare?: Job;
  /* Every score kept for this film, newest first. Sent with the listing so a
   * row can show its history without a request of its own. */
  builds?: Build[];
}

interface View {
  scores: string;
  free: number;
  canBuild: boolean;
  canUpload: boolean;
  canPrepare: boolean;
  current: string;
  entries: Entry[];
}

const busy = (e: Entry) =>
  [e.job, e.prepare].some((j) => j && (j.state === 'queued' || j.state === 'running'));

const megabytes = (b: number) => (b / (1024 * 1024)).toFixed(0) + ' MB';

const clock = (s: number) =>
  Math.floor(s / 60) + 'm' + String(Math.round(s % 60)).padStart(2, '0');

export function Library(props: { onOpen: (film: string) => void; fps: Fps }) {
  const [data, setData] = useState<View | null>(null);
  const [uploading, setUploading] = useState<{ name: string; percent: number } | null>(null);
  const polling = useRef(false);
  const file = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  /* Which film's description is open for reading. One at a time: it is a full
   * panel, and comparing two descriptions is a different job from reading one. */
  const [reading, setReading] = useState<string | null>(null);
  /* Which film's steps are open. One at a time: a page of ten expanded runs
   * is a wall, and comparing two is done in the version picker anyway. */
  const [open, setOpen] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  /* The page size is remembered and the filter is not. One is how you like to
   * work; the other is what you were looking for a minute ago, and having it
   * still applied on the next visit reads as an empty library. */
  const [size, setSize] = useState(() => {
    try {
      const raw = localStorage.getItem('componium.libraryPage');
      /* Test the key, not the value. Number(null) is 0, and 0 is the size
       * meaning "show all" — so a first visit read as a deliberate choice to
       * paginate nothing, and the library opened with every film on one page. */
      if (raw === null || raw === '') return DEFAULT_PAGE_SIZE;
      const n = Number(raw);
      return PAGE_SIZES.includes(n as never) || n === ALL ? n : DEFAULT_PAGE_SIZE;
    } catch {
      return DEFAULT_PAGE_SIZE;
    }
  });
  useEffect(() => {
    try { localStorage.setItem('componium.libraryPage', String(size)); } catch { /* private mode */ }
  }, [size]);

  const refresh = useCallback(async () => {
    const res = await fetch('/api/library');
    if (!res.ok) return null;
    const next: View = await res.json();
    setData(next);
    return next;
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  /* Poll only while something is actually running. A studio sitting idle
   * should not be asking the server a question every second forever. */
  useEffect(() => {
    if (!data || polling.current) return;
    if (!(data.entries ?? []).some(busy)) return;
    polling.current = true;
    const tick = async () => {
      const next = await refresh();
      if (next && (next.entries ?? []).some(busy)) setTimeout(tick, POLL_MS);
      else polling.current = false;
    };
    setTimeout(tick, POLL_MS);
  }, [data, refresh]);

  const post = async (url: string) => {
    await fetch(url, { method: 'POST' });
    await refresh();
  };

  /* XMLHttpRequest rather than fetch, because fetch reports no progress on the
   * way *up* and a film is gigabytes. A browser sitting silent for four
   * minutes is indistinguishable from one that has hung. */
  const upload = (f: File) => {
    setUploading({ name: f.name, percent: 0 });
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/upload?name=' + encodeURIComponent(f.name));
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) {
        setUploading({ name: f.name, percent: Math.round((e.loaded / e.total) * 100) });
      }
    };
    xhr.onload = () => { setUploading(null); void refresh(); };
    xhr.onerror = () => setUploading(null);
    xhr.send(f);
  };

  /* Reset is asked about rather than done, because the thing it discards is
   * the expensive thing: on a feature, the finished pieces can be an hour of
   * work that nothing else in the studio can get back. */
  const reset = async (e: Entry) => {
    const n = (e.job?.chunks ?? []).filter((c) => c.state === 'done').length;
    const what = n > 0
      ? `Throw away ${n} finished piece${n === 1 ? '' : 's'} of ${e.film} and analyse it again from the start?`
      : `Start the analysis of ${e.film} again from nothing?`;
    if (!window.confirm(what)) return;
    await post('/api/build?reset=1&file=' + encodeURIComponent(e.film));
  };

  /* Showing a film to the model again.
   *
   * Confirmed in the reading room rather than here, so that the confirmation
   * can say what is being thrown away and the person answering has just been
   * looking at it. This only sends the request. */
  const lookAgain = async (film: string) => {
    await post('/api/build?vision=redo&file=' + encodeURIComponent(film));
  };

  const remove = async (e: Entry) => {
    const what = e.hasScore
      ? `Delete ${e.film} and its score? This cannot be undone.`
      : `Delete ${e.film}? This cannot be undone.`;
    if (!window.confirm(what)) return;
    await fetch('/api/delete?file=' + encodeURIComponent(e.film) + (e.hasScore ? '&score=1' : ''),
      { method: 'DELETE' });
    await refresh();
  };

  const shown = useMemo(
    () => paginate(matches(data?.entries ?? [], query, (e) => e.film), page, size),
    [data, query, page, size]);

  /* Back to the first page whenever the filter changes: staying on page three
   * of a search that now matches two things shows nothing at all. */
  useEffect(() => { setPage(1); }, [query, size]);

  if (!data) return <p className="dim small">loading the library…</p>;

  return (
    <div className="lib">
      {reading && (
        <Vision
          film={reading}
          fps={props.fps}
          onClose={() => setReading(null)}
          onLookAgain={() => { void lookAgain(reading); }}
        />
      )}
      <div className="lib-head">
        <span className="dim small">
          {data.canBuild ? 'scores in ' + (data.scores || '(none)')
            : 'no composer found, so films cannot be analysed here'}
          {data.free > 0 && '  ·  ' + megabytes(data.free) + ' free'}
        </span>
        <div className="lib-actions">
          {data.canUpload && (
            <>
              <input
                ref={file} type="file" hidden
                accept="video/*,.mkv,.mp4,.webm,.mov,.m4v"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (f) upload(f);
                  e.target.value = '';
                }}
              />
              <button onClick={() => file.current?.click()}>Upload film</button>
            </>
          )}
          {data.canPrepare && (
            <button
              onClick={() => post('/api/prepare?all=1')}
              title="Make a browser-playable copy of every film that has not got one"
            >Prepare all</button>
          )}
          {data.canBuild && (
            <button onClick={() => post('/api/build?all=1')} title="Queue every film, one at a time">
              Rebuild all
            </button>
          )}
        </div>
      </div>

      <div className="lib-tools">
        <label className="lib-find">
          <Icon name="search" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="filter films"
            aria-label="Filter films"
          />
        </label>
        <span className="dim small lib-count">
          {shown.total === 0
            ? (query ? 'nothing matches' : 'no films yet')
            : shown.pages > 1
              ? `${shown.first}–${shown.last} of ${shown.total}`
              : `${shown.total} film${shown.total === 1 ? '' : 's'}`}
        </span>
        <select
          value={size}
          onChange={(e) => setSize(Number(e.target.value))}
          aria-label="Films per page"
          title="How many films to show at once"
        >
          {PAGE_SIZES.map((n) => <option key={n} value={n}>{n} per page</option>)}
          <option value={ALL}>show all</option>
        </select>
      </div>

      {uploading && (
        <div className="lib-row">
          <div className="lib-name">{uploading.name}</div>
          <div className="lib-status">
            <Progress value={uploading.percent / 100} label={'uploading ' + uploading.name} />
          </div>
        </div>
      )}

      {shown.items.map((e) => (
        <div key={e.film} className={'lib-row' + (e.scoreName === data.current ? ' current' : '')}>
          <div className="lib-name">
            {e.film} <span className="dim">{megabytes(e.size)}</span>
          </div>
          <div className="lib-status">{status(e)}</div>
          {/* Fixed slots, always in the same order, empty where an action
              does not apply. Rows used to lay their buttons out left to
              right, so a film with no Prepare button put Delete where its
              neighbour put Rebuild — the whole column shifted from row to
              row, and the button under the pointer changed as the list
              refreshed underneath it. */}
          <div className="lib-actions">
            <span className="slot">
              {e.hasScore && <button onClick={() => props.onOpen(e.film)}>Open</button>}
            </span>
            <span className="slot">
              {data.canBuild && (
                <button
                  disabled={!!(e.job && (e.job.state === 'queued' || e.job.state === 'running'))}
                  onClick={() => post('/api/build?file=' + encodeURIComponent(e.film))}
                  title={resumable(e.job)
                    ? `Continue from piece ${done(e.job)} of ${e.job!.chunks!.length}. ` +
                      'The finished pieces are kept.'
                    : 'Analyse the whole film, in pieces that can be resumed.'
                      + (e.seen
                        ? ' What the model already said is reused — open vision'
                          + ' to read it, or to ask it to look again.'
                        : '')}
                >{resumable(e.job) ? 'Resume' : e.hasScore ? 'Rebuild' : 'Analyse'}</button>
              )}
            </span>
            <span className="slot">
              {data.canBuild && (e.job?.chunks?.length ?? 0) > 0
                && e.job?.state !== 'running' && e.job?.state !== 'queued' && (
                <button
                  onClick={() => reset(e)}
                  title="Throw away every finished piece and analyse the film again from nothing"
                >Reset</button>
              )}
            </span>
            <span className="slot">
              {data.canPrepare && !e.preview && (
                <button
                  disabled={!!(e.prepare && (e.prepare.state === 'queued' || e.prepare.state === 'running'))}
                  onClick={() => post('/api/prepare?file=' + encodeURIComponent(e.film))}
                  title="Make a copy this browser can play. Usually quick: the video is only re-encoded when it has to be."
                >Prepare</button>
              )}
            </span>
            <span className="slot">
              {e.seen && (
                <button
                  onClick={() => setReading(e.film)}
                  title="Read what the model said about this film, and ask it to look again"
                >vision</button>
              )}
            </span>
            <span className="slot">
              {((e.job?.steps?.length ?? 0) > 0 || (e.builds?.length ?? 0) > 0) && (
                <button
                  onClick={() => setOpen(open === e.film ? null : e.film)}
                  aria-expanded={open === e.film}
                  title="Every build of this film, and what each step cost"
                >{open === e.film ? 'hide' : `builds${e.builds?.length ? ' ' + e.builds.length : ''}`}</button>
              )}
            </span>
            <span className="slot slot-icon">
              {data.canUpload && (
                <button
                  className="danger icon-btn"
                  onClick={() => remove(e)}
                  title={'Delete ' + e.film}
                  aria-label={'Delete ' + e.film}
                ><Icon name="trash" /></button>
              )}
            </span>
          </div>
          {open === e.film && (
            <div className="lib-steps">
              {/* The run happening now, if one is, above the ones that are
                  finished — it is the one being watched. */}
              {e.job?.state === 'running' && e.job.steps && (
                <div className="build build-now">
                  <div className="build-head">
                    <strong>running now</strong>
                    <span className="dim small">{e.job.label}</span>
                  </div>
                  <Steps steps={e.job.steps} />
                </div>
              )}
              {(e.builds ?? []).map((b) => (
                <div className="build" key={b.id}>
                  <div className="build-head">
                    <strong>{b.label}</strong>
                    <span className="dim small">{b.note}</span>
                    {b.seconds ? <span className="build-took small">{howLong(b.seconds)}</span> : null}
                  </div>
                  {b.steps?.length
                    ? <Steps steps={b.steps} total={b.seconds} />
                    : <p className="dim small build-none">
                        Made before the steps were recorded, so there is nothing to show.
                      </p>}
                </div>
              ))}
              {!(e.builds ?? []).length && e.job?.state !== 'running' && (
                <p className="dim small build-none">No builds kept for this film yet.</p>
              )}
            </div>
          )}
        </div>
      ))}

      {shown.total === 0 && (
        <p className="dim small lib-empty">
          {query
            ? `Nothing matches “${query}”.`
            : 'No films here yet. Upload one to get started.'}
        </p>
      )}

      {shown.pages > 1 && (
        <div className="lib-pages">
          <button
            onClick={() => setPage((n) => n - 1)}
            disabled={shown.page <= 1}
            aria-label="Previous page"
            title="Previous page"
            className="icon-btn"
          ><Icon name="left" /></button>
          <span className="dim small">page {shown.page} of {shown.pages}</span>
          <button
            onClick={() => setPage((n) => n + 1)}
            disabled={shown.page >= shown.pages}
            aria-label="Next page"
            title="Next page"
            className="icon-btn"
          ><Icon name="right" /></button>
        </div>
      )}
    </div>
  );
}

function status(e: Entry) {
  const j = e.job;
  if (j && (j.state === 'queued' || j.state === 'running')) {
    return (
      <>
        <Progress
          value={j.progress}
          label={j.state === 'queued' ? 'queued' : j.label}
        />
      </>
    );
  }
  if (j?.state === 'failed') {
    /* The composer's own last words rather than a generic failure: knowing it
     * was ffmpeg that objected is most of the diagnosis. And which piece it
     * got to, because that is what Resume will start from. */
    const kept = (j.chunks ?? []).filter((c) => c.state === 'done').length;
    return (
      <span className="failed small">
        failed: {j.error ?? 'unknown'}
        {kept > 0 && ` — ${kept} of ${j.chunks!.length} pieces finished and kept`}
      </span>
    );
  }
  if (j?.state === 'interrupted') {
    /* Say it was killed rather than saying nothing. Silence reads as "it never
     * started", which is the opposite of the truth. And say how much survived,
     * because "stopped at 60%" and "stopped at 60%, and 60% of it is kept" are
     * very different pieces of news. */
    const kept = (j.chunks ?? []).filter((c) => c.state === 'done').length;
    const all = j.chunks?.length ?? 0;
    return (
      <span className="dim small">
        analysis stopped when the studio restarted
        {all > 0
          ? ` — ${kept} of ${all} pieces finished and kept`
          : ` at ${Math.round((j.progress ?? 0) * 100)}%`}
      </span>
    );
  }

  const prep = e.prepare;
  const note = prep && (prep.state === 'queued' || prep.state === 'running')
    ? ` · preview ${Math.round((prep.progress ?? 0) * 100)}%`
    : prep?.state === 'failed' ? ' · preview failed'
      : e.preview ? ' · browser copy ready' : '';

  if (e.hasScore) {
    return (
      <span className="dim small">
        {e.tracks} tracks, {e.cues} cues, {clock(e.duration ?? 0)}{note}
      </span>
    );
  }
  return <span className="dim small">no score yet{note}</span>;
}
