/* The effects library, and a way to try one before you commit to it.
 *
 * Picking a preset drives the room with its envelope on a loop, so the shape
 * is something you watch rather than something you read. That preview reuses
 * the force mechanism the sliders already use — the room has one way of being
 * told "this device is at this level regardless of the score", and a second
 * one would be a second thing to keep in agreement with it.
 */

import { useEffect, useRef, useState } from 'react';
import { presetsFor, valueOf, type Preset } from '../core/presets';
import { timecode } from '../core/time';

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
}) {
  const { instrument, kind, at, fps, canInsert, onInsert, onPreview } = props;
  const [chosen, setChosen] = useState<Preset | null>(null);
  const presets = presetsFor(kind);
  const frame = useRef(0);

  /* Run the envelope on a loop while something is chosen, and hand the device
   * back the moment nothing is. The cleanup releasing the force is the part
   * that matters: without it, closing the panel mid-preview leaves a device
   * stuck at whatever level the last frame happened to be. */
  useEffect(() => {
    if (!chosen || !instrument) return;
    const began = performance.now();
    const run = () => {
      const elapsed = (performance.now() - began) / 1000;
      /* A pause at the end of each pass, so a short shape does not become a
       * strobe and a long one has a moment of rest to be read against. */
      const cycle = chosen.seconds + 0.6;
      const f = (elapsed % cycle) / chosen.seconds;
      onPreview(instrument, f > 1 ? 0 : Math.abs(valueOf(chosen.shape, f)));
      frame.current = requestAnimationFrame(run);
    };
    frame.current = requestAnimationFrame(run);
    return () => {
      cancelAnimationFrame(frame.current);
      onPreview(instrument, null);
    };
  }, [chosen, instrument, onPreview]);

  /* A preset chosen for one instrument means nothing on the next. */
  useEffect(() => { setChosen(null); }, [instrument]);

  if (!instrument) {
    return (
      <div className="fx">
        <p className="dim small fx-note">
          Pick a track to see the effects that suit it.
        </p>
      </div>
    );
  }

  return (
    <div className="fx">
      <div className="fx-head">
        <span className="dim small">
          Effects for <strong>{instrument}</strong>
        </span>
        <span className="dim small fx-at">at {timecode(at, fps, { hours: true })}</span>
      </div>

      {presets.length === 0 && (
        <p className="dim small fx-note">Nothing in the library suits a {kind || 'device'} yet.</p>
      )}

      <div className="fx-list">
        {presets.map((p) => (
          <div
            key={p.id}
            className={'fx-row' + (chosen?.id === p.id ? ' on' : '')}
          >
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
          <span className="dim small fx-hint">{chosen.hint}</span>
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
