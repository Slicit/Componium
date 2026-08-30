/* What is selected, and its numbers.
 *
 * Dragging is how you shape a curve and it is a poor way to say "exactly one
 * second, exactly a half". Both belong in an editor: the gesture for finding
 * the shape, the field for pinning it down once found. So a plain click opens
 * this, and every value in it is typed rather than dragged.
 *
 * Editing here goes through the same commands as a drag, which is what makes
 * one undo step out of a typed value and lets the two coexist without either
 * knowing about the other.
 */

import { useEffect, useState } from 'react';
import type { History } from '../core/history';
import { movePoints, moveCues, resizeCues } from '../core/history';
import { durationLabel, parseTime, timecode, clamp01, round3, type Fps } from '../core/time';
import {
  amplitudeOf, colourOf, cueEnd, isHSI, isSpan,
  type Cue, type Point, type Score, type Track,
} from '../core/score';

export interface Selection {
  track: Track;
  cue?: Cue;
  point?: Point;
  channel?: string;
}

export function Inspector(props: {
  score: Score;
  selection: Selection | null;
  history: History;
  fps: Fps;
  onChanged: () => void;
  onSeek: (t: number) => void;
  onClose: () => void;
}) {
  const { score, selection, history, fps, onChanged, onSeek, onClose } = props;
  if (!selection) return null;
  const { track, cue, point, channel } = selection;

  const run = (cmd: ReturnType<typeof movePoints> | null) => {
    if (!cmd) return;
    history.run(cmd);
    history.seal();
    onChanged();
  };

  return (
    <aside className="insp">
      <header>
        <span className="insp-what">{track.instrument}</span>
        <button className="insp-close" onClick={onClose} aria-label="Close">×</button>
      </header>

      {cue && (
        <>
          <Row label="Action" value={cue.action} />
          <Field
            label="Time"
            value={timecode(cue.t, fps, { hours: true })}
            hint="hh:mm:ss:ff, or 90, or 1:30"
            onCommit={(text) => {
              const t = parseTime(text, fps);
              if (t === null) return false;
              run(moveCues([{ track, cue, from: cue.t, to: Math.max(0, Math.min(score.duration, t)) }]));
              return true;
            }}
          />
          {isSpan(cue) ? (
            <Field
              label="Length"
              value={String(round3(cue.duration ?? 0))}
              hint={durationLabel(cue.duration ?? 0, fps) + ', in seconds'}
              onCommit={(text) => {
                const v = Number(text);
                if (!Number.isFinite(v) || v <= 0) return false;
                run(resizeCues([{ track, cue, from: cue.duration ?? 0, to: v }]));
                return true;
              }}
            />
          ) : (
            <Row label="Length" value="an instant" />
          )}
          <Row label="Ends" value={isSpan(cue) ? timecode(cueEnd(cue), fps, { hours: true }) : '—'} />

          {Object.keys(cue.params ?? {}).length > 0 && <div className="insp-sep" />}
          {Object.entries(cue.params ?? {}).map(([key, v]) => (
            <Field
              key={key}
              label={key}
              value={String(round3(v))}
              hint="0 to 1"
              onCommit={(text) => {
                const n = Number(text);
                if (!Number.isFinite(n)) return false;
                /* Parameters are not on the undo path yet — the commands cover
                 * time and length. Written directly, and said so, rather than
                 * pretending otherwise. */
                cue.params![key] = clamp01(n);
                history.run(moveCues([{ track, cue, from: cue.t, to: cue.t }]));
                history.seal();
                onChanged();
                return true;
              }}
            />
          ))}

          {colourOf(cue.params) && (
            <Row label="Colour" value={<span className="swatch" style={{ background: colourOf(cue.params)! }} />} />
          )}
          {cue.source && (
            <p className="insp-note">
              Nominated by {cue.source}. The composer guessed at this rather than
              measuring it, so it is worth confirming before trusting it to a machine.
            </p>
          )}
          <button className="insp-go" onClick={() => onSeek(cue.t)}>Move playhead here</button>
        </>
      )}

      {point && channel && (
        <>
          <Row label="Channel" value={channel} />
          <Field
            label="Time"
            value={timecode(point.t, fps, { hours: true })}
            hint="hh:mm:ss:ff, or 90, or 1:30"
            onCommit={(text) => {
              const t = parseTime(text, fps);
              if (t === null) return false;
              run(movePoints([{
                track, point,
                fromT: point.t, toT: Math.max(0, Math.min(score.duration, t)),
              }]));
              return true;
            }}
          />
          {Object.entries(point.value ?? {}).map(([key, v]) => (
            <Field
              key={key}
              label={key}
              value={String(round3(v))}
              hint={key === 'h' ? '0 to 1, a turn round the wheel' : '0 to 1'}
              highlight={key === channel}
              onCommit={(text) => {
                const n = Number(text);
                if (!Number.isFinite(n)) return false;
                run(movePoints([{
                  track, point, channel: key,
                  fromT: point.t, toT: point.t,
                  fromV: v, toV: key === 'h' ? ((n % 1) + 1) % 1 : clamp01(n),
                }]));
                return true;
              }}
            />
          ))}
          {colourOf(point.value) && (
            <Row label={isHSI(track) ? 'Colour' : 'Mix'} value={
              <span className="swatch" style={{ background: colourOf(point.value)! }} />
            } />
          )}
          <Row label="Level" value={String(round3(amplitudeOf(point.value) ?? 0))} />
          <button className="insp-go" onClick={() => onSeek(point.t)}>Move playhead here</button>
        </>
      )}
    </aside>
  );
}

function Row(props: { label: string; value: React.ReactNode }) {
  return (
    <div className="insp-row">
      <span className="insp-label">{props.label}</span>
      <span className="insp-value">{props.value}</span>
    </div>
  );
}

/**
 * A field that commits on Enter or on leaving, and refuses rather than
 * guessing.
 *
 * Refusing matters: a half-typed time is not a time, and quietly interpreting
 * it moves an event somewhere arbitrary. On a refusal the field flashes and
 * puts the real value back, so nothing is lost and nothing is invented.
 */
function Field(props: {
  label: string;
  value: string;
  hint?: string;
  highlight?: boolean;
  onCommit: (text: string) => boolean;
}) {
  const [text, setText] = useState(props.value);
  const [bad, setBad] = useState(false);
  /* Follow the document while not being typed in: a drag elsewhere, or an
   * undo, has to show here too. */
  useEffect(() => { setText(props.value); }, [props.value]);

  const commit = () => {
    if (text === props.value) return;
    if (props.onCommit(text)) {
      setBad(false);
    } else {
      setBad(true);
      setText(props.value);
      setTimeout(() => setBad(false), 600);
    }
  };

  return (
    <label className={'insp-row' + (props.highlight ? ' is-channel' : '')}>
      <span className="insp-label">{props.label}</span>
      <input
        className={'insp-input' + (bad ? ' bad' : '')}
        value={text}
        title={props.hint}
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') { e.preventDefault(); commit(); (e.target as HTMLInputElement).blur(); }
          if (e.key === 'Escape') { setText(props.value); (e.target as HTMLInputElement).blur(); }
          /* The timeline's shortcuts must not fire while a number is being
           * typed — s is "split", and it is also a letter. */
          e.stopPropagation();
        }}
      />
    </label>
  );
}
