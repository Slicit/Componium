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
  setMuted(muted: Set<string>): void;
  setForced(forced: Map<string, number>): void;
  setBrightness(v: number): void;
  update(state: unknown): void;
  onView(fn: (view: CameraView) => void): void;
  getView(): CameraView;
  setView(view: CameraView | null): void;
  dispose(): void;
}

export function Room(props: {
  score: Score;
  rig: Rig | null;
  time: number;
  muted: Set<string>;
  forced: Map<string, number>;
  brightness: number;
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
}) {
  const { score, rig, time, muted, forced, brightness, view, onView } = props;
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

  useEffect(() => { room.current?.setMuted(muted); }, [muted, status]);
  useEffect(() => { room.current?.setForced(forced); }, [forced, status]);
  useEffect(() => { room.current?.setBrightness(brightness / 100); }, [brightness, status]);

  /* The room is told the time rather than reading a clock, the same way the
   * conductor is. One thing owns the playhead. */
  useEffect(() => {
    room.current?.update(evaluate(score, time, rig));
  }, [score, time, rig, status, muted, forced]);

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
