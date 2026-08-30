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
  amplitudeOf, cueEnd, hexOf, isHSI, isSpan, writeColour,
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

  /* Drawn empty rather than not drawn.
   *
   * This used to vanish when nothing was selected, which meant the lanes
   * changed width every time you clicked something and the editor was a place
   * you waited for rather than a place you looked. An empty pane costs a
   * column and says what it is for. */
  if (!selection) {
    return (
      <aside className="insp is-empty">
        <header><span className="insp-what">Editor</span></header>
        <p className="insp-note">
          Click an event or a curve point and its numbers appear here, where
          they can be typed exactly rather than dragged approximately.
        </p>
      </aside>
    );
  }

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
        <button className="insp-close" onClick={onClose}
                aria-label="Clear selection"
                title="Clear the selection. The editor stays.">×</button>
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

          {hexOf(cue.params) && (
            <Swatch
              label="Colour"
              hex={hexOf(cue.params)!}
              onPick={(picked) => {
                /* Written directly, like the parameters above and for the same
                 * reason: the commands cover time and length, and pretending
                 * otherwise would put a value on the undo stack that undo
                 * cannot reach. */
                writeColour(cue.params!, picked);
                history.run(moveCues([{ track, cue, from: cue.t, to: cue.t }]));
                history.seal();
                onChanged();
              }}
            />
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
          {hexOf(point.value) && (
            <Swatch
              label={isHSI(track) ? 'Colour' : 'Mix'}
              hex={hexOf(point.value)!}
              onPick={(picked) => {
                /* Every channel in one command, so picking a colour is one
                 * undo rather than three — and so a half-applied colour cannot
                 * exist between them. */
                const before = { ...point.value };
                writeColour(point.value, picked);
                const edits = Object.keys(point.value)
                  .filter((k) => point.value[k] !== before[k])
                  .map((k) => ({
                    track, point, channel: k,
                    fromT: point.t, toT: point.t,
                    fromV: before[k], toV: point.value[k],
                  }));
                if (!edits.length) return;
                /* Put back, because the command is what applies it. Editing in
                 * place first was only a way to work out what changed. */
                Object.assign(point.value, before);
                run(movePoints(edits));
              }}
            />
          )}
          <Row label="Level" value={String(round3(amplitudeOf(point.value) ?? 0))} />
          <button className="insp-go" onClick={() => onSeek(point.t)}>Move playhead here</button>
        </>
      )}
    </aside>
  );
}

/* The colour, as a colour.
 *
 * The channel fields stay: typing 0.5 into saturation is how you pin a value
 * down once you have found it, and this is how you find it. The same division
 * the note at the top of this file makes about dragging and typing time.
 */
function Swatch(props: { label: string; hex: string; onPick: (hex: string) => void }) {
  return (
    <div className="insp-row">
      <span className="insp-label">{props.label}</span>
      <span className="insp-value insp-colour">
        <input
          type="color"
          className="swatch swatch-pick"
          value={props.hex}
          aria-label={props.label}
          title="Pick a colour"
          onChange={(e) => props.onPick(e.target.value)}
        />
        <span className="insp-hex">{props.hex}</span>
      </span>
    </div>
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
