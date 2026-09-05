/* Two knobs per light, for strips that do not look like the numbers say.
 *
 * A generated ambient curve can hold its saturation around five percent for
 * most of a film. The hue swings the whole way round the circle, the timeline
 * draws it beautifully, and the strip is white. Nothing is wrong with the score
 * and nothing is wrong with the strip: two reels of LEDs with the same part
 * number on the bag reach the same numbers differently.
 *
 * Per light rather than per room, because that is where the difference is. An
 * ambient wash behind a screen and an event strip in a cornice are different
 * parts, bought at different times, and the number that makes one of them right
 * makes the other one wrong.
 *
 * Not in the score, because a score edited to suit one strip plays wrong on
 * every other rig. This is a statement about a room.
 *
 * In a panel rather than in the toolbar: two sliders per light is more than a
 * row of buttons can hold, and this is read while looking at the room rather
 * than at the screen.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

export interface Trim {
  brightness: number;
  saturation: number;
}

/** Per instrument id. An instrument nobody has touched is simply absent. */
export type Trims = Record<string, Trim>;

const NONE: Trim = { brightness: 0, saturation: 0 };

/** Slower than a slider moves, so dragging does not become a flood. */
const SEND_MS = 60;

export function LiveTrim({ lights }: { lights: string[] }) {
  const [open, setOpen] = useState(false);
  const [trims, setTrims] = useState<Trims>({});
  /* Set when the server took the change but had nowhere to write it down,
     which is a studio started without -rig. The knob still works on the room
     in front of it; it will not survive a restart, and the difference between
     those two is worth a line of text. */
  const [unsaved, setUnsaved] = useState<string | null>(null);
  /* What has not been sent yet. A drag is dozens of change events and the
   * server needs the last one, not all of them. */
  const pending = useRef<Record<string, Trim>>({});
  const timer = useRef<number | null>(null);

  useEffect(() => {
    let live = true;
    void fetch('/api/live/trim')
      .then((r) => (r.ok ? r.json() : null))
      .then((got) => { if (live && got?.trim) setTrims(got.trim); })
      .catch(() => { /* the sliders still work, they just start at zero */ });
    return () => { live = false; };
  }, []);

  const send = useCallback((instrument: string, next: Trim) => {
    pending.current[instrument] = next;
    if (timer.current !== null) return;
    timer.current = window.setTimeout(() => {
      timer.current = null;
      const batch = pending.current;
      pending.current = {};
      for (const [id, body] of Object.entries(batch)) {
        void fetch('/api/live/trim', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instrument: id, ...body }),
        })
          .then((r) => (r.ok ? r.json() : null))
          .then((said) => setUnsaved(said?.unsaved ?? null))
          .catch(() => { /* the next drag sends it again */ });
      }
    }, SEND_MS);
  }, []);

  const move = (id: string, what: keyof Trim) =>
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const next = { ...(trims[id] ?? NONE), [what]: Number(e.target.value) };
      setTrims((was) => ({ ...was, [id]: next }));
      send(id, next);
    };

  const reset = (id: string) => {
    setTrims((was) => ({ ...was, [id]: NONE }));
    send(id, NONE);
  };

  const touched = (id: string) => {
    const t = trims[id];
    return !!t && (t.brightness !== 0 || t.saturation !== 0);
  };
  const anyTouched = lights.some(touched);

  if (lights.length === 0) return null;

  return (
    <span className="trim-host">
      <button
        className={'toggle' + (anyTouched ? ' on' : '')}
        onClick={() => setOpen((o) => !o)}
        title={anyTouched
          ? 'Some lights are being adjusted away from what the score says'
          : 'Adjust brightness and saturation per light, without changing the score'}
        aria-expanded={open}
      >trim{anyTouched ? ' •' : ''}</button>

      {open && (
        <div className="trim-panel" role="group" aria-label="Live colour trim">
          {unsaved && <p className="trim-unsaved">{unsaved}</p>}
          <p className="trim-why">
            Added to what the score asks for, on the way out. Nothing here
            changes the score. Kept in the rig, so a show gets it too and it
            survives a restart.
          </p>
          {lights.map((id) => {
            const t = trims[id] ?? NONE;
            return (
              <div className="trim-row" key={id}>
                <span className="trim-who" title={id}>{id}</span>
                <label>
                  <span className="trim-name">bright</span>
                  <input
                    type="range" min={-100} max={100} step={1}
                    value={t.brightness}
                    onChange={move(id, 'brightness')}
                    aria-label={'Brightness trim for ' + id + ', percent'}
                  />
                  <output className={t.brightness ? 'trim-value on' : 'trim-value'}>
                    {t.brightness > 0 ? '+' : ''}{t.brightness}
                  </output>
                </label>
                <label>
                  <span className="trim-name">colour</span>
                  <input
                    type="range" min={-100} max={100} step={1}
                    value={t.saturation}
                    onChange={move(id, 'saturation')}
                    aria-label={'Saturation trim for ' + id + ', percent'}
                  />
                  <output className={t.saturation ? 'trim-value on' : 'trim-value'}>
                    {t.saturation > 0 ? '+' : ''}{t.saturation}
                  </output>
                </label>
                <button
                  className="trim-reset"
                  onClick={() => reset(id)}
                  disabled={!touched(id)}
                  title={'Back to the score as written for ' + id}
                  aria-label={'Reset trim for ' + id}
                >reset</button>
              </div>
            );
          })}
        </div>
      )}
    </span>
  );
}
