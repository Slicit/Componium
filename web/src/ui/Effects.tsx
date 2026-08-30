/* The effects library, and a way to try one before you commit to it.
 *
 * Preview is an action, not a consequence of looking. Choosing a preset used
 * to start it playing on an endless loop, which meant the room was showing the
 * preview instead of the score for as long as anything was selected — so the
 * moment you wanted to check the effect against what the timeline already had,
 * the preview was in the way of exactly that. It also meant the only way to
 * stop looking at it was to stop selecting it, and you have to keep selecting
 * it in order to insert it.
 *
 * So: picking selects, the play button previews, and a preview runs a fixed
 * number of times and stops on its own. Nothing keeps hold of a device.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { presetsFor, valueOf, type Preset } from '../core/presets';
import { timecode } from '../core/time';
import { Icon } from './Icon';

/**
 * How many times a preview plays before it gives the device back.
 *
 * Three: one to see what it does, and two to watch the parts you missed. A
 * preview that ran forever would be a preview you have to remember to turn
 * off, and forgetting leaves a device driven by something that is not in the
 * score at all.
 */
export const PREVIEW_LOOPS = 3;

/** A breath between passes, so a short shape does not read as a strobe. */
const GAP_SECONDS = 0.6;

/** A thumbnail of the envelope, drawn from the same nodes that build it. */
function Shape(props: { preset: Preset; width?: number; height?: number }) {
  const w = props.width ?? 54;
  const h = props.height ?? 18;
  const pad = 1.5;
  /* Motion presets go negative, so the baseline sits in the middle for them
   * and along the bottom for everything else — a drop drawn as if it were a
   * rise is a picture of the wrong effect. */
  const low = Math.min(0, ...props.preset.shape.map(([, v]) => v));
  const top = Math.max(1, ...props.preset.shape.map(([, v]) => v));
  const span = top - low || 1;
  const y = (v: number) => pad + (1 - (v - low) / span) * (h - pad * 2);

  const d = props.preset.shape
    .map(([f, v], i) => `${i ? 'L' : 'M'}${(pad + f * (w - pad * 2)).toFixed(2)},${y(v).toFixed(2)}`)
    .join(' ');

  return (
    <svg className="fx-shape" width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-hidden="true">
      <line x1={pad} y1={y(0)} x2={w - pad} y2={y(0)} className="fx-base" />
      <path d={d} />
    </svg>
  );
}

export function Effects(props: {
  /** The instrument the presets are for, and its kind. */
  instrument: string | null;
  kind: string;
  /** Where an insert would land. */
  at: number;
  fps: number;
  canInsert: boolean;
  onInsert: (preset: Preset) => void;
  /** Drive the room at this level, or release it with null. */
  onPreview: (instrument: string, level: number | null) => void;
  /** Overridable so a test need not wait three seconds to watch one finish. */
  loops?: number;
}) {
  const { instrument, kind, at, fps, canInsert, onInsert, onPreview } = props;
  const loops = props.loops ?? PREVIEW_LOOPS;
  const [chosen, setChosen] = useState<Preset | null>(null);
  const [playing, setPlaying] = useState(false);
  const [pass, setPass] = useState(0);
  const presets = presetsFor(kind);
  const frame = useRef(0);

  const stop = useCallback(() => setPlaying(false), []);

  /* Run the envelope, count the passes, and stop. The cleanup releasing the
   * device is the part that matters: without it, unmounting mid-preview leaves
   * a device driven by a level that is in no score anywhere. */
  useEffect(() => {
    if (!playing || !chosen || !instrument) return;
    const cycle = chosen.seconds + GAP_SECONDS;
    const began = performance.now();
    let done = false;

    const run = () => {
      const elapsed = (performance.now() - began) / 1000;
      if (elapsed >= cycle * loops) {
        done = true;
        setPlaying(false);
        return;
      }
      setPass(Math.min(loops, Math.floor(elapsed / cycle) + 1));
      const f = (elapsed % cycle) / chosen.seconds;
      onPreview(instrument, f > 1 ? 0 : Math.abs(valueOf(chosen.shape, f)));
      frame.current = requestAnimationFrame(run);
    };
    frame.current = requestAnimationFrame(run);

    return () => {
      cancelAnimationFrame(frame.current);
      onPreview(instrument, null);
      if (!done) setPass(0);
    };
  }, [playing, chosen, instrument, loops, onPreview]);

  useEffect(() => { if (!playing) setPass(0); }, [playing]);

  /* A different preset, or a different track, means the preview that was
   * running was of something else. */
  useEffect(() => { setPlaying(false); }, [chosen, instrument]);
  useEffect(() => { setChosen(null); }, [instrument]);

  if (!instrument) {
    return (
      <div className="fx">
        <p className="dim small fx-note">Pick a track to see the effects that suit it.</p>
      </div>
    );
  }

  return (
    <div className="fx">
      <div className="fx-head">
        <span className="dim small">Effects for <strong>{instrument}</strong></span>
        <span className="dim small fx-at">at {timecode(at, fps, { hours: true })}</span>
      </div>

      {presets.length === 0 && (
        <p className="dim small fx-note">Nothing in the library suits a {kind || 'device'} yet.</p>
      )}

      <div className="fx-list">
        {presets.map((p) => (
          <div key={p.id} className={'fx-row' + (chosen?.id === p.id ? ' on' : '')}>
            <button
              className="fx-pick"
              onClick={() => setChosen(chosen?.id === p.id ? null : p)}
              title={p.hint + '  ·  ' + p.seconds + 's'}
              aria-pressed={chosen?.id === p.id}
            >
              <Shape preset={p} />
              <span className="fx-name">{p.name}</span>
              <span className="dim small fx-secs">{p.seconds}s</span>
            </button>
          </div>
        ))}
      </div>

      {chosen && (
        <div className="fx-foot">
          <button
            className="icon-btn fx-play"
            onClick={() => (playing ? stop() : setPlaying(true))}
            aria-label={playing ? 'Stop the preview' : 'Preview in the room'}
            title={playing
              ? 'Stop the preview'
              : `Play ${chosen.name} in the room ${loops} times`}
          ><Icon name={playing ? 'stop' : 'play'} /></button>

          <span className="dim small fx-hint">
            {playing ? `Previewing — pass ${pass} of ${loops}` : chosen.hint}
          </span>

          <button
            className="fx-insert"
            disabled={!canInsert}
            onClick={() => onInsert(chosen)}
            title={canInsert
              ? `Insert ${chosen.name} at the playhead`
              : 'Pick a track that can take this effect'}
          >Insert at playhead</button>
        </div>
      )}
    </div>
  );
}
