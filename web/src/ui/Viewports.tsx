/* Saved arrangements of the stage.
 *
 * A panel rather than a row of buttons in the bar: saving, recalling and
 * deleting are three verbs over a list, and a list needs somewhere to be. The
 * bar keeps one button, which is all the room it has.
 */

import { useEffect, useRef, useState } from 'react';
import type { NamedViewport, Viewport } from '../core/viewport';
import { MAX_VIEWPORTS, cleanName, sameLayout } from '../core/viewport';

export function Viewports(props: {
  viewport: Viewport;
  saved: NamedViewport[];
  onSave: (name: string) => void;
  onApply: (name: string) => void;
  onRemove: (name: string) => void;
  onReset: () => void;
}) {
  const { viewport, saved, onSave, onApply, onRemove, onReset } = props;
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const box = useRef<HTMLDivElement>(null);

  /* Escape closes it, and so does a click anywhere else. A panel that can only
   * be dismissed by the button that opened it is a panel people leave open. */
  useEffect(() => {
    if (!open) return;
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false); };
    const down = (e: PointerEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener('keydown', key);
    /* Capture, so a click on something that stops propagation still closes. */
    window.addEventListener('pointerdown', down, true);
    return () => {
      window.removeEventListener('keydown', key);
      window.removeEventListener('pointerdown', down, true);
    };
  }, [open]);

  const clean = cleanName(name);
  const existing = saved.find((v) => v.name === clean);
  const full = saved.length >= MAX_VIEWPORTS && !existing;

  const commit = () => {
    if (!clean || full) return;
    onSave(clean);
    setName('');
  };

  return (
    <div className="views" ref={box}>
      <button
        className={'toggle' + (open ? ' on' : '')}
        onClick={() => setOpen((v) => !v)}
        title="Saved arrangements of the picture, the room and the sliders"
        aria-expanded={open}
      >views{saved.length > 0 && <span className="count">{saved.length}</span>}</button>

      {open && (
        <div className="views-panel" role="dialog" aria-label="Saved viewports">
          {saved.length === 0 && (
            <p className="dim small views-empty">
              No saved arrangements yet. Set the stage how you want it, name it, and it
              will be here next time.
            </p>
          )}

          {saved.map((v) => (
            <div
              key={v.name}
              className={'views-row' + (sameLayout(v.viewport, viewport) ? ' match' : '')}
            >
              <button
                className="views-apply"
                onClick={() => { onApply(v.name); setOpen(false); }}
                title={describe(v.viewport)}
              >{v.name}</button>
              <span className="dim small views-note">{describe(v.viewport)}</span>
              <button
                className="danger views-del"
                onClick={() => onRemove(v.name)}
                title={'Delete ' + v.name}
                aria-label={'Delete ' + v.name}
              >×</button>
            </div>
          ))}

          <div className="views-save">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') commit(); }}
              placeholder="name this arrangement"
              aria-label="Name this arrangement"
              maxLength={40}
            />
            <button onClick={commit} disabled={!clean || full}>
              {existing ? 'Replace' : 'Save'}
            </button>
          </div>
          {full && (
            <p className="dim small views-empty">
              That is {MAX_VIEWPORTS} saved arrangements, which is the limit. Delete one
              to make room, or reuse a name to replace it.
            </p>
          )}

          <div className="views-foot">
            <button onClick={() => { onReset(); setOpen(false); }}>
              Reset to default
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/** A viewport in a few words, for the row under its name. */
function describe(v: Viewport): string {
  const parts = [v.room ? `${v.columns}/${12 - v.columns} split` : 'picture only'];
  if (v.room && !v.force) parts.push('no sliders');
  if (v.camera) parts.push('camera');
  parts.push(v.height + 'px');
  return parts.join(' · ');
}
