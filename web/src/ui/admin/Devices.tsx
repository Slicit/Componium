/* What the rig says is out there.
 *
 * Read only, and deliberately: the rig is a file, it lives beside the score,
 * and every machine that runs the show reads the same one. A settings page
 * that edited it here would be a second source of truth for the one thing in
 * the system that must not have two.
 *
 * It earns its place during bring up. "Is the studio even loading my rig, and
 * does it think the fan is a real node or a virtual one" is the first question
 * anybody asks when a device does not move, and until now the only way to
 * answer it was to read the TOML over somebody's shoulder.
 */

import { useEffect, useState } from 'react';
import type { Instrument, Rig } from '../../core/score';

type Wired = { driver?: string; addr?: string; universe?: number };

function driverOf(inst: Wired): string {
  return inst.driver || 'virtual';
}

/** Real hardware, or a stand in that logs what it would have sent. */
function isReal(driver: string): boolean {
  return driver !== 'virtual';
}

function whereOf(inst: Wired): string {
  if (inst.addr) return inst.addr;
  if (inst.universe !== undefined) return 'universe ' + inst.universe;
  return '—';
}

export function Devices() {
  const [rig, setRig] = useState<Rig | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    void fetch('/api/rig')
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error('none loaded'))))
      .then((r) => { if (alive) setRig(r); })
      .catch((e) => { if (alive) setError(String(e.message || e)); });
    return () => { alive = false; };
  }, []);

  const instruments = (rig?.instruments ?? []) as (Instrument & Wired)[];
  const real = instruments.filter((i) => isReal(driverOf(i))).length;

  return (
    <div className="adm-page">
      <h2>Devices</h2>
      <p className="dim">
        From the rig file the studio was started with. To change any of it, edit
        that file: it is the one thing every machine running the show reads, and
        a second place to set it would be a second thing to be wrong.
      </p>

      {error && (
        <p className="adm-warn">
          No rig: {error}. Start the studio with <code>-rig</code>.
        </p>
      )}

      {rig && (
        <>
          <section className="adm-card">
            <h3>{rig.name || 'unnamed rig'}</h3>
            <p className="dim small">
              {instruments.length} instrument{instruments.length === 1 ? '' : 's'},
              {' '}{real} on real hardware.
              {real === 0 && ' Everything here is virtual, so nothing physical will move.'}
            </p>
          </section>

          <section className="adm-card">
            <div className="adm-scroll">
              <table className="adm-table">
                <thead>
                  <tr>
                    <th>Instrument</th><th>Kind</th><th>Driver</th>
                    <th>Where</th><th className="num">Latency</th>
                  </tr>
                </thead>
                <tbody>
                  {instruments.map((i) => {
                    const driver = driverOf(i);
                    return (
                      <tr key={i.id}>
                        <td><code>{i.id}</code></td>
                        <td>{i.kind}</td>
                        <td>
                          <span className={'adm-pill' + (isReal(driver) ? ' is-real' : '')}>
                            {driver}
                          </span>
                        </td>
                        <td className="dim">{whereOf(i)}</td>
                        <td className="num">{i.latency ? i.latency + ' s' : '—'}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </div>
  );
}
