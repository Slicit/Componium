/* The films, their scores, and the work running over them.
 *
 * Analysis is slow — a feature is tens of minutes — so nothing here happens
 * inline while somebody waits on a click. It is queued on the server, one at a
 * time, and this polls while anything is running and stops when nothing is.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

const POLL_MS = 700;

interface Job {
  kind: string;
  state: 'queued' | 'running' | 'done' | 'failed' | 'interrupted';
  progress: number;
  label: string;
  error?: string;
  seconds?: number;
}

interface Entry {
  film: string;
  size: number;
  hasScore: boolean;
  scoreName?: string;
  tracks?: number;
  cues?: number;
  duration?: number;
  preview?: boolean;
  job?: Job;
  prepare?: Job;
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

export function Library(props: { onOpen: (film: string) => void }) {
  const [data, setData] = useState<View | null>(null);
  const [uploading, setUploading] = useState<{ name: string; percent: number } | null>(null);
  const polling = useRef(false);
  const file = useRef<HTMLInputElement>(null);

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

  const remove = async (e: Entry) => {
    const what = e.hasScore
      ? `Delete ${e.film} and its score? This cannot be undone.`
      : `Delete ${e.film}? This cannot be undone.`;
    if (!window.confirm(what)) return;
    await fetch('/api/delete?file=' + encodeURIComponent(e.film) + (e.hasScore ? '&score=1' : ''),
      { method: 'DELETE' });
    await refresh();
  };

  if (!data) return <p className="dim small">loading the library…</p>;

  return (
    <div className="lib">
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

      {uploading && (
        <div className="lib-row">
          <div className="lib-name">{uploading.name}</div>
          <div className="lib-status">
            <div className="bar"><div className="fill" style={{ width: uploading.percent + '%' }} /></div>
            <span className="dim small">uploading {uploading.percent}%</span>
          </div>
        </div>
      )}

      {data.entries?.map((e) => (
        <div key={e.film} className={'lib-row' + (e.scoreName === data.current ? ' current' : '')}>
          <div className="lib-name">
            {e.film} <span className="dim">{megabytes(e.size)}</span>
          </div>
          <div className="lib-status">{status(e)}</div>
          <div className="lib-actions">
            {e.hasScore && <button onClick={() => props.onOpen(e.film)}>Open</button>}
            {data.canBuild && (
              <button
                disabled={!!(e.job && (e.job.state === 'queued' || e.job.state === 'running'))}
                onClick={() => post('/api/build?file=' + encodeURIComponent(e.film))}
              >{e.hasScore ? 'Rebuild' : 'Analyse'}</button>
            )}
            {data.canPrepare && !e.preview && (
              <button
                disabled={!!(e.prepare && (e.prepare.state === 'queued' || e.prepare.state === 'running'))}
                onClick={() => post('/api/prepare?file=' + encodeURIComponent(e.film))}
                title="Make a copy this browser can play. Usually quick: the video is only re-encoded when it has to be."
              >Prepare</button>
            )}
            {data.canUpload && <button className="danger" onClick={() => remove(e)}>Delete</button>}
          </div>
        </div>
      ))}
    </div>
  );
}

function status(e: Entry) {
  const j = e.job;
  if (j && (j.state === 'queued' || j.state === 'running')) {
    return (
      <>
        <div className="bar"><div className="fill" style={{ width: Math.round(j.progress * 100) + '%' }} /></div>
        <span className="dim small">
          {j.state === 'queued' ? 'queued' : `${j.label}  ${Math.round(j.progress * 100)}%`}
        </span>
      </>
    );
  }
  if (j?.state === 'failed') {
    /* The composer's own last words rather than a generic failure: knowing it
     * was ffmpeg that objected is most of the diagnosis. */
    return <span className="failed small">failed: {j.error ?? 'unknown'}</span>;
  }
  if (j?.state === 'interrupted') {
    /* Say it was killed rather than saying nothing. Silence reads as "it never
     * started", which is the opposite of the truth. */
    return (
      <span className="dim small">
        analysis stopped at {Math.round((j.progress ?? 0) * 100)}% when the studio restarted
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
