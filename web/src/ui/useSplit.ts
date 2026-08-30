/* The split between the picture and the room.
 *
 * Twelve columns, because that is a grid a person can aim at: dragging lands
 * on a twelfth rather than on 47.3%, so two sessions on two machines end up
 * with the same layout and a half-and-half split is exactly half. Free
 * dragging feels more capable and is worse — nobody wants 5.7 columns, and
 * without snapping nobody can get back to six.
 */

import { useCallback, useEffect, useState } from 'react';

export const COLUMNS = 12;

/**
 * Neither pane may be squeezed to nothing.
 *
 * Two columns is already narrow enough that the room is a stamp and the video
 * is a strip; below that the pane stops being a preview and becomes a handle
 * you cannot find again. The limit is what makes the drag safe to let go of.
 */
export const MIN_COLUMNS = 2;
export const MAX_COLUMNS = COLUMNS - MIN_COLUMNS;

export const MIN_HEIGHT = 160;
export const MAX_HEIGHT = 900;

/** Half and half. */
export const DEFAULT_COLUMNS = COLUMNS / 2;
export const DEFAULT_HEIGHT = 300;

/** Which column boundary a drag at this fraction of the width lands on. */
export function columnsAt(fraction: number): number {
  if (!isFinite(fraction)) return DEFAULT_COLUMNS;
  const snapped = Math.round(fraction * COLUMNS);
  return Math.max(MIN_COLUMNS, Math.min(MAX_COLUMNS, snapped));
}

export function clampHeight(px: number): number {
  if (!isFinite(px)) return DEFAULT_HEIGHT;
  return Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, Math.round(px)));
}

function remembered(key: string, fallback: number, clamp: (v: number) => number): number {
  try {
    const raw = localStorage.getItem(key);
    /* Number(null) is 0, which is a plausible-looking value that then gets
     * clamped to the minimum — so a first visit would open with the panes
     * collapsed rather than at the default. Test for the key, not the value. */
    if (raw === null || raw === '') return fallback;
    const v = Number(raw);
    return Number.isFinite(v) ? clamp(v) : fallback;
  } catch {
    return fallback;
  }
}

export interface Split {
  columns: number;
  height: number;
  setColumns: (n: number) => void;
  setHeight: (px: number) => void;
  reset: () => void;
}

/** The remembered split, and the two setters that keep it remembered. */
export function useSplit(): Split {
  const [columns, setCols] = useState(() =>
    remembered('componium.split', DEFAULT_COLUMNS, (v) => columnsAt(v / COLUMNS)));
  const [height, setH] = useState(() =>
    remembered('componium.stageHeight', DEFAULT_HEIGHT, clampHeight));

  useEffect(() => {
    try { localStorage.setItem('componium.split', String(columns)); } catch { /* private mode */ }
  }, [columns]);
  useEffect(() => {
    try { localStorage.setItem('componium.stageHeight', String(height)); } catch { /* private mode */ }
  }, [height]);

  const setColumns = useCallback((n: number) => setCols(columnsAt(n / COLUMNS)), []);
  const setHeight = useCallback((px: number) => setH(clampHeight(px)), []);
  const reset = useCallback(() => {
    setCols(DEFAULT_COLUMNS);
    setH(DEFAULT_HEIGHT);
  }, []);

  return { columns, height, setColumns, setHeight, reset };
}

/**
 * Drag a handle, reporting where it is.
 *
 * Pointer capture is taken for the whole gesture: without it, moving faster
 * than the browser repaints drops the pointer outside the handle and the drag
 * simply stops halfway, which reads as the handle being sticky.
 */
export function useDrag(
  onMove: (e: PointerEvent) => void,
): (e: React.PointerEvent) => void {
  return useCallback((e: React.PointerEvent) => {
    if (e.button !== 0) return;
    /* Deliberately no preventDefault(). It suppresses the compatibility mouse
     * events the browser synthesises from pointer events, which means the
     * double click that resets the split never fires — the drag works
     * perfectly and the reset silently does not, so nothing looks broken.
     * Selection and scrolling are prevented in the stylesheet instead, which
     * is what those properties are for. */
    const el = e.currentTarget as HTMLElement;
    try { el.setPointerCapture(e.pointerId); } catch { /* not captured */ }

    const move = (ev: PointerEvent) => onMove(ev);
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      try { el.releasePointerCapture(e.pointerId); } catch { /* already gone */ }
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  }, [onMove]);
}
