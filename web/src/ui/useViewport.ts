/* The live viewport, the saved ones, and the storage behind both. */

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  DEFAULT_VIEWPORT, drop, normalise, normaliseList, put,
  type CameraView, type NamedViewport, type Viewport,
} from '../core/viewport';
import { clampHeight, columnsAt, COLUMNS } from './useSplit';

const CURRENT = 'componium.viewport';
const SAVED = 'componium.viewports';

function read(key: string): unknown {
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : null;
  } catch {
    /* Private mode, or a half written value. Either way the arrangement is a
     * convenience and losing it must never stop the studio opening. */
    return null;
  }
}

function write(key: string, value: unknown) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* private mode */ }
}

export interface Viewports {
  viewport: Viewport;
  setColumns: (n: number) => void;
  setHeight: (px: number) => void;
  setRoom: (on: boolean) => void;
  setForce: (on: boolean) => void;
  /** Where the camera should go, changing identity only when it is moved for you. */
  camera: CameraView | null;
  /** Called by the room as the camera is dragged. Does not re-render. */
  onCamera: (v: CameraView) => void;
  saved: NamedViewport[];
  save: (name: string) => void;
  apply: (name: string) => void;
  remove: (name: string) => void;
  reset: () => void;
}

export function useViewport(): Viewports {
  const [viewport, setViewport] = useState<Viewport>(() => normalise(read(CURRENT)));
  const [saved, setSaved] = useState<NamedViewport[]>(() => normaliseList(read(SAVED)));

  /* The camera is deliberately not state.
   *
   * OrbitControls reports a new position on every frame of a drag, and routing
   * that through React would re-render the timeline sixty times a second while
   * somebody turns the room around. So the live position lives in a ref, and
   * only the position we want the room to *move to* is state. */
  const live = useRef<CameraView | null>(viewport.camera);
  const [camera, setCamera] = useState<CameraView | null>(() => viewport.camera);

  useEffect(() => { write(CURRENT, { ...viewport, camera: live.current }); }, [viewport]);
  useEffect(() => { write(SAVED, saved); }, [saved]);

  /* Saving the camera on a timer rather than on every frame. Writing
   * localStorage per frame of a drag is a serialise and a synchronous disk
   * write per frame, which is felt as the room turning in steps. */
  const pending = useRef<ReturnType<typeof setTimeout> | null>(null);
  const onCamera = useCallback((v: CameraView) => {
    live.current = v;
    if (pending.current) return;
    pending.current = setTimeout(() => {
      pending.current = null;
      const stored = normalise(read(CURRENT));
      write(CURRENT, { ...stored, camera: live.current });
    }, 400);
  }, []);

  useEffect(() => () => { if (pending.current) clearTimeout(pending.current); }, []);

  const setColumns = useCallback(
    (n: number) => setViewport((v) => ({ ...v, columns: columnsAt(n / COLUMNS) })), []);
  const setHeight = useCallback(
    (px: number) => setViewport((v) => ({ ...v, height: clampHeight(px) })), []);
  const setRoom = useCallback((on: boolean) => setViewport((v) => ({ ...v, room: on })), []);
  const setForce = useCallback((on: boolean) => setViewport((v) => ({ ...v, force: on })), []);

  const save = useCallback((name: string) => {
    setSaved((list) => put(list, name, { ...viewport, camera: live.current }));
  }, [viewport]);

  const apply = useCallback((name: string) => {
    setSaved((list) => {
      const found = list.find((v) => v.name === name);
      if (found) {
        setViewport(found.viewport);
        /* A fresh object even when the numbers match, because the room applies
         * a view on identity: recalling the same viewport twice should put the
         * camera back both times, not only the first. */
        if (found.viewport.camera) setCamera({ ...found.viewport.camera });
      }
      return list;
    });
  }, []);

  const remove = useCallback((name: string) => setSaved((list) => drop(list, name)), []);

  const reset = useCallback(() => {
    setViewport({ ...DEFAULT_VIEWPORT });
    /* Null tells the room to go back to where it starts, which is a decision
     * the room owns — this module does not know where that is. */
    setCamera(null);
    live.current = null;
  }, []);

  return {
    viewport, setColumns, setHeight, setRoom, setForce,
    camera, onCamera, saved, save, apply, remove, reset,
  };
}
