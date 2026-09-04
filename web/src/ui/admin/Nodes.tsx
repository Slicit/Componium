/* Telling a board what is attached to it.
 *
 * Distinct from Devices, which edits the rig: the rig says what instruments the
 * show has and where to reach them, and this says what is physically wired to
 * one board. A rig entry names an instrument; a board decides whether that
 * instrument exists.
 *
 * The page that makes latency measurable. Until the board could be configured,
 * a fan's declared dead time was compiled into firmware, so measuring the real
 * one meant editing C and reflashing and nobody ever did. It is a field here.
 */

import { useCallback, useEffect, useState } from 'react';

import { Boards } from './Boards';

interface Attached {
  id: string;
  type: 'pwm' | 'ws28xx' | 'relay';
  gpio: number;
  kind: string;
  freqHz?: number;
  pixels?: number;
  active?: string;
  latencyMs?: number;
  rampUpMs?: number;
  rampDownMs?: number;
  safe?: number;
}

interface Board {
  name?: string;
  firmware?: string;
  chip?: string;
  instruments: { index: number; id: string; kind: string; latencyMs: number }[];
}

/** What a build contains. A device is what a configuration says is plugged in. */
const TYPES = [
  { id: 'pwm', label: 'PWM', hint: 'Fans, dimmable lights, misters' },
  { id: 'ws28xx', label: 'LED strip', hint: 'WS2812 and friends' },
  { id: 'relay', label: 'Relay', hint: 'Foggers, valves, anything switched' },
] as const;

/* The kinds a device can be come from the server, not from a list here.
 *
 * They are what a score addresses, and the rig already publishes them. A second
 * copy would be right on the day it was written and wrong the first time a kind
 * is added, which is the drift the parity rule exists to stop. */

/** Defaults per type, so a new row is already nearly right. */
function blank(type: Attached['type']): Attached {
  switch (type) {
    case 'ws28xx':
      return { id: 'light.strip', type, gpio: 5, kind: 'light', pixels: 30, latencyMs: 20 };
    case 'relay':
      return { id: 'fog.left', type, gpio: 21, kind: 'fog', active: 'high', latencyMs: 2000 };
    default:
      return {
        id: 'wind.main', type, gpio: 18, kind: 'wind', freqHz: 25000,
        latencyMs: 1200, rampUpMs: 1800, rampDownMs: 3000,
      };
  }
}

export function Nodes() {
  const [addr, setAddr] = useState('');
  const [secret, setSecret] = useState('');
  /* A remembered board, reached by name. When this is set the studio supplies
   * both the address and the secret, so neither is in this page at all.
   *
   * Named picked rather than board because board already means the description
   * a node sends back about itself, which is a different thing entirely. */
  const [picked, setPicked] = useState('');
  const [board, setBoard] = useState<Board | null>(null);
  const [devices, setDevices] = useState<Attached[]>([]);
  const [kinds, setKinds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let live = true;
    fetch('/api/rig/options')
      .then((r) => (r.ok ? r.json() : { kinds: [] }))
      .then((o: { kinds?: { kind: string }[] }) => {
        if (live) setKinds((o.kinds ?? []).map((k) => k.kind));
      })
      .catch(() => { /* The page still works; the kind stays what it was. */ });
    return () => { live = false; };
  }, []);

  /* `pick` overrides the board in state, because a click has to ask about the
   * board that was just clicked and setBoard has not landed by then. */
  const ask = useCallback(async (configure: boolean, pick?: string) => {
    const named = pick ?? picked;
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const res = await fetch('/api/node', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: named
          ? JSON.stringify({ board: named, configure, devices })
          : JSON.stringify({ addr, secret, configure, devices }),
      });
      const said = await res.text();
      if (!res.ok) {
        setError(said.trim() || 'the board did not answer');
        return;
      }
      const got: Board = JSON.parse(said);
      setBoard(got);
      if (configure) setSaved(true);
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusy(false);
    }
  }, [addr, secret, picked, devices]);

  const change = (i: number, patch: Partial<Attached>) => {
    setDevices((was) => was.map((d, n) => {
      if (n !== i) return d;
      // Changing the type changes which fields mean anything, so it starts
      // from that type's defaults and keeps the name and pin somebody chose.
      if (patch.type && patch.type !== d.type) {
        return { ...blank(patch.type), id: d.id, gpio: d.gpio, kind: d.kind };
      }
      return { ...d, ...patch };
    }));
    setSaved(false);
  };

  return (
    <div className="adm-page adm-wide">
      <h2>Boards</h2>
      <p className="dim">
        What is physically wired to one ESP32. Separate from the rig, which says
        what instruments the show has: a rig entry names an instrument, and the
        board decides whether that instrument exists.
      </p>

      <Boards
        picked={picked}
        onPick={(name) => {
          /* Clearing the typed pair, so there is never a question about which
           * of the two the next request will use. */
          setPicked(name);
          setAddr('');
          setSecret('');
          setDevices([]);
          void ask(false, name);
        }}
      />

      <section className="adm-card">
        <h3>A board that is not on the list yet</h3>
        <div className="adm-row">
          <input
            type="text" value={addr} placeholder="192.168.1.145"
            aria-label="Board address"
            onChange={(e) => setAddr(e.target.value)}
          />
          <input
            type="password" value={secret} placeholder="shared secret"
            aria-label="Shared secret"
            onChange={(e) => setSecret(e.target.value)}
          />
          <button disabled={!addr || busy} onClick={() => void ask(false)}>
            {busy ? 'asking…' : 'Ask what it has'}
          </button>
        </div>
        <p className="dim small">
          The secret is used for this exchange and not kept. A board that accepts
          configuration ignores anyone who does not have it, so a wrong one looks
          exactly like a board that is not there.
        </p>
      </section>

      {error && <p className="adm-warn">{error}</p>}

      {board && (
        <>
          <section className="adm-card">
            <h3>{board.name || 'this board'}</h3>
            <dl className="adm-facts">
              <dt>firmware</dt><dd>{board.firmware || 'unknown'}</dd>
              <dt>chip</dt><dd>{board.chip || 'unknown'}</dd>
              <dt>attached</dt>
              <dd>
                {board.instruments.length === 0
                  ? 'nothing yet, which is what a freshly flashed board says'
                  : board.instruments.map((i) => i.id).join(', ')}
              </dd>
            </dl>
            {board.instruments.length > 0 && (
              <button
                className="adm-reset"
                onClick={() => {
                  // Start from what the board says it has, so editing an
                  // existing board does not mean typing it all again.
                  setDevices(board.instruments.map((i) => ({
                    ...blank('pwm'), id: i.id, kind: i.kind,
                    latencyMs: i.latencyMs,
                  })));
                }}
              >start from what it has</button>
            )}
          </section>

          <section className="adm-card">
            <h3>What is wired to it</h3>
            <div className="adm-scroll">
              <table className="adm-table adm-edit">
                <thead>
                  <tr>
                    <th>Name</th><th>Type</th><th className="num">GPIO</th>
                    <th>Kind</th><th className="num">Latency</th><th /><th />
                  </tr>
                </thead>
                <tbody>
                  {devices.map((d, i) => (
                    <tr key={i}>
                      <td>
                        <input
                          type="text" value={d.id}
                          aria-label={'Device ' + (i + 1) + ' name'}
                          onChange={(e) => change(i, { id: e.target.value })}
                        />
                      </td>
                      <td>
                        <select
                          value={d.type}
                          aria-label={'Device ' + (i + 1) + ' type'}
                          onChange={(e) => change(i, { type: e.target.value as Attached['type'] })}
                        >
                          {TYPES.map((t) => (
                            <option key={t.id} value={t.id} title={t.hint}>{t.label}</option>
                          ))}
                        </select>
                      </td>
                      <td className="num">
                        <input
                          type="number" min={0} max={39} value={d.gpio}
                          aria-label={'Device ' + (i + 1) + ' gpio'}
                          onChange={(e) => change(i, { gpio: Number(e.target.value) })}
                        />
                      </td>
                      <td>
                        <select
                          value={d.kind}
                          aria-label={'Device ' + (i + 1) + ' kind'}
                          onChange={(e) => change(i, { kind: e.target.value })}
                        >
                          {/* Before the list arrives, at least what this
                              device already is, so the select is never empty. */}
                          {(kinds.length ? kinds : [d.kind])
                            .map((k) => <option key={k} value={k}>{k}</option>)}
                        </select>
                      </td>
                      <td className="num">
                        <input
                          type="number" min={0} max={10000} step={10}
                          value={d.latencyMs ?? 0}
                          aria-label={'Device ' + (i + 1) + ' latency'}
                          onChange={(e) => change(i, { latencyMs: Number(e.target.value) })}
                        />
                      </td>
                      <td className="dim small">
                        {d.type === 'ws28xx' && (
                          <label>
                            pixels{' '}
                            <input
                              type="number" min={1} max={300} value={d.pixels ?? 30}
                              aria-label={'Device ' + (i + 1) + ' pixels'}
                              onChange={(e) => change(i, { pixels: Number(e.target.value) })}
                            />
                          </label>
                        )}
                        {d.type === 'relay' && (
                          <label>
                            closed on{' '}
                            <select
                              value={d.active ?? 'high'}
                              aria-label={'Device ' + (i + 1) + ' active level'}
                              onChange={(e) => change(i, { active: e.target.value })}
                            >
                              <option value="high">high</option>
                              <option value="low">low</option>
                            </select>
                          </label>
                        )}
                        {d.type === 'pwm' && (
                          <label>
                            Hz{' '}
                            <input
                              type="number" min={100} max={40000} step={100}
                              value={d.freqHz ?? 25000}
                              aria-label={'Device ' + (i + 1) + ' frequency'}
                              onChange={(e) => change(i, { freqHz: Number(e.target.value) })}
                            />
                          </label>
                        )}
                      </td>
                      <td>
                        <button
                          className="adm-remove"
                          aria-label={'Remove ' + d.id}
                          onClick={() => {
                            setDevices((was) => was.filter((_, n) => n !== i));
                            setSaved(false);
                          }}
                        >remove</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="adm-row">
              <button onClick={() => { setDevices((was) => [...was, blank('pwm')]); setSaved(false); }}>
                Add a device
              </button>
              <span className="spacer" />
              {saved && <span className="dim small">the board took it</span>}
              <button className="adm-go" disabled={busy} onClick={() => void ask(true)}>
                Write it to the board
              </button>
            </div>

            <p className="dim small">
              Latency is the number that matters and the one nobody can guess:
              the conductor fires every cue that far early. Film your fan, drive
              it from the sliders, count frames, and put the real number here.
              Until this page existed it was compiled into the firmware, which is
              why the one it shipped with has always been a guess.
            </p>
            <p className="dim small">
              Not every pin can do this, and a configuration naming one that
              cannot is refused whole with the reason. The table on the Firmware
              page says which and why.
            </p>
          </section>
        </>
      )}
    </div>
  );
}
