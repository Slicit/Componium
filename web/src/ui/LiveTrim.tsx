/* Two knobs for a strip that does not look like the numbers say it should.
 *
 * A generated ambient curve can hold its saturation around five percent for
 * most of a film. The hue swings the whole way round the circle, the timeline
 * draws it beautifully, and the strip is white. Nothing is wrong with the score
 * and nothing is wrong with the strip: two reels of LEDs with the same part
 * number on the bag reach the same numbers differently, and the difference has
 * to be adjustable somewhere.
 *
 * Here, rather than in the score, because it is a statement about this room. A
 * score edited to suit one strip plays wrong on every other rig.
 *
 * Sent as they move, so the answer is on the wall rather than in a form. The
 * server holds them, so disarming to move a board does not lose a setting that
 * took ten minutes to find.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

export interface Trim {
  brightness: number;
  saturation: number;
}

const NONE: Trim = { brightness: 0, saturation: 0 };

/** Slower than the slider moves, so dragging does not become a flood. */
const SEND_MS = 60;

export function LiveTrim() {
  const [trim, setTrim] = useState<Trim>(NONE);
  /* What the server has, so a drag is not fighting a reply that arrives late
   * and snaps the handle back to where it was two frames ago. */
  const pending = useRef<Trim | null>(null);
  const timer = useRef<number | null>(null);

  useEffect(() => {
    let live = true;
    void fetch('/api/live/trim')
      .then((r) => (r.ok ? r.json() : null))
      .then((got: Trim | null) => { if (live && got) setTrim(got); })
      .catch(() => { /* the sliders still work, they just start at zero */ });
    return () => { live = false; };
  }, []);

  const send = useCallback((next: Trim) => {
    pending.current = next;
    if (timer.current !== null) return;
    timer.current = window.setTimeout(() => {
      timer.current = null;
      const body = pending.current;
      pending.current = null;
      if (!body) return;
      void fetch('/api/live/trim', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }).catch(() => { /* the next drag sends it again */ });
    }, SEND_MS);
  }, []);

  const move = (what: keyof Trim) => (e: React.ChangeEvent<HTMLInputElement>) => {
    const next = { ...trim, [what]: Number(e.target.value) };
    setTrim(next);
    send(next);
  };

  const reset = () => { setTrim(NONE); send(NONE); };
  const touched = trim.brightness !== 0 || trim.saturation !== 0;

  return (
    <span className="trim" role="group" aria-label="Live colour trim">
      <label title={
        'Added to the intensity of every colour cue, and to nothing else. '
        + 'Added rather than scaled, because the values that need help are the '
        + 'small ones. The score is not changed.'}>
        <span className="trim-name">bright</span>
        <input
          type="range" min={-100} max={100} step={1}
          value={trim.brightness}
          onChange={move('brightness')}
          aria-label="Brightness trim, percent"
        />
        <output className={trim.brightness ? 'trim-value on' : 'trim-value'}>
          {trim.brightness > 0 ? '+' : ''}{trim.brightness}
        </output>
      </label>

      <label title={
        'Added to the saturation of every colour cue. A track sitting at five '
        + 'percent saturation is white on a strip however good the hue is; '
        + 'this is the knob that makes it a colour. The score is not changed.'}>
        <span className="trim-name">colour</span>
        <input
          type="range" min={-100} max={100} step={1}
          value={trim.saturation}
          onChange={move('saturation')}
          aria-label="Saturation trim, percent"
        />
        <output className={trim.saturation ? 'trim-value on' : 'trim-value'}>
          {trim.saturation > 0 ? '+' : ''}{trim.saturation}
        </output>
      </label>

      <button
        className="trim-reset"
        onClick={reset}
        disabled={!touched}
        title="Back to the score as written"
      >reset</button>
    </span>
  );
}
