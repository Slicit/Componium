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
import { Room } from './ui/room/Room';
import { Force } from './ui/Force';
import { Library } from './ui/Library';
import { COLUMNS, useDrag } from './ui/useSplit';
import { useViewport } from './ui/useViewport';
import { Viewports } from './ui/Viewports';
import { filmForScore } from './core/film';
import { useVersions } from './ui/useVersions';
import { Effects } from './ui/Effects';
import { insertPreset } from './core/edits';
import { channelsOf, kindOf } from './core/score';
import { followFrames } from './ui/frameClock';
import { settingOf, writeSetting } from './core/settings';
import { isTyping } from './core/typing';
import { useLive } from './ui/useLive';
import { LiveTrim } from './ui/LiveTrim';
import type { Preset } from './core/presets';
import { canCollapse } from './core/layout';
import { menuFor } from './ui/menuItems';
import { addTrack, copy, missingInstruments, nudge, paste, splitCue, duplicateCues, type Clip } from './core/edits';
import type { Cue, Point } from './core/score';

interface Film { name: string; size: number; preview?: boolean }

/* Hoisted rather than written inline in the markup.
 *
 * A fresh Set on every render is a changed dependency on every render, and the
 * room re-evaluates every track of the score when its dependencies change —
 * so an empty set built in place cost a full pass over the score for each
 * keystroke. Nothing in the studio mutes anything yet; when something does,
 * this becomes state.
 */
const NO_MUTES = new Set<string>();

export function App({ active = true }: { active?: boolean } = {}) {
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
  /* Written by the callback ref below, so the element type has to admit
   * null. A RefObject React fills in is read only to us. */
  const video = useRef<HTMLVideoElement | null>(null);
  /* The same element, as state, so the room can be handed it.
   *
   * A ref alone cannot do this: it is populated during commit and never
   * re-renders anything, so a room reading `video.current` would see null on
   * the render that matters and never look again. A callback ref is React
   * telling us the element exists, which is the only reliable moment. */
  const [picture, setPicture] = useState<HTMLVideoElement | null>(null);
  const holdVideo = useCallback((el: HTMLVideoElement | null) => {
    video.current = el;
    setPicture(el);
  }, []);
  /* Off unless asked for. The screen in the room previews the ambient layer;
   * a film on it is a different thing to look at, and a gimmick until judged
   * otherwise, so it is opt in and it is remembered. */
  const [onScreen, setOnScreen] = useState(() => {
    try { return localStorage.getItem('componium.roomPicture') === 'on'; }
    catch { return false; }
  });
  useEffect(() => {
    try {
      localStorage.setItem('componium.roomPicture', onScreen ? 'on' : 'off');
    } catch { /* private mode */ }
  }, [onScreen]);
  /* Throwing the picture into the room. A television does not do this, so it
   * is its own switch rather than something the picture brings with it. */
  const [projecting, setProjecting] = useState(() => {
    try { return localStorage.getItem('componium.roomProjection') === 'on'; }
    catch { return false; }
  });
  useEffect(() => {
    try {
      localStorage.setItem('componium.roomProjection', projecting ? 'on' : 'off');
    } catch { /* private mode */ }
  }, [projecting]);
  const history = useRef(new History()).current;
  const [saving, setSaving] = useState<string | null>(null);
  const [clipboard, setClipboard] = useState<Clip | null>(null);
  /* Shuttle speed, in the J/K/L sense: negative is backwards, and repeated
   * presses multiply rather than step, which is what makes it a shuttle. */
  const [shuttle, setShuttle] = useState(0);
  const [addMenu, setAddMenu] = useState<{ x: number; y: number } | null>(null);
  const [overlays, setOverlays] = useState({ calm: true, latency: true });
  const [forced, setForced] = useState<Map<string, number>>(new Map());
  /* Low, because the room is for judging what the score is doing and a room
   * lit like a showroom hides it. The slider reaches nothing at all from here,
   * which is the state most of this is worth looking at in. */
  const [brightness, setBrightness] = useState(() => settingOf('roomLight'));
  /* How strong the soft wash is. Remembered, because it is a judgement about a
   * particular screen in a particular room and re-making it every session is
   * the sort of small tax that makes a tool tiring. */
  const [wash, setWash] = useState(() => settingOf('roomWash'));
  useEffect(() => { writeSetting('roomWash', wash); }, [wash]);
  useEffect(() => { writeSetting('roomLight', brightness); }, [brightness]);
  const views = useViewport();
  const split = views.viewport;
  const stage = useRef<HTMLDivElement>(null);
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
        const media = m ?? [];
        setFilms(media);
        /* Open the film this score was made from.
         *
         * The studio starts holding a score and nothing else, so without this
         * the picture pane shows its "pick a film" hint over a score that is
         * obviously already about one particular film, and the timeline scrubs
         * against nothing until somebody works out that the dropdown in the
         * corner is the thing to touch. There is no ambiguity to preserve: a
         * score is named after its film and at most one film can match.
         */
        setFilm(filmForScore(s?.path, media));
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

  /* Open a kept score for review.
   *
   * The film stays where it is: comparing an old version against a new one is
   * about the same film, and reloading the picture under it would lose the
   * playhead for no reason. An empty id means the live score. */
  const openVersion = useCallback(async (id: string) => {
    setError(null);
    const q = id
      ? '/api/score?film=' + encodeURIComponent(film) + '&version=' + encodeURIComponent(id)
      : '/api/score?film=' + encodeURIComponent(film);
    const res = await fetch(q);
    if (!res.ok) {
      setError('could not open that version');
      return;
    }
    const next = await res.json();
    setScore(next);
    history.reset();
    onView();
  }, [film, history, onView]);

  const versions = useVersions(film, openVersion);

  /* The track the effects library is talking about: whatever was last touched
   * in the timeline. Deliberately the focus rather than the selection — you
   * pick a track by clicking in it, and requiring something to be selected
   * first would mean selecting a point in order to be offered a shape that
   * replaces it. */
  const target = (edit.focus && score?.tracks?.[edit.focus.track]) || null;
  const targetKind = target ? kindOf(target.instrument, rig) : '';

  /* Previewing drives the room through the same force map the sliders use, so
   * the room has one idea of "this device is at this level regardless of the
   * score" rather than two that have to agree. */
  const preview = useCallback((instrument: string, level: number | null) => {
    setForced((was) => {
      const next = new Map(was);
      if (level === null) next.delete(instrument); else next.set(instrument, level);
      return next;
    });
  }, []);

  const insert = useCallback((preset: Preset) => {
    if (!target || !score) return;
    const cmd = insertPreset(target, preset, time, channelsOf(target, rig), {}, rig);
    if (!cmd) return;
    history.run(cmd);
    history.seal();
    onView();
  }, [target, score, rig, time, history, onView]);

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
  /* Whether the film is running, readable from callbacks that must not be
   * rebuilt every time it changes. */
  const playingNow = useRef(false);

  const live = useLive();
  const liveRef = useRef(live.follow);
  liveRef.current = live.follow;

  const follow = useCallback((t: number) => {
    setTime(t);
    view.reveal(t);
    /* And to the room itself, when it is armed. The same playhead drives the
     * preview and the hardware, which is the entire point: an effect that
     * lands wrong in the room lands wrong on the wall. */
    liveRef.current(t, playingNow.current);
  }, [view]);

  /* Held in a ref because the loop below must not be restarted when `follow`
   * is rebuilt. It is rebuilt whenever the viewport changes, which is once per
   * scroll, and a loop torn down and started again mid-scroll drops frames for
   * a reason that has nothing to do with the film. */
  const followRef = useRef(follow);
  followRef.current = follow;

  /* While the film plays, the playhead comes from the film's own frames.
   *
   * `timeupdate` is throttled to about four a second by every browser, which
   * is below what any of this is for: a strobe of twelve pulses over two
   * seconds is a 6Hz square wave, and four samples a second cannot reconstruct
   * it. The room was never struggling to draw it — it was being handed four
   * light values a second. See ui/frameClock.ts. */
  const [playing, setPlaying] = useState(false);
  playingNow.current = playing;
  useEffect(() => {
    const v = video.current;
    if (!playing || !v) return;
    return followFrames(v, (t) => followRef.current(t));
  }, [playing, film]);

  /* --- keyboard ---
   *
   * The subset that is honest today. Frame stepping is exact in the score's
   * own arithmetic; whether the picture lands on that exact frame is up to the
   * browser, which does not promise it. Shuttle and the rest come with the
   * editing model.
   */
  useEffect(() => {
    /* Not while somebody is somewhere else. The studio stays mounted behind
     * the admin so that it keeps its score and its undo history, and a global
     * key listener does not care that its own subtree is hidden: Delete was
     * still deleting the selection while a person was filling in a form two
     * sections away. */
    if (!active) return;

    const onKey = (e: KeyboardEvent) => {
      /* composedPath, not target. An event crossing a shadow boundary is
       * retargeted to the host, so an input inside a web component arrives
       * claiming to be that component and a guard written against tagName
       * waves it straight through. See core/typing.ts. */
      if (isTyping(e)) return;
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
  }, [active, time, fps, duration, seek, view, onView, history, edit, save, score, rig, clipboard]);

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

  /* Both drags are measured against the stage's own box, so they mean the same
   * thing at any window size and the split survives a resize. */
  const dragSplit = useDrag((e) => {
    const box = stage.current?.getBoundingClientRect();
    if (!box || box.width <= 0) return;
    views.setColumns(((e.clientX - box.left) / box.width) * COLUMNS);
  });
  const dragHeight = useDrag((e) => {
    const box = stage.current?.getBoundingClientRect();
    if (!box) return;
    views.setHeight(e.clientY - box.top);
  });

  if (error && !score) return <div className="fail">{error}</div>;
  if (!score) return <div className="loading">loading…</div>;

  const playable = films.find((f) => f.name === film);

  return (
    <div className="app">
      {live.state.problem && !live.armed && (
        <p className="bar-problem" role="alert">
          <strong>Not live:</strong> {live.state.problem}
          <button className="bar-dismiss" onClick={live.forget}
                  aria-label="Dismiss">dismiss</button>
        </p>
      )}
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
        <select
          className="versions"
          value={versions.current}
          onChange={(e) => versions.select(e.target.value)}
          disabled={!film || versions.list.length === 0}
          aria-label="Score version"
          title={versions.list.length === 0
            ? 'No earlier scores kept for this film yet'
            : 'Scores kept from earlier analyses of this film'}
        >
          <option value="">
            {versions.list.length === 0 ? 'no history' : 'latest'}
          </option>
          {versions.list.map((v) => (
            <option key={v.id} value={v.id} title={v.note}>{v.label}</option>
          ))}
        </select>
        <span className="spacer" />
        <span className="tc" title="Timecode, HH:MM:SS:FF">{timecode(time, fps, { hours: true })}</span>
        <span className="dim small">{fps} fps</span>
        <span className="dim small">{Math.round(view.fraction * 100)}% shown</span>
        <button
          className={'toggle' + (split.room ? ' on' : '')}
          onClick={() => views.setRoom(!split.room)}
          title="The room preview"
        >room</button>
        <button
          className={'toggle' + (split.force ? ' on' : '')}
          onClick={() => views.setForce(!split.force)}
          disabled={!split.room}
          title={split.room
            ? 'The sliders that force one effect on, whatever the score says'
            : 'The sliders live in the room pane, which is hidden'}
        >sliders</button>
        <Viewports
          viewport={split}
          saved={views.saved}
          onSave={views.save}
          onApply={views.apply}
          onRemove={views.remove}
          onReset={views.reset}
        />
        <button
          className={'toggle live' + (live.armed ? ' on' : '')}
          onClick={() => (live.armed ? live.disarm() : live.arm())}
          title={live.armed
            ? 'Driving the rig from this playhead. Click to stop.'
            : 'Drive the real rig from this playhead, through the same clock '
              + 'and safety supervisor a show uses'}
        >{live.armed ? 'live' : 'go live'}</button>
        {live.armed && (
          <span
            className={'chip' + (live.state.real === 0 ? ' warn' : '')}
            title={live.state.real === 0
              ? 'Every instrument in this rig is virtual, so nothing physical '
                + 'will move. The conductor is logging what it would have sent.'
              : live.state.cues + ' cues and ' + live.state.curves
                + ' curve updates sent, ' + Math.round(live.state.precision * 1000)
                + 'ms precision'}
          >
            {live.state.real === 0
              ? 'all virtual'
              : live.state.real + ' live'}
          </span>
        )}
        {live.armed && <LiveTrim lights={live.state.lights ?? []} />}
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

      <div
        className="stage"
        ref={stage}
        style={{
          height: split.height,
          gridTemplateColumns: split.room
            ? `${split.columns}fr 10px ${COLUMNS - split.columns}fr`
            : '1fr',
        }}
      >
        <div className="stage-film">
        {playable ? (
          <video
            ref={holdVideo}
            src={'/media?file=' + encodeURIComponent(film)}
            controls
            preload="metadata"
            data-testid="film"
            /* Only while stopped. Playing, the frame clock owns the
               playhead, and `currentTime` is a slightly later number than the
               presented frame's own — interleaving the two makes a playhead
               that steps backwards several times a second. */
            onTimeUpdate={(e) => {
              if (e.currentTarget.paused) follow(e.currentTarget.currentTime);
            }}
            onSeeked={(e) => follow(e.currentTarget.currentTime)}
            onLoadedMetadata={(e) => follow(e.currentTarget.currentTime)}
            onPlay={() => { setPlaying(true); live.follow(time, true); }}
            onPause={() => { setPlaying(false); live.follow(video.current?.currentTime ?? time, false); }}
            onEnded={() => { setPlaying(false); live.follow(video.current?.currentTime ?? time, false); }}
          />
        ) : (
          <p className="dim small hint">
            Pick a film to scrub against the picture. The timeline works without one.
          </p>
        )}
        </div>

        {split.room && (
          <div
            className="split-v"
            onPointerDown={dragSplit}
            onDoubleClick={views.reset}
            role="separator"
            aria-label="Resize the picture and the room"
            aria-valuenow={split.columns}
            aria-valuemin={2}
            aria-valuemax={10}
            title={`${split.columns} of ${COLUMNS} columns — drag to resize, double click for half and half`}
          />
        )}

        {split.room && (
          <div className="stage-room">
            <div className="room-bar">
              <span className="dim small">Room</span>
              <label className="lumen" title="How brightly the room is lit. Only the fill lighting moves: the lamps a cue drives stay where the score put them, so turning this down makes an effect the brightest thing in the picture rather than dimming it.">
                <span className="dim small">light</span>
                <input type="range" min={0} max={100} value={brightness}
                  onChange={(e) => setBrightness(Number(e.target.value))} />
              </label>
              <label className="lumen" title="How strong the soft ambient wash is — the two LED strips in the ceiling, carrying whatever colour the score is holding. It is a hint of the scene's colour rather than a light to see by, so it sits well below the room lighting.">
                <span className="dim small">wash</span>
                <input type="range" min={0} max={100} value={wash}
                  onChange={(e) => setWash(Number(e.target.value))} />
              </label>
              <label className="room-toggle" title="Show the film itself on the television in the room, instead of the ambient colour it is driving. The glow behind the panel keeps showing the ambient layer either way.">
                <input type="checkbox" checked={onScreen}
                  onChange={(e) => setOnScreen(e.target.checked)} />
                <span className="dim small">picture</span>
              </label>
              <label className="room-toggle" title="Throw the film into the room from the television, so it lands on the floor, the rug and the couch. A television does not really do this — it spills light rather than projecting an image — so it is off unless you want to look at it.">
                <input type="checkbox" checked={projecting}
                  onChange={(e) => setProjecting(e.target.checked)} />
                <span className="dim small">project</span>
              </label>
            </div>
            {/* Eight columns of room against four of controls, side by side.
                Stacked, the panels pushed the room up and everything scrolled
                against everything else; beside it, each has a column of its
                own and neither moves when the other grows. */}
            <div className={'room-split' + (split.force ? '' : ' alone')}>
            <div className="room-view">
            <Room
              score={score}
              rig={rig}
              time={time}
              muted={NO_MUTES}
              forced={forced}
              brightness={brightness}
              wash={wash}
              view={views.camera}
              onView={views.onCamera}
              revision={history.version}
              picture={onScreen ? picture : null}
              projection={projecting ? picture : null}
            />
            </div>
            {split.force && (
              <aside className="room-side">
                <Effects
                  instrument={target?.instrument ?? null}
                  kind={targetKind}
                  holds={target?.type === 'cue' ? 'cue' : 'curve'}
                  at={time}
                  fps={fps}
                  canInsert={!!target}
                  onInsert={insert}
                  onPreview={preview}
                />
                <Force rig={rig} forced={forced} onChange={setForced} />
              </aside>
            )}
            </div>
          </div>
        )}
      </div>

      <div
        className="split-h"
        onPointerDown={dragHeight}
        onDoubleClick={views.reset}
        role="separator"
        aria-label="Resize the height of the picture and the room"
        title="Drag to resize, double click to reset"
      />

      <section className="tl">
        {/* The lanes and the editor, side by side. The editor is a column
            rather than a panel that appears over the corner: it is drawn even
            with nothing selected, so the lanes keep one width and the place to
            edit is somewhere you look rather than somewhere you wait for. */}
        <div className="tl-split">
        <div className="tl-lanes">
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
        </div>
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

      <section className="panel">
        <h2>Library <span className="dim small">one film, one score; analysis runs in the background, one at a time</span></h2>
        <Library onOpen={openFilm} fps={fps} />
      </section>
    </div>
  );
}
