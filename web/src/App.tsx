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
import { useEditing } from './ui/useEditing';
import { History } from './core/history';
import { Menu } from './ui/Menu';
import { Inspector } from './ui/Inspector';
import { canCollapse } from './core/layout';
import { menuFor } from './ui/menuItems';
import { addTrack, copy, missingInstruments, nudge, paste, splitCue, duplicateCues, type Clip } from './core/edits';
import type { Cue, Point } from './core/score';

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
  const history = useRef(new History()).current;
  const [saving, setSaving] = useState<string | null>(null);
  const [clipboard, setClipboard] = useState<Clip | null>(null);
  /* Shuttle speed, in the J/K/L sense: negative is backwards, and repeated
   * presses multiply rather than step, which is what makes it a shuttle. */
  const [shuttle, setShuttle] = useState(0);
  const [addMenu, setAddMenu] = useState<{ x: number; y: number } | null>(null);
  const [overlays, setOverlays] = useState({ calm: true, latency: true });
  /* useEditing needs to seek, seek needs the view, and the view is built
   * below. A ref breaks the cycle without either of them knowing about the
   * other's lifetime. */
  const seekRef = useRef<(t: number) => void>(() => {});

  const duration = score?.duration ?? 60;
  const fps = score?.fps ?? 24;
  const view = useMemo(() => new TimeView(duration, fps), [duration, fps]);

  const edit = useEditing({
    score: score ?? { title: '', duration: 60, tracks: [] },
    rig, view, history, time, fps,
    onSeek: (t) => seekRef.current(t),
    onChanged: onView,
  });

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
        void loadLayout();
      } catch (e) {
        if (!gone) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => { gone = true; };
  }, []);

  /* The arrangement lives beside the score, not in it. Loaded whenever the
   * open score changes, saved whenever it is rearranged. */
  const loadLayout = useCallback(async () => {
    try {
      const res = await fetch('/api/layout');
      if (!res.ok) return;
      const l = await res.json();
      setOrder(Array.isArray(l.order) ? l.order : []);
      setCollapsed(new Set(Array.isArray(l.collapsed) ? l.collapsed : []));
    } catch { /* an arrangement is a convenience, never a blocker */ }
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
    void loadLayout();
  }, [loadLayout]);

  /* --- transport --- */

  const seek = useCallback((t: number) => {
    const at = clamp(t, 0, duration);
    setTime(at);
    view.reveal(at);
    if (video.current && Number.isFinite(video.current.duration)) {
      video.current.currentTime = at;
    }
  }, [duration, view]);
  seekRef.current = seek;

  const save = useCallback(async () => {
    if (!score) return;
    setSaving('saving…');
    try {
      const res = await fetch('/api/score', {
        method: 'PUT',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(score),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        /* The server round trips through the same parser the player uses, so a
         * refusal here means the score is genuinely invalid rather than that
         * the request failed. Saying which turns a mystery into something
         * fixable — a curve left with one point, most likely. */
        setSaving('refused: ' + (body.error ?? res.status));
        return;
      }
      history.saved();
      setSaving('saved');
      setTimeout(() => setSaving(null), 1500);
      onView();
    } catch (e) {
      setSaving('failed: ' + (e instanceof Error ? e.message : String(e)));
    }
  }, [score, history, onView]);

  /**
   * The playhead follows the picture.
   *
   * Deliberately a prop on the element rather than an effect that reaches for
   * `video.current` and calls addEventListener. That effect was keyed on the
   * view, and the video element does not exist until a film is picked — which
   * happens later — so `video.current` was null when it ran, it returned
   * early, and the listener was never attached at all. The playhead simply
   * stopped following the film, silently, and only for the case where there
   * was a film to follow.
   *
   * React attaches this to whatever element is actually mounted, whenever that
   * happens, which makes the whole failure unrepresentable.
   */
  const follow = useCallback((t: number) => {
    setTime(t);
    view.reveal(t);
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
      /* Nothing to act on before the score arrives, and every branch below
       * assumes there is one. */
      if (!score) return;

      const mod = e.metaKey || e.ctrlKey;
      if (mod && (e.key === 'z' || e.key === 'Z')) {
        e.preventDefault();
        if (e.shiftKey ? history.redo() : history.undo()) onView();
        return;
      }
      if (mod && (e.key === 'y' || e.key === 'Y')) {
        e.preventDefault();
        if (history.redo()) onView();
        return;
      }
      if (mod && (e.key === 'a' || e.key === 'A')) { e.preventDefault(); edit.selectAll(); return; }
      if (mod && (e.key === 's' || e.key === 'S')) { e.preventDefault(); void save(); return; }
      if (mod && (e.key === 'c' || e.key === 'C')) {
        e.preventDefault();
        setClipboard(copy(score, edit.selected));
        return;
      }
      if (mod && (e.key === 'x' || e.key === 'X')) {
        e.preventDefault();
        setClipboard(copy(score, edit.selected));
        edit.deleteSelection();
        return;
      }
      if (mod && (e.key === 'v' || e.key === 'V')) {
        e.preventDefault();
        /* Paste into the track the selection came from, at the playhead. With
         * nothing selected there is no destination to infer, and guessing one
         * would drop events into a track nobody was looking at. */
        if (!clipboard) return;
        const target = (score.tracks ?? []).find((t) =>
          (t.cues ?? []).some((c) => edit.selected.has(c))
          || (t.points ?? []).some((p) => edit.selected.has(p)))
          ?? (score.tracks ?? []).find((t) =>
            clipboard.points.length ? t.type === 'curve' : t.type !== 'curve');
        if (!target) return;
        const cmd = paste(clipboard, target, time, score, rig);
        if (cmd) { history.run(cmd); history.seal(); onView(); }
        return;
      }
      if (mod && (e.key === 'd' || e.key === 'D')) {
        e.preventDefault();
        for (const t of score.tracks ?? []) {
          const cues = (t.cues ?? []).filter((c) => edit.selected.has(c));
          if (!cues.length) continue;
          const cmd = duplicateCues(t, cues);
          if (cmd) { history.run(cmd); history.seal(); onView(); }
        }
        return;
      }
      if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); edit.deleteSelection(); return; }
      if (e.key === 'Escape') { edit.clearSelection(); return; }

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

        /* J K L: an editor's hands go here before anywhere else. Repeated
         * presses multiply the speed, K stops, and pressing the opposite
         * direction returns to single speed rather than subtracting — which
         * is how every editor since tape has behaved. */
        case 'j': case 'J':
          e.preventDefault();
          setShuttle((s) => (s < 0 ? s * 2 : -1));
          break;
        case 'k': case 'K':
          e.preventDefault();
          setShuttle(0);
          video.current?.pause();
          break;
        case 'l': case 'L':
          e.preventDefault();
          setShuttle((s) => (s > 0 ? s * 2 : 1));
          break;

        case ',': {
          e.preventDefault();
          const cmd = nudge(score, edit.selected, -1 / fps);
          if (cmd) { history.run(cmd); history.seal(); onView(); }
          break;
        }
        case '.': {
          e.preventDefault();
          const cmd = nudge(score, edit.selected, 1 / fps);
          if (cmd) { history.run(cmd); history.seal(); onView(); }
          break;
        }
        case 's': case 'S': {
          e.preventDefault();
          /* Split whatever span the playhead is inside, in any selected
           * track — the closest thing to an editor's blade tool. */
          for (const t of score.tracks ?? []) {
            for (const c of [...(t.cues ?? [])]) {
              if (!edit.selected.has(c)) continue;
              const cmd = splitCue(t, c, time);
              if (cmd) { history.run(cmd); history.seal(); onView(); }
            }
          }
          break;
        }
        case '+': case '=': view.zoomAt(view.fractionOf(time), 0.6); onView(); break;
        case '-': case '_': view.zoomAt(view.fractionOf(time), 1 / 0.6); onView(); break;
        case 'f': case 'F': view.fit(); onView(); break;
        case 'z': case 'Z': view.reset(); view.reveal(time); onView(); break;
        default: return;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [time, fps, duration, seek, view, onView, history, edit, save, score, rig, clipboard]);

  /* The shuttle itself. Runs the transport at a multiple of speed when the
   * video can do it, and moves the playhead directly when there is no film —
   * so J and L work against a score alone, which is most of the time here. */
  useEffect(() => {
    if (!shuttle) {
      if (video.current) video.current.playbackRate = 1;
      return;
    }
    const v = video.current;
    if (v && Number.isFinite(v.duration) && shuttle > 0) {
      v.playbackRate = Math.min(16, shuttle);
      void v.play();
      return;
    }
    /* Backwards, or no film: step the clock ourselves. Browsers cannot play a
     * video backwards at all, so this is not a shortcut, it is the only way. */
    let last = performance.now();
    let raf = 0;
    const step = (now: number) => {
      const dt = (now - last) / 1000;
      last = now;
      seekRef.current(clamp(time + dt * shuttle, 0, duration));
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
    // `time` is deliberately out of the deps: including it restarts the loop
    // on every frame, which is a stutter rather than a shuttle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shuttle, duration]);

  /* --- arrangement --- */

  const toggleCollapse = useCallback((instrument: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      next.has(instrument) ? next.delete(instrument) : next.add(instrument);
      return next;
    });
  }, []);

  /* Saved on a timer rather than on every drag: reordering is a burst of small
   * changes and each one would otherwise be a request. */
  useEffect(() => {
    if (!order.length && !collapsed.size) return;
    const id = setTimeout(() => {
      void fetch('/api/layout', {
        method: 'PUT',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ order, collapsed: [...collapsed] }),
      }).catch(() => { /* losing an arrangement is not worth an error */ });
    }, 600);
    return () => clearTimeout(id);
  }, [order, collapsed]);

  /** Put an instrument at a position, for a drag rather than a nudge. */
  const moveTo = useCallback((instrument: string, before: string | null) => {
    setOrder((prev) => {
      const ids = prev.length ? [...prev] : (score?.tracks ?? []).map((t) => t.instrument);
      const from = ids.indexOf(instrument);
      if (from < 0) return prev;

      /* The target's index is taken *before* the removal, and reused after it.
       *
       * Looking it up afterwards is the obvious version and it cannot move a
       * track downwards at all: removing wind from [wind, light] leaves
       * [light], indexOf(light) is 0, and inserting there puts wind back
       * exactly where it started. Because the removal shifts everything after
       * it down by one, the pre-removal index lands the dragged track *after*
       * the target when moving down and *before* it when moving up — which is
       * what dropping onto something means in both directions.
       */
      let at = before === null ? ids.length : ids.indexOf(before);
      if (at < 0) at = ids.length;
      ids.splice(from, 1);
      ids.splice(at, 0, instrument);
      return ids;
    });
  }, [score]);

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
        <button
          className={'toggle' + (overlays.calm ? ' on' : '')}
          onClick={() => setOverlays((o) => ({ ...o, calm: !o.calm }))}
          title={score?.calm?.length
            ? 'Where the analysis decided to leave the film alone'
            : 'This score records no calm regions — rebuild it to get them'}
          disabled={!score?.calm?.length}
        >calm</button>
        <button
          className={'toggle' + (overlays.latency ? ' on' : '')}
          onClick={() => setOverlays((o) => ({ ...o, latency: !o.latency }))}
          title="When the conductor actually fires, against when the effect lands"
        >lead</button>
        {edit.selected.size > 0 && <span className="chip">{edit.selected.size} selected</span>}
        <button
          onClick={() => { if (history.undo()) onView(); }}
          disabled={!history.canUndo}
          title={history.undoLabel ? 'Undo ' + history.undoLabel : 'Nothing to undo'}
        >Undo</button>
        <button
          onClick={() => { if (history.redo()) onView(); }}
          disabled={!history.canRedo}
          title={history.redoLabel ? 'Redo ' + history.redoLabel : 'Nothing to redo'}
        >Redo</button>
        <button onClick={() => void save()} disabled={!history.dirty}>
          {saving ?? (history.dirty ? 'Save' : 'Saved')}
        </button>
      </header>

      {error && <p className="warn">{error}</p>}

      <div className="stage">
        {playable ? (
          <video
            ref={video}
            src={'/media?file=' + encodeURIComponent(film)}
            controls
            preload="metadata"
            data-testid="film"
            onTimeUpdate={(e) => follow(e.currentTarget.currentTime)}
            onSeeked={(e) => follow(e.currentTarget.currentTime)}
            onLoadedMetadata={(e) => follow(e.currentTarget.currentTime)}
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
            onMoveTo={moveTo}
            revision={history.version}
            onAddTrack={missingInstruments(score, rig).length
              ? (e) => setAddMenu({ x: e.clientX, y: e.clientY })
              : null}
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
            edit={edit}
            revision={history.version}
            overlays={overlays}
          />
        </div>
        {/* Indented to sit under the lanes rather than under the whole panel,
            so the window box lines up with the time it represents. */}
        <div className="tl-under">
          <Overview score={score} rig={rig} view={view} time={time} onView={onView} />
        </div>
        {addMenu && (
          <Menu
            x={addMenu.x}
            y={addMenu.y}
            onClose={() => setAddMenu(null)}
            items={[
              { label: 'Add a track', why: 'instruments the rig has that this score does not' },
              { separator: true },
              ...missingInstruments(score, rig).map((inst) => ({
                label: inst.id,
                key: inst.kind,
                run: () => {
                  history.run(addTrack(score, inst));
                  history.seal();
                  onView();
                },
              })),
            ]}
          />
        )}
        {edit.menu && (
          <Menu
            x={edit.menu.x}
            y={edit.menu.y}
            onClose={edit.closeMenu}
            items={menuFor({
              hit: edit.menu.hit,
              score, rig, history, time, fps,
              selected: edit.selected,
              clipboard,
              setClipboard,
              setSelected: (s: Set<Cue | Point>) => edit.setSelected(s),
              changed: onView,
              seek,
              zoomTo: (a, b) => { view.zoomTo(a, b); onView(); },
              toggleCollapse,
              canCollapse: (t) => canCollapse(t, rig),
            })}
          />
        )}
        <Inspector
          score={score}
          history={history}
          fps={fps}
          selection={edit.focus ? {
            track: score.tracks[edit.focus.track],
            cue: edit.focus.cue,
            point: edit.focus.point,
            channel: edit.focus.channel,
          } : null}
          onChanged={onView}
          onSeek={seek}
          onClose={edit.clearFocus}
        />
        <p className="legend dim small">
          wheel scrolls · ⇧/⌘ wheel zooms · drag the ruler to scrub · drag the strip below to move
          · <kbd>←</kbd><kbd>→</kbd> frame · <kbd>F</kbd> fit
          <br />
          drag an event to move it, its edges to trim · double click a lane to add a point,
          a point to remove it · drag empty space to select a range
          · <kbd>⌥</kbd> suspends snapping · <kbd>⇧</kbd> while dragging a point locks its time
          · <kbd>⌘Z</kbd> undo · <kbd>⌫</kbd> delete · <kbd>⌘S</kbd> save
          <br />
          right click anything for what you can do to it
          · <kbd>J</kbd><kbd>K</kbd><kbd>L</kbd> shuttle · <kbd>S</kbd> split at the playhead
          · <kbd>,</kbd><kbd>.</kbd> nudge a frame · <kbd>⌘C</kbd><kbd>⌘X</kbd><kbd>⌘V</kbd> · <kbd>⌘D</kbd> duplicate
          {shuttle !== 0 && <strong className="shuttle"> shuttle {shuttle > 0 ? '▶' : '◀'} {Math.abs(shuttle)}×</strong>}
        </p>
      </section>
    </div>
  );
}
