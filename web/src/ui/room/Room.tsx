/* The room, as a component.
 *
 * Room3D is an imperative class that owns a WebGL context and a scene graph,
 * and it stays that way: React is a poor fit for something redrawn every frame
 * from mutable state, and rewriting it as components would trade a working
 * renderer for a fashionable one. This wrapper does the three things React is
 * actually good for here — mount it, feed it, and take it down again.
 *
 * three.js is loaded on demand. It is around 600KB, the timeline is what most
 * of this application is, and someone who never opens the room should not pay
 * for it on first load.
 */

import { useEffect, useRef, useState } from 'react';
import type { Rig, Score } from '../../core/score';
import type { CameraView } from '../../core/viewport';
import { evaluate } from '../../core/state';

interface RoomHandle {
  setInstruments(instruments: unknown[]): void;
  setPicture(video: HTMLVideoElement | null): void;
  setProjection(video: HTMLVideoElement | null): void;
  setMuted(muted: Set<string>): void;
  setForced(forced: Map<string, number>): void;
  setBrightness(v: number): void;
  setWash(v: number): void;
  update(state: unknown): void;
  onView(fn: (view: CameraView) => void): void;
  getView(): CameraView;
  setView(view: CameraView | null): void;
  dispose(): void;
}

/* Matches the room own default, so the prop and its absence agree. */
const WASH_DEFAULT = 75;

export function Room(props: {
  score: Score;
  rig: Rig | null;
  time: number;
  muted: Set<string>;
  forced: Map<string, number>;
  brightness: number;
  /**
   * How strong the soft wash is, 0 to 100.
   *
   * Its own control rather than part of `brightness`. That one is the room's
   * own lighting and exists to be able to reach nothing; this is the strength
   * of something the score is driving, and they are adjusted at different
   * times for different reasons.
   */
  wash?: number;
  /**
   * Where to put the camera.
   *
   * Applied when the object identity changes, never on every render, because
   * the camera is something the person watching is holding: re-applying it
   * continuously would snap the view back out from under a drag.
   */
  view?: CameraView | null;
  /** Called as the camera moves, so the arrangement can remember it. */
  onView?: (view: CameraView) => void;
  /**
   * The film to show on the television in the room, or null for none.
   *
   * The very element the picture pane is playing, not a copy: one film, one
   * decode, one clock. Two would drift the moment either was scrubbed, and
   * the drift would be worst exactly where the room earns its keep — placing
   * a cue against a frame.
   */
  picture?: HTMLVideoElement | null;
  /**
   * The film to throw into the room from the television, or null for none.
   *
   * Independent of `picture`: either can be on without the other, and both
   * end up asking the room for the same frames. A television does not
   * actually project, so this is off by default and stays something switched
   * on deliberately.
   */
  projection?: HTMLVideoElement | null;
  /**
   * Bumped whenever the score is edited.
   *
   * Not decoration. Commands mutate the score in place — they hold references
   * to the tracks and cues they act on, because every edit re-sorts the track —
   * so the score object handed down here is the same object before and after an
   * edit, and an effect watching it never runs again. Adding a cue at the
   * playhead therefore changed nothing in the room until you scrubbed away and
   * back, which reads as the room being out of sync with the timeline.
   */
  revision?: number;
}) {
  const { score, rig, time, muted, forced, brightness, wash, view, onView, revision, picture,
    projection } =
    props;
  const host = useRef<HTMLDivElement>(null);
  const room = useRef<RoomHandle | null>(null);
  const [status, setStatus] = useState<'loading' | 'ready' | 'unavailable'>('loading');
  /* Held in a ref so the subscription below survives a caller that passes a
   * new arrow function on every render, which is the normal case. */
  const report = useRef(onView);
  report.current = onView;

  /* Build once, and take the context back on the way out. Browsers allow a
   * small number of live WebGL contexts per page, and a component that
   * mounted and unmounted a dozen times without disposing would lose the
   * oldest one and blank its own canvas. */
  useEffect(() => {
    let gone = false;
    let made: RoomHandle | null = null;

    (async () => {
      const mod = await import('./Room3D.js');
      if (gone || !host.current) return;
      if (!mod.webglAvailable()) { setStatus('unavailable'); return; }
      made = new mod.Room3D(host.current) as RoomHandle;
      room.current = made;
      setStatus('ready');
    })();

    return () => {
      gone = true;
      made?.dispose();
      room.current = null;
    };
  }, []);

  useEffect(() => {
    room.current?.setInstruments((rig?.instruments ?? []) as unknown[]);
  }, [rig, status]);

  useEffect(() => {
    room.current?.onView((v) => report.current?.(v));
  }, [status]);

  /* The camera is placed once per distinct view handed down, and null is a
   * real instruction — it is what reset means — so this does not skip it. */
  useEffect(() => {
    if (status !== 'ready') return;
    room.current?.setView(view ?? null);
  }, [view, status]);

  /* Keyed on the element itself, so it is applied whenever React mounts the
   * video — which happens after this component exists, once a film is picked.
   * Reaching into a ref from an effect keyed on anything else is how the
   * playhead once stopped following the picture. */
  useEffect(() => { room.current?.setPicture(picture ?? null); }, [picture, status]);

  useEffect(() => {
    room.current?.setProjection(projection ?? null);
  }, [projection, status]);

  useEffect(() => { room.current?.setMuted(muted); }, [muted, status]);
  useEffect(() => { room.current?.setForced(forced); }, [forced, status]);
  useEffect(() => { room.current?.setBrightness(brightness / 100); }, [brightness, status]);
  useEffect(() => { room.current?.setWash((wash ?? WASH_DEFAULT) / 100); }, [wash, status]);

  /* The room is told the time rather than reading a clock, the same way the
   * conductor is. One thing owns the playhead. */
  useEffect(() => {
    room.current?.update(evaluate(score, time, rig));
  }, [score, revision, time, rig, status, muted, forced]);

  return (
    <div className="room">
      <div className="room-host" ref={host} />
      {status === 'loading' && <p className="dim small room-note">building the room…</p>}
      {status === 'unavailable' && (
        <p className="dim small room-note">
          No WebGL here, so the room cannot be drawn. The timeline is unaffected.
        </p>
      )}
    </div>
  );
}
