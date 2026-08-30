/* The scores kept for one film.
 *
 * Every analysis used to overwrite the last, which makes tuning the composer a
 * guessing game: change a threshold, rerun, and the thing you were comparing
 * against is gone. So each run is kept, and this is how the editor reaches
 * them.
 */

import { useCallback, useEffect, useState } from 'react';
import type { Step } from './Steps';

export interface Version {
  id: string;
  label: string;
  note?: string;
  from: number;
  to: number;
  duration: number;
  complete: boolean;
  cues: number;
  points: number;
  /* What the run that made this score did. Kept with the version because the
   * job is overwritten by the next run, and "why was that one slower" is a
   * question asked afterwards. */
  steps?: Step[];
  seconds?: number;
}

export interface Versions {
  list: Version[];
  /** Which one is open, or '' for the live score. */
  current: string;
  select: (id: string) => void;
  refresh: () => Promise<void>;
}

/**
 * The versions of whichever film is open.
 *
 * Reloaded when the film changes, because the list belongs to the film and a
 * stale list offers to open another film's scores.
 */
export function useVersions(film: string, onOpen: (id: string) => void): Versions {
  const [list, setList] = useState<Version[]>([]);
  const [current, setCurrent] = useState('');

  const refresh = useCallback(async () => {
    if (!film) {
      setList([]);
      return;
    }
    try {
      const res = await fetch('/api/versions?film=' + encodeURIComponent(film));
      if (!res.ok) {
        setList([]);
        return;
      }
      const data = await res.json();
      setList(Array.isArray(data.versions) ? data.versions : []);
    } catch {
      /* A history is for comparing runs, not for running. Losing it must
       * never stop the film opening. */
      setList([]);
    }
  }, [film]);

  useEffect(() => {
    /* Back to the live score whenever the film changes: a version id belongs
     * to one film, and carrying a selection across would ask for a score that
     * does not exist. */
    setCurrent('');
    void refresh();
  }, [film, refresh]);

  const select = useCallback((id: string) => {
    setCurrent(id);
    onOpen(id);
  }, [onOpen]);

  return { list, current, select, refresh };
}
