/* What the model said, so a person can decide whether to pay for it again.
 *
 * The description has been written beside every score since the vision seam
 * existed and there has never been a way to look at it without a shell on the
 * box. Which is most of why the choice between reusing it and running it again
 * was never offered: the thing the choice is about was invisible, and "is this
 * description good enough" is not a question anybody can answer from a count of
 * cues in a timeline.
 *
 * So this is a reading room, and the expensive action lives at the bottom of
 * it — where it can be taken after looking rather than instead of looking.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import {
  effects, matching, quietShare, scenes, tally, type Observation,
} from '../core/observations';
import { timecode, type Fps } from '../core/time';

interface Seen {
  film: string;
  observations: Observation[];
  note?: string;
  made?: string;
  /** The film's own length, so what is here can be read against it. */
  duration?: number;
}

/* Below this, a description is standing in for a film it has barely seen.
 *
 * Not a round number for its own sake: a description reaching four fifths of
 * the way through has plausibly just run out of frames worth reporting near
 * the credits, and one reaching a tenth has not. */
const COVERS_ENOUGH = 0.8;

export function Vision(props: {
  film: string;
  fps: Fps;
  onClose: () => void;
  /** Run the description again. Confirmed here; the caller does the work. */
  onLookAgain: () => void;
}) {
  const { film, fps, onClose, onLookAgain } = props;
  const [data, setData] = useState<Seen | null>(null);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState('');
  const close = useRef(onClose);
  close.current = onClose;

  useEffect(() => {
    let gone = false;
    fetch('/api/seen?film=' + encodeURIComponent(film))
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((d: Seen) => { if (!gone) setData(d); })
      .catch(() => { if (!gone) setFailed(true); });
    return () => { gone = true; };
  }, [film]);

  /* Escape closes, because a full screen panel with one small × is a trap on
   * a laptop trackpad. */
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close.current(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const all = data?.observations ?? [];
  const counts = useMemo(() => tally(all), [all]);
  const rows = useMemo(() => matching(all, query), [all, query]);
  const quiet = quietShare(all);
  const duration = data?.duration ?? 0;
  const reaches = all.length ? all[all.length - 1].t : 0;
  const covers = duration > 0 ? reaches / duration : 0;
  const partial = duration > 0 && covers < COVERS_ENOUGH;

  const lookAgain = () => {
    const what = all.length
      ? `Show ${film} to the model again?\n\n`
        + `This throws away the ${all.length} observations below and asks for `
        + 'new ones. It is the one expensive part of an analysis — on a feature '
        + 'it is the difference between minutes and most of an hour — and the '
        + 'old description cannot be got back.'
      : `Show ${film} to the model?`;
    if (!window.confirm(what)) return;
    onLookAgain();
    onClose();
  };

  return (
    <div className="modal-back" onPointerDown={(e) => {
      if (e.target === e.currentTarget) onClose();
    }}>
      <div className="modal" role="dialog" aria-modal="true" aria-label={'What the model saw in ' + film}>
        <header className="modal-head">
          <div>
            <h2>What the model saw</h2>
            <p className="dim small modal-sub">{film}</p>
          </div>
          <button className="insp-close" onClick={onClose} aria-label="Close">×</button>
        </header>

        {failed && <p className="dim small">Could not read the description.</p>}
        {!failed && !data && <p className="dim small">reading…</p>}

        {data && !all.length && (
          <p className="dim small">
            Nothing kept for this film yet. A description is written the first
            time it is analysed with a model configured.
          </p>
        )}

        {data && all.length > 0 && (
          <>
            <div className="vis-summary">
              <p className="dim small">
                {all.length} frames, {timecode(all[0].t, fps, { hours: true })}
                {' to '}{timecode(reaches, fps, { hours: true })}
                {duration > 0 && <> of {timecode(duration, fps, { hours: true })}</>}
                {duration > 0 && <> · covers {Math.round(covers * 100)}%</>}
                {' '}· {Math.round(quiet * 100)}% carried no effect
              </p>
              {data.made && (
                <p className="dim small">{data.made}{data.note && <> · {data.note}</>}</p>
              )}
              {partial && (
                /* The situation this was built to make visible. A rebuild
                 * reuses whatever description exists, however little of the
                 * film it covers, so a trial run of the first few minutes
                 * quietly becomes the description of the whole feature and
                 * stays that way. */
                <p className="vis-warn small">
                  This describes only the first{' '}
                  {timecode(reaches, fps, { hours: true })} of the film. A
                  rebuild reuses it as it stands, so the rest is never looked
                  at. Looking again covers all of it.
                </p>
              )}
              <div className="vis-tally">
                {effects(counts).map((t) => (
                  <span key={t.label} className="vis-chip">
                    {t.label}<b>{t.count}</b>
                  </span>
                ))}
                {scenes(counts).map((t) => (
                  <span key={t.label} className="vis-chip is-scene">
                    {t.label.replace('scene-', '')}<b>{t.count}</b>
                  </span>
                ))}
              </div>
            </div>

            <input
              className="vis-find"
              value={query}
              placeholder="find a label, or a word the model used"
              onChange={(e) => setQuery(e.target.value)}
            />

            <div className="vis-rows">
              {rows.map((o, i) => (
                <div className="vis-row" key={o.t + ':' + i}>
                  <span className="vis-t">{timecode(o.t, fps, { hours: true })}</span>
                  <span className="vis-labels">
                    {(o.labels ?? [])
                      .filter((l) => !l.startsWith('scene-'))
                      .map((l) => <span key={l} className="vis-chip">{l}</span>)}
                  </span>
                  <span className="vis-said">{o.seen}</span>
                </div>
              ))}
              {!rows.length && <p className="dim small">nothing matches that.</p>}
            </div>
          </>
        )}

        <footer className="modal-foot">
          <p className="dim small modal-note">
            A rebuild reuses this. Looking again is the only part of an analysis
            that costs a GPU, so it is asked for rather than assumed.
          </p>
          <button onClick={lookAgain}>Look again</button>
        </footer>
      </div>
    </div>
  );
}
