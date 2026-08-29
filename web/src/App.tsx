/* The studio, v2.
 *
 * Served at /v2 while the original keeps working at /, so the two can be
 * compared on the same score rather than one being replaced by trust. This
 * one is the timeline; the room preview still lives in the original.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { TimeView } from './core/view';
import { timecode, stepFrames, clamp } from './core/time';
import type { Rig, Score } from './core/score';
import { Timeline, TrackHeads } from './ui/Timeline';
import { Overview } from './ui/Overview';

interface Film { name: string; size: number; preview?: boolean }

export function App() {
  const [score, setScore] = useState<Score | null>(null);
  const [rig, setRig] = useState<Rig | null>(null);
  const [films, setFilms] = useState<Film[]>([]);
  const [film, setFilm] = useState<string>('');
  const [error, setError] = useState<string | null>(null);
  const [time, setTime] = useState(0);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [order, setOrder] = useState<string[]>([]);
  /* The view is a mutable object rather than state: it is written on every
   * pointer move, and routing that through React would re-render the whole
   * tree per event. This counter is what tells React something changed. */
  const [, bump] = useState(0);
  const onView = useCallback(() => bump((n) => n + 1), []);
  const video = useRef<HTMLVideoElement>(null);

  const duration = score?.duration ?? 60;
  const fps = score?.fps ?? 24;
  const view = useMemo(() => new TimeView(duration, fps), [duration, fps]);

  /* --- loading --- */

  useEffect(() => {
    let gone = false;
    (async () => {
      try {
        const [s, r, m] = await Promise.all([
          fetch('/api/score').then((x) => x.ok ? x.json() : Promise.reject(new Error('no score'))),
          fetch('/api/rig').then((x) => x.ok ? x.json() : null).catch(() => null),
          fetch('/api/media').then((x) => x.ok ? x.json() : []).catch(() => []),
        ]);
        if (gone) return;
        setScore(s);
        setRig(r);
        setFilms(m ?? []);
      } catch (e) {
        if (!gone) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => { gone = true; };
  }, []);

  const openFilm = useCallback(async (name: string) => {
    setFilm(name);
    setError(null);
    const res = await fetch('/api/score?film=' + encodeURIComponent(name));
    if (!res.ok) {
      setError('no score for ' + name + ' yet — analyse it in the original studio');
      return;
    }
    setScore(await res.json());
    setTime(0);
  }, []);

  /* --- transport --- */

  const seek = useCallback((t: number) => {
    const at = clamp(t, 0, duration);
    setTime(at);
    view.reveal(at);
    if (video.current && Number.isFinite(video.current.duration)) {
      video.current.currentTime = at;
    }
  }, [duration, view]);

  useEffect(() => {
    const v = video.current;
    if (!v) return;
    const on = () => { setTime(v.currentTime); view.reveal(v.currentTime); };
    v.addEventListener('timeupdate', on);
    return () => v.removeEventListener('timeupdate', on);
  }, [view]);

  /* --- keyboard ---
   *
   * The subset that is honest today. Frame stepping is exact in the score's
   * own arithmetic; whether the picture lands on that exact frame is up to the
   * browser, which does not promise it. Shuttle and the rest come with the
   * editing model.
   */
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (el && /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)) return;

      switch (e.key) {
        case ' ': {
          e.preventDefault();
          const v = video.current;
          if (v && Number.isFinite(v.duration)) { v.paused ? v.play() : v.pause(); }
          break;
        }
        case 'ArrowLeft':
          e.preventDefault();
          seek(stepFrames(time, e.shiftKey ? -fps : -1, fps, duration));
          break;
        case 'ArrowRight':
          e.preventDefault();
          seek(stepFrames(time, e.shiftKey ? fps : 1, fps, duration));
          break;
        case 'Home': e.preventDefault(); seek(0); break;
        case 'End': e.preventDefault(); seek(duration); break;
        case '+': case '=': view.zoomAt(view.fractionOf(time), 0.6); onView(); break;
        case '-': case '_': view.zoomAt(view.fractionOf(time), 1 / 0.6); onView(); break;
        case 'f': case 'F': view.fit(); onView(); break;
        case 'z': case 'Z': view.reset(); view.reveal(time); onView(); break;
        default: return;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [time, fps, duration, seek, view, onView]);

  /* --- arrangement --- */

  const toggleCollapse = useCallback((instrument: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      next.has(instrument) ? next.delete(instrument) : next.add(instrument);
      return next;
    });
  }, []);

  const move = useCallback((instrument: string, by: number) => {
    setOrder((prev) => {
      const ids = prev.length ? [...prev] : (score?.tracks ?? []).map((t) => t.instrument);
      const at = ids.indexOf(instrument);
      if (at < 0) return prev;
      const to = clamp(at + by, 0, ids.length - 1);
      ids.splice(to, 0, ids.splice(at, 1)[0]);
      return ids;
    });
  }, [score]);

  if (error && !score) return <div className="fail">{error}</div>;
  if (!score) return <div className="loading">loading…</div>;

  const playable = films.find((f) => f.name === film);

  return (
    <div className="app">
      <header className="bar">
        <h1>Componium <span className="dim">studio</span> <span className="tag">v2</span></h1>
        <select
          value={film}
          onChange={(e) => openFilm(e.target.value)}
          aria-label="Film"
        >
          <option value="">{score.title || '(score)'}</option>
          {films.map((f) => (
            <option key={f.name} value={f.name}>{f.name}</option>
          ))}
        </select>
        <span className="spacer" />
        <span className="tc" title="Timecode, HH:MM:SS:FF">{timecode(time, fps, { hours: true })}</span>
        <span className="dim small">{fps} fps</span>
        <span className="dim small">{Math.round(view.fraction * 100)}% shown</span>
      </header>

      {error && <p className="warn">{error}</p>}

      <div className="stage">
        {playable ? (
          <video
            ref={video}
            src={'/media?file=' + encodeURIComponent(film)}
            controls
            preload="metadata"
          />
        ) : (
          <p className="dim small hint">
            Pick a film to scrub against the picture. The timeline works without one.
          </p>
        )}
      </div>

      <section className="tl">
        <div className="tl-body">
          <TrackHeads
            score={score}
            rig={rig}
            collapsed={collapsed}
            order={order}
            onToggleCollapse={toggleCollapse}
            onMove={move}
          />
          <Timeline
            score={score}
            rig={rig}
            view={view}
            time={time}
            collapsed={collapsed}
            order={order}
            onSeek={seek}
            onView={onView}
          />
        </div>
        {/* Indented to sit under the lanes rather than under the whole panel,
            so the window box lines up with the time it represents. */}
        <div className="tl-under">
          <Overview score={score} rig={rig} view={view} time={time} onView={onView} />
        </div>
        <p className="legend dim small">
          wheel scrolls · ⇧/⌘ wheel zooms · drag the ruler to scrub · drag the strip below to move
          · <kbd>←</kbd><kbd>→</kbd> frame · <kbd>⇧</kbd>+ second · <kbd>F</kbd> fit · <kbd>Z</kbd> back to 10%
        </p>
      </section>
    </div>
  );
}
