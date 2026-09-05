/* Arming the room, and telling it where the playhead is.
 *
 * The studio's playhead lives here and the rig lives in the server, so while
 * this is armed the page reports its position and the server drives the room
 * from it. Everything about timing is already on the other side: the clock
 * discipline, the latency compensation, the curve driver and the safety
 * supervisor are the same ones a show uses. This is a switch and a wire.
 *
 * Two things it does that are about safety rather than features.
 *
 * It reports while paused as well as while playing, a few times a second.
 * Silence has to mean "this page is gone" and nothing else, or a paused editor
 * would look identical to a closed tab and the server could not tell them
 * apart.
 *
 * It disarms on the way out. The server puts the rig away by itself after a
 * few seconds of silence, which is the guarantee; this makes closing the tab
 * immediate rather than eventual, and a fan that stops when you close the page
 * is worth the ten lines.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

export interface LiveState {
  armed: boolean;
  problem?: string;
  rig?: string;
  real: number;
  instruments?: string[];
  /* The instruments a colour trim can do anything to, so the sliders are
     offered for those and for nothing else. */
  lights?: string[];
  silent: boolean;
  media: number;
  precision: number;
  cues: number;
  curves: number;
  events?: string[];
}

const IDLE: LiveState = {
  armed: false, real: 0, silent: true, media: 0, precision: 0, cues: 0, curves: 0,
};

/** How often to speak while paused. Silence must mean gone, not still. */
const PAUSED_MS = 250;

/** How often to ask the server what it thinks, for the readout. */
const POLL_MS = 1000;

export function useLive() {
  const [state, setState] = useState<LiveState>(IDLE);
  const armed = state.armed;
  /* Read by the reporting path, which must not be rebuilt on every frame. */
  const armedRef = useRef(false);
  armedRef.current = armed;
  /* Where the playhead was, for the paused heartbeat below. */
  const atRef = useRef(0);
  const playingRef = useRef(false);

  const set = useCallback(async (want: boolean) => {
    const res = await fetch('/api/live', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ armed: want }),
    });
    const said = await res.json().catch(() => null);
    setState(said ?? IDLE);
  }, []);

  const arm = useCallback(() => set(true), [set]);
  const disarm = useCallback(() => set(false), [set]);

  /** Tell the server where the playhead is. Cheap, and called per frame. */
  const report = useCallback((at: number, playing: boolean) => {
    if (!armedRef.current) return;
    void fetch('/api/live/at', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ at, playing }),
    }).then((res) => {
      /* The rig was put away while we were not looking, which happens after a
       * stall long enough for the server to give up. Stop reporting at it. */
      if (res.status === 409) setState((was) => ({ ...was, armed: false }));
    }).catch(() => { /* a dropped report is the next one's problem */ });
  }, []);

  /* What the server thinks, for the readout: cues sent, curve updates, how
   * precisely it knows where the film is, and anything safety had to say. */
  useEffect(() => {
    if (!armed) return;
    const id = setInterval(() => {
      void fetch('/api/live')
        .then((r) => (r.ok ? r.json() : null))
        .then((s) => { if (s) setState(s); })
        .catch(() => { /* the readout can miss one */ });
    }, POLL_MS);
    return () => clearInterval(id);
  }, [armed]);

  /* Somebody armed this and then went to lunch with the film paused. The page
   * has to keep speaking or the server will rightly assume it has gone. */
  useEffect(() => {
    if (!armed) return;
    const id = setInterval(() => {
      if (!playingRef.current) report(atRef.current, false);
    }, PAUSED_MS);
    return () => clearInterval(id);
  }, [armed, report]);

  const follow = useCallback((at: number, playing: boolean) => {
    atRef.current = at;
    playingRef.current = playing;
    report(at, playing);
  }, [report]);

  /* Closing the tab puts the rig away now rather than in five seconds.
   * sendBeacon because an ordinary fetch is abandoned when the page unloads,
   * which is exactly when this one matters. */
  useEffect(() => {
    if (!armed) return;
    const go = () => {
      try {
        navigator.sendBeacon('/api/live',
          new Blob([JSON.stringify({ armed: false })], { type: 'application/json' }));
      } catch { /* the server's own timeout is the guarantee */ }
    };
    window.addEventListener('pagehide', go);
    return () => window.removeEventListener('pagehide', go);
  }, [armed]);

  /** Put a refusal away once it has been read. */
  const forget = useCallback(() => {
    setState((was) => ({ ...was, problem: undefined }));
  }, []);

  return { state, armed, arm, disarm, follow, forget };
}
