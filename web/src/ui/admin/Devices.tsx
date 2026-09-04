/* What is in the room, and where to reach it.
 *
 * This edits the rig *file*, not a copy of it. That distinction is the whole
 * reason it is safe to have at all: there is still one place that says what is
 * on the end of every wire, and it is still a file anybody can open in an
 * editor. A studio keeping its own idea of the hardware would be a studio that
 * disagrees with the conductor, and the conductor is the one holding the mains.
 *
 * The menus are built from what the server says a rig may contain, rather than
 * from a list typed in here. The loader dispatches on that same table, so a
 * driver this page offers is a driver that will start, and a new one becomes
 * available here the day it exists there rather than the day somebody
 * remembers this file.
 */

import { useCallback, useEffect, useState } from 'react';

interface Device {
  id: string;
  kind: string;
  driver: string;
  latency: number;
  addr?: string;
  universe?: number;
  start?: number;
  mode?: string;
  position: [number, number, number];
}

interface Rig {
  name: string;
  editable: boolean;
  instruments: Device[];
}

interface Options {
  kinds: { kind: string; drivers: string[] }[];
  modes: string[];
  editable: boolean;
}

interface Shelf {
  /** Whether there is a directory of rigs to pick from at all. */
  shelf: boolean;
  current: string;
  rigs: string[];
}

/** A device that is not virtual is a device that will move something. */
const isReal = (d: Device) => (d.driver || 'virtual') !== 'virtual';

/** What a driver needs to be reachable. */
const wantsAddress = (driver: string) => driver === 'cip' || driver === 'motion';
const wantsUniverse = (driver: string) => driver === 'sacn';

/**
 * The fields a driver cannot go without, filled in when it is chosen.
 *
 * Held rather than merely shown. A DMX fixture with no start address is not a
 * fixture with a start address of 1, it is a rig the conductor will refuse, and
 * an input displaying `value ?? 1` over an undefined number tells somebody it
 * is set when nothing is set at all.
 */
function withDriverDefaults(d: Device, driver: string): Device {
  const next: Device = { ...d, driver };
  if (driver === 'sacn') {
    next.universe = d.universe ?? 1;
    next.start = d.start ?? 1;
    next.mode = d.mode || 'rgb';
  }
  return next;
}

function blank(kind: string, drivers: string[]): Device {
  const driver = drivers[0] ?? 'virtual';
  return withDriverDefaults({
    id: kind + '.new', kind, driver,
    latency: 0, position: [0, 1, 1],
  }, driver);
}

/* An address, chosen from the boards this installation has.
 *
 * A select rather than a text field because an IP is the one thing about a
 * board nobody remembers and the one thing that changes underneath them: DHCP
 * moves it and every rig entry pointing at it is quietly wrong, with no symptom
 * except a cue that does not arrive.
 *
 * Typing one stays possible. The first board is reached before there is a list
 * to pick from, and somebody debugging wants to point at a thing that is not on
 * any list. An address that matches no board selects that option on its own, so
 * a rig written before any of this still shows what it says.
 */
export function BoardPicker({ boards, value, disabled, label, onChange }: {
  boards: { name: string; addr: string }[];
  value: string;
  disabled: boolean;
  label: string;
  onChange: (addr: string) => void;
}) {
  const known = boards.some((b) => b.addr === value);
  const [typing, setTyping] = useState(!known && value !== '');

  if (boards.length === 0) {
    return (
      <input
        type="text" value={value} disabled={disabled}
        placeholder="192.168.1.90:5570"
        aria-label={label + ' address'}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <span className="adm-picker">
      <select
        value={typing || (!known && value !== '') ? '' : value}
        disabled={disabled}
        aria-label={label + ' board'}
        onChange={(e) => {
          if (e.target.value === '') {
            setTyping(true);
            return;
          }
          setTyping(false);
          onChange(e.target.value);
        }}
      >
        <option value="">an address not on the list</option>
        {boards.map((b) => (
          <option key={b.name} value={b.addr}>{b.name} &middot; {b.addr}</option>
        ))}
      </select>
      {(typing || (!known && value !== '')) && (
        <input
          type="text" value={value} disabled={disabled}
          placeholder="192.168.1.90:5570"
          aria-label={label + ' address'}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
    </span>
  );
}

export function Devices() {
  const [rig, setRig] = useState<Rig | null>(null);
  const [options, setOptions] = useState<Options | null>(null);
  const [shelf, setShelf] = useState<Shelf | null>(null);
  /* The boards this installation has, so a CIP instrument can be pointed at one
   * by name. Empty is normal: an installation with no boards attached yet, and
   * then the address is typed the way it always was. */
  const [boards, setBoards] = useState<{ name: string; addr: string }[]>([]);
  const [devices, setDevices] = useState<Device[]>([]);
  const [name, setName] = useState('');
  const [dirty, setDirty] = useState(false);
  const [problems, setProblems] = useState<string[]>([]);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [r, o, sh] = await Promise.all([
        fetch('/api/rig').then((x) => (x.ok ? x.json() : Promise.reject(new Error('none loaded')))),
        fetch('/api/rig/options').then((x) => (x.ok ? x.json() : { kinds: [], modes: [] })),
        fetch('/api/rigs').then((x) => (x.ok ? x.json() : { shelf: false, current: '', rigs: [] })),
      ]);
      setRig(r);
      setOptions(o);
      setShelf(sh);
      setDevices(r.instruments ?? []);
      setName(r.name ?? '');
      setDirty(false);
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    let live = true;
    fetch('/api/boards')
      .then((r) => (r.ok ? r.json() : { boards: [] }))
      .then((got: { boards?: { name: string; addr: string }[] }) => {
        if (live) setBoards(got.boards ?? []);
      })
      .catch(() => { /* The page still works; the address stays typed. */ });
    return () => { live = false; };
  }, []);

  const change = (i: number, patch: Partial<Device>) => {
    setDevices((was) => was.map((d, n) => {
      if (n !== i) return d;
      const next = { ...d, ...patch };
      return patch.driver !== undefined ? withDriverDefaults(next, patch.driver) : next;
    }));
    setDirty(true);
    setSaved(false);
  };

  const driversFor = (kind: string) =>
    options?.kinds.find((k) => k.kind === kind)?.drivers ?? ['virtual'];

  /* Changing a kind can strand a driver that kind cannot use, so it moves to
   * one it can rather than leaving a rig that will not start. */
  const changeKind = (i: number, kind: string) => {
    const allowed = driversFor(kind);
    const keep = allowed.includes(devices[i].driver) ? devices[i].driver : allowed[0];
    change(i, { kind, driver: keep });   // change() fills in what `keep` needs
  };

  const save = async () => {
    setProblems([]);
    setError(null);
    const res = await fetch('/api/rig', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, instruments: devices }),
    });
    if (res.ok) {
      setSaved(true);
      setDirty(false);
      await load();
      return;
    }
    /* Read once. A response body is a stream: calling json() consumes it, so
     * the text() that used to follow threw on an already read stream and every
     * failure that was not a validation list surfaced as "the studio refused
     * it". The server had been saying exactly what was wrong the whole time. */
    const said = await res.text().catch(() => '');
    try {
      const body = JSON.parse(said);
      if (Array.isArray(body?.problems)) { setProblems(body.problems); return; }
    } catch { /* not JSON, so it is a plain message and shown as one */ }
    setError(said.trim() || ('the studio refused it, with status ' + res.status));
  };

  const choose = async (name: string) => {
    setError(null);
    setProblems([]);
    const res = await fetch('/api/rigs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ rig: name }),
    });
    if (!res.ok) {
      setError((await res.text().catch(() => '')).trim() || 'could not switch rig');
      return;
    }
    setShelf(await res.json());
    await load();
  };

  const editable = rig?.editable ?? false;
  const real = devices.filter(isReal).length;

  return (
    <div className="adm-page adm-wide">
      <h2>Devices</h2>
      <p className="dim">
        This is the rig file, not a copy of it. Edit it here or in a text editor;
        both write the same place, which is what keeps the studio and the
        conductor agreeing about what is on the end of every wire.
      </p>

      {error && <p className="adm-warn">{error}</p>}

      {rig && !editable && (
        <p className="adm-warn">
          Read only: this studio was started without <code>-rig</code>, so what
          you see below was inferred from the score and there is no file to
          write it to.
        </p>
      )}

      {rig && (
        <>
          {shelf?.shelf && (
            <section className="adm-card">
              <div className="adm-set-head">
                <label htmlFor="rig-file">Rig in use</label>
                <span className="dim small">{shelf.rigs.length} on the shelf</span>
              </div>
              <select
                id="rig-file"
                value={shelf.current}
                onChange={(e) => void choose(e.target.value)}
                disabled={dirty}
                title={dirty
                  ? 'Save or reload first: switching now would lose the edits'
                  : 'Which rig this installation is using'}
              >
                {shelf.rigs.map((f) => <option key={f} value={f}>{f}</option>)}
              </select>
              <p className="dim small">
                The choice is a file on the shelf, not a setting in this browser,
                so a conductor pointed at the same directory plays whichever rig
                is picked here. It reads it when it starts, so a running show
                keeps the one it opened with.
              </p>
            </section>
          )}

          <section className="adm-card">
            <div className="adm-set-head">
              <label htmlFor="rig-name">Rig name</label>
              <span className="dim small">
                {devices.length} instrument{devices.length === 1 ? '' : 's'},
                {' '}{real} on real hardware
              </span>
            </div>
            <input
              id="rig-name" type="text" value={name} disabled={!editable}
              onChange={(e) => { setName(e.target.value); setDirty(true); setSaved(false); }}
            />
          </section>

          <section className="adm-card">
            <div className="adm-scroll">
              <table className="adm-table adm-edit">
                <thead>
                  <tr>
                    <th>Instrument</th><th>Kind</th><th>Driver</th>
                    <th>Where</th><th className="num">Latency</th><th />
                  </tr>
                </thead>
                <tbody>
                  {devices.map((d, i) => (
                    <tr key={i}>
                      <td>
                        <input
                          type="text" value={d.id} disabled={!editable}
                          aria-label={'Instrument ' + (i + 1) + ' id'}
                          onChange={(e) => change(i, { id: e.target.value })}
                        />
                      </td>
                      <td>
                        <select
                          value={d.kind} disabled={!editable}
                          aria-label={'Instrument ' + (i + 1) + ' kind'}
                          onChange={(e) => changeKind(i, e.target.value)}
                        >
                          {options?.kinds.map((k) => (
                            <option key={k.kind} value={k.kind}>{k.kind}</option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <select
                          value={d.driver || 'virtual'} disabled={!editable}
                          aria-label={'Instrument ' + (i + 1) + ' driver'}
                          onChange={(e) => change(i, { driver: e.target.value })}
                        >
                          {driversFor(d.kind).map((v) => (
                            <option key={v} value={v}>{v}</option>
                          ))}
                        </select>
                      </td>
                      <td>
                        {wantsAddress(d.driver) && (
                          <BoardPicker
                            boards={boards}
                            value={d.addr ?? ''}
                            disabled={!editable}
                            label={'Instrument ' + (i + 1)}
                            onChange={(addr) => change(i, { addr })}
                          />
                        )}
                        {wantsUniverse(d.driver) && (
                          <span className="adm-dmx">
                            <input
                              type="text" value={d.addr ?? ''} disabled={!editable}
                              placeholder="192.168.1.90:5568"
                              aria-label={'Instrument ' + (i + 1) + ' address'}
                              onChange={(e) => change(i, { addr: e.target.value })}
                            />
                            <input
                              type="number" min={1} max={63999} value={d.universe ?? ''}
                              disabled={!editable}
                              aria-label={'Instrument ' + (i + 1) + ' universe'}
                              onChange={(e) => change(i, { universe: Number(e.target.value) })}
                            />
                            <input
                              type="number" min={1} max={512} value={d.start ?? ''}
                              disabled={!editable}
                              aria-label={'Instrument ' + (i + 1) + ' DMX address'}
                              onChange={(e) => change(i, { start: Number(e.target.value) })}
                            />
                            <select
                              value={d.mode ?? ''} disabled={!editable}
                              aria-label={'Instrument ' + (i + 1) + ' mode'}
                              onChange={(e) => change(i, { mode: e.target.value })}
                            >
                              {(options?.modes ?? []).map((m) => (
                                <option key={m} value={m}>{m}</option>
                              ))}
                            </select>
                          </span>
                        )}
                        {!wantsAddress(d.driver) && !wantsUniverse(d.driver) && (
                          <span className="dim small">nothing to reach</span>
                        )}
                      </td>
                      <td className="num">
                        <input
                          type="number" min={0} max={10} step={0.01}
                          value={d.latency ?? 0} disabled={!editable}
                          aria-label={'Instrument ' + (i + 1) + ' latency'}
                          onChange={(e) => change(i, { latency: Number(e.target.value) })}
                        />
                      </td>
                      <td>
                        <button
                          className="adm-remove" disabled={!editable}
                          title={'Remove ' + d.id}
                          aria-label={'Remove ' + d.id}
                          onClick={() => {
                            setDevices((was) => was.filter((_, n) => n !== i));
                            setDirty(true); setSaved(false);
                          }}
                        >remove</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {editable && (
              <div className="adm-row">
                <button
                  onClick={() => {
                    const kind = options?.kinds[0]?.kind ?? 'light';
                    setDevices((was) => [...was, blank(kind, driversFor(kind))]);
                    setDirty(true); setSaved(false);
                  }}
                >Add a device</button>
                <span className="spacer" />
                {problems.length === 0 && saved && <span className="dim small">saved</span>}
                {dirty && <span className="dim small">unsaved</span>}
                <button className="adm-go" disabled={!dirty} onClick={() => void save()}>
                  Save the rig
                </button>
              </div>
            )}
          </section>

          {problems.length > 0 && (
            <section className="adm-card">
              <h3>Not saved</h3>
              <ul className="adm-problems">
                {problems.map((p) => <li key={p}>{p}</li>)}
              </ul>
            </section>
          )}

          <p className="dim small">
            The conductor reads the rig when it starts. Saving here changes what
            the next show does, not what a running one is doing.
          </p>
        </>
      )}
    </div>
  );
}
