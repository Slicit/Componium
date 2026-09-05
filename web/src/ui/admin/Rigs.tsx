/* The shelf: which rigs exist, which one is in use, and getting them on and
 * off this machine.
 *
 * One installation is not one rig. A bench with a board on it, the room as it
 * actually stands, and the demonstration that needs no hardware are three
 * different files, and until this page the only way to have a second one was a
 * shell on the server.
 *
 * The selection is a file on the shelf rather than a setting in the studio, so
 * a conductor pointed at the same directory plays whatever was chosen here. A
 * choice only the studio knew about would be a choice the thing holding the
 * mains does not.
 */

import { useCallback, useEffect, useRef, useState } from 'react';

interface Shelf {
  shelf: boolean;
  current: string;
  rigs: string[];
}

const EMPTY: Shelf = { shelf: false, current: '', rigs: [] };

/** The file name without its extension, which is what people call a rig. */
const plain = (file: string) => file.replace(/\.toml$/, '');

export function Rigs() {
  const [shelf, setShelf] = useState<Shelf>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [adding, setAdding] = useState<{ name: string; from: string } | null>(null);
  const file = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/rigs');
      if (res.ok) setShelf(await res.json());
    } catch {
      /* Leaving what is on screen is better than blanking it: an unreachable
         studio is not news about the shelf. */
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  /** Every change goes through here, so a refusal is reported the same way. */
  const send = useCallback(async (
    path: string, body: string | object | null, said: string,
  ) => {
    setBusy(true);
    setError(null);
    setNote(null);
    try {
      const res = await fetch(path, {
        method: 'POST',
        headers: typeof body === 'string'
          ? { 'Content-Type': 'text/plain' }
          : { 'Content-Type': 'application/json' },
        body: typeof body === 'string' ? body : JSON.stringify(body ?? {}),
      });
      const text = await res.text();
      if (!res.ok) {
        setError(text.trim() || 'that did not work');
        return false;
      }
      try {
        setShelf(JSON.parse(text));
      } catch {
        void load();
      }
      setNote(said);
      return true;
    } catch (e) {
      setError(String((e as Error).message || e));
      return false;
    } finally {
      setBusy(false);
    }
  }, [load]);

  const forget = (name: string) => {
    /* Confirmed, because a rig describes what is wired to the mains and there
       is no copy of it anywhere else unless somebody exported one. */
    if (!window.confirm(
      `Remove ${name}?\n\nThis deletes the file. If you have not exported it, `
      + `everything it says about addresses, pins and positions is gone.`
    )) return;
    void send('/api/rigs/delete', { name: plain(name) }, name + ' removed');
  };

  const bring = async (chosen: File) => {
    const text = await chosen.text();
    const name = plain(chosen.name);
    const already = shelf.rigs.some((r) => plain(r) === name);
    if (already && !window.confirm(
      `${name} is already on this shelf.\n\nReplace it with the file you just `
      + `chose? What is there now will be gone.`
    )) return;
    await send(
      '/api/rigs/import?name=' + encodeURIComponent(name)
      + (already ? '&replace=yes' : ''),
      text, name + ' imported');
  };

  if (!shelf.shelf) {
    return (
      <section className="adm-card">
        <h2>Rigs</h2>
        <p className="dim small">
          This studio was started with <code>-rig</code> pointing at a single
          file rather than a directory, so there is no shelf to manage. Point it
          at a directory and every rig in it can be chosen, copied and swapped
          from here.
        </p>
        <p>
          <a className="adm-link" href="/api/rigs/export" download>
            Export {shelf.current || 'the rig'}
          </a>
        </p>
      </section>
    );
  }

  return (
    <section className="adm-card">
      <div className="adm-row">
        <h2>Rigs</h2>
        <span className="spacer" />
        <button onClick={() => file.current?.click()} disabled={busy}>Import</button>
        {!adding && (
          <button onClick={() => setAdding({ name: '', from: shelf.current })}>
            New rig
          </button>
        )}
      </div>

      <input
        ref={file} type="file" accept=".toml,text/plain" hidden
        aria-label="Import a rig file"
        onChange={(e) => {
          const chosen = e.target.files?.[0];
          e.target.value = ''; // so the same file can be chosen twice
          if (chosen) void bring(chosen);
        }}
      />

      <p className="dim small">
        The one in use is what a show plays. The choice is a file on the shelf,
        so a conductor reading the same directory follows it without being told.
      </p>

      {error && <p className="adm-warn">{error}</p>}
      {note && <p className="dim small">{note}</p>}

      <div className="adm-scroll">
        <table className="adm-table">
          <thead>
            <tr><th /><th>Rig</th><th /></tr>
          </thead>
          <tbody>
            {shelf.rigs.map((r) => {
              const current = r === shelf.current;
              return (
                <tr key={r} className={current ? 'is-current' : undefined}>
                  <td>
                    <input
                      type="radio" name="rig" checked={current} disabled={busy}
                      aria-label={'Use ' + plain(r)}
                      onChange={() => void send('/api/rigs', { rig: r }, 'now using ' + plain(r))}
                    />
                  </td>
                  <td>
                    {plain(r)}
                    {current && <span className="dim small"> in use</span>}
                  </td>
                  <td className="adm-actions">
                    <a className="adm-link" download
                       href={'/api/rigs/export?rig=' + encodeURIComponent(plain(r))}
                    >export</a>
                    <button
                      disabled={busy}
                      title={'Start a new rig from ' + plain(r)}
                      onClick={() => setAdding({ name: '', from: r })}
                    >copy</button>
                    <button
                      className="adm-remove"
                      disabled={busy || shelf.rigs.length <= 1}
                      title={shelf.rigs.length <= 1
                        ? 'The only rig on the shelf. A shelf with nothing on it '
                          + 'is a studio that will not open.'
                        : 'Remove ' + plain(r)}
                      aria-label={'Remove ' + plain(r)}
                      onClick={() => forget(r)}
                    >remove</button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {adding && (
        <div className="adm-card adm-inset">
          <div className="adm-row">
            <input
              type="text" value={adding.name} placeholder="the-room"
              aria-label="New rig name" autoFocus
              onChange={(e) => setAdding({ ...adding, name: e.target.value })}
            />
            <label className="dim small">
              from{' '}
              <select
                value={adding.from}
                aria-label="Start the new rig from"
                onChange={(e) => setAdding({ ...adding, from: e.target.value })}
              >
                <option value="">nothing</option>
                {shelf.rigs.map((r) => (
                  <option key={r} value={r}>{plain(r)}</option>
                ))}
              </select>
            </label>
            <button
              className="adm-go" disabled={busy || !adding.name.trim()}
              onClick={async () => {
                const ok = await send('/api/rigs/new', {
                  name: adding.name.trim(), from: adding.from, select: false,
                }, adding.name.trim() + ' added');
                if (ok) setAdding(null);
              }}
            >Create</button>
            <button onClick={() => { setAdding(null); setError(null); }}>Cancel</button>
          </div>
          <p className="dim small">
            Copying is how a second rig usually starts: the room as it stands,
            plus one change. From nothing gives you one virtual light to build
            on, because a rig with nothing in it cannot be opened.
          </p>
        </div>
      )}
    </section>
  );
}
