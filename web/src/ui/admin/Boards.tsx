/* The boards this installation has, and which of them are switched on.
 *
 * Distinct from the rig, which says what the show uses, and from the table
 * below it, which says what is wired to one board. A board on a shelf exists
 * whether or not any rig mentions it, and until this page it left no trace at
 * all: an address was typed, used once, and forgotten when the tab closed.
 *
 * The secret is typed once and never comes back. Everything after that reaches
 * the board by name, so the string that authorises moving a relay onto a pin
 * lives in one file on the server rather than in a browser.
 */

import { useCallback, useEffect, useState } from 'react';

export interface Board {
  name: string;
  addr: string;
  note?: string;
  secret?: string;
  hasSecret: boolean;
}

export interface BoardStatus {
  name: string;
  addr: string;
  online: boolean;
  why?: string;
  firmware?: string;
  instruments?: { index: number; id: string; kind: string; latencyMs: number }[];
}

/** A row being added, before it is a board. */
const blankBoard = (): Board => ({ name: '', addr: '', secret: '', hasSecret: false });

export function Boards({ onPick, picked }: {
  onPick: (name: string) => void;
  picked: string;
}) {
  const [boards, setBoards] = useState<Board[]>([]);
  const [editable, setEditable] = useState(true);
  const [status, setStatus] = useState<Record<string, BoardStatus>>({});
  const [adding, setAdding] = useState<Board | null>(null);
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    const res = await fetch('/api/boards');
    if (!res.ok) return;
    const got = await res.json();
    setBoards(got.boards ?? []);
    setEditable(!!got.editable);
  }, []);

  const check = useCallback(async () => {
    setChecking(true);
    try {
      const res = await fetch('/api/boards/check', { method: 'POST' });
      if (!res.ok) return;
      const got: { boards: BoardStatus[] } = await res.json();
      const next: Record<string, BoardStatus> = {};
      for (const b of got.boards ?? []) next[b.name] = b;
      setStatus(next);
    } catch {
      /* Leaving the last known state is better than blanking the column: an
       * unreachable studio is not news about the boards. */
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => { void load().then(check); }, [load, check]);

  /* Saving the whole list, which is also how one is deleted. One path for every
   * edit, so a delete cannot rot separately from an add. */
  const save = useCallback(async (next: Board[]) => {
    setBusy(true);
    setError(null);
    try {
      const res = await fetch('/api/boards', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ boards: next }),
      });
      const said = await res.text();
      if (!res.ok) {
        setError(said.trim() || 'could not save that');
        return false;
      }
      const got = JSON.parse(said);
      setBoards(got.boards ?? []);
      void check();
      return true;
    } catch (e) {
      setError(String((e as Error).message || e));
      return false;
    } finally {
      setBusy(false);
    }
  }, [check]);

  const forget = (name: string) => {
    /* Confirmed, because the secret goes with it and there is no way to read
     * one back off a board: getting it wrong means a USB cable. */
    if (!window.confirm(
      `Forget ${name}?\n\nIts secret is stored only here. If you have no other ` +
      `copy, the only way back into that board is to reflash it over USB.`
    )) return;
    void save(boards.filter((b) => b.name !== name));
  };

  return (
    <section className="adm-card">
      <div className="adm-row">
        <h3>Boards</h3>
        <span className="spacer" />
        <button onClick={() => void check()} disabled={checking || boards.length === 0}>
          {checking ? 'checking…' : 'Check them'}
        </button>
        {editable && !adding && (
          <button onClick={() => setAdding(blankBoard())}>Attach a board</button>
        )}
      </div>

      {!editable && (
        <p className="dim small">
          This studio was started without a boards file, so what you attach here
          cannot be remembered. Start it with <code>-boards</code> pointing at a
          file, somewhere outside the repository: it holds the secrets.
        </p>
      )}

      {error && <p className="adm-warn">{error}</p>}

      {boards.length === 0 && !adding && (
        <p className="dim small">
          No boards yet. Attach one and it stays: the address and its secret are
          remembered, and everything after that reaches it by name.
        </p>
      )}

      {boards.length > 0 && (
        <div className="adm-scroll">
          <table className="adm-table">
            <thead>
              <tr>
                <th /><th>Name</th><th>Address</th><th>Attached</th><th>Note</th><th />
              </tr>
            </thead>
            <tbody>
              {boards.map((b) => {
                const st = status[b.name];
                return (
                  <tr key={b.name} className={picked === b.name ? 'is-current' : undefined}>
                    <td>
                      <span
                        className={'adm-dot ' + (st ? (st.online ? 'is-on' : 'is-off') : 'is-unknown')}
                        title={st ? (st.online ? 'answering' : st.why ?? 'no answer') : 'not checked yet'}
                        aria-label={st ? (st.online ? b.name + ' is online' : b.name + ' is offline') : b.name + ' not checked'}
                      />
                    </td>
                    <td>
                      <button className="adm-link" onClick={() => onPick(b.name)}>{b.name}</button>
                      {!b.hasSecret && (
                        <span className="dim small" title="Without one, this board cannot be reached at all">
                          {' '}no secret
                        </span>
                      )}
                    </td>
                    <td>{b.addr}</td>
                    <td className="dim">
                      {st?.online
                        ? (st.instruments?.length
                            ? st.instruments.map((i) => i.id).join(', ')
                            : 'nothing yet')
                        : (st ? shortReason(st.why) : '—')}
                    </td>
                    <td className="dim">{b.note ?? ''}</td>
                    <td>
                      {editable && (
                        <button className="adm-remove" aria-label={'Forget ' + b.name}
                          onClick={() => forget(b.name)}>forget</button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {adding && (
        <div className="adm-card adm-inset">
          <div className="adm-row">
            <input
              type="text" value={adding.name} placeholder="bench"
              aria-label="New board name"
              onChange={(e) => setAdding({ ...adding, name: e.target.value })}
            />
            <input
              type="text" value={adding.addr} placeholder="192.168.1.145"
              aria-label="New board address"
              onChange={(e) => setAdding({ ...adding, addr: e.target.value })}
            />
            <input
              type="password" value={adding.secret ?? ''} placeholder="shared secret"
              aria-label="New board secret"
              onChange={(e) => setAdding({ ...adding, secret: e.target.value })}
            />
            <input
              type="text" value={adding.note ?? ''} placeholder="note (optional)"
              aria-label="New board note"
              onChange={(e) => setAdding({ ...adding, note: e.target.value })}
            />
            <button className="adm-go" disabled={busy || !adding.name || !adding.addr}
              onClick={async () => {
                if (await save([...boards, adding])) setAdding(null);
              }}>Attach</button>
            <button onClick={() => { setAdding(null); setError(null); }}>Cancel</button>
          </div>
          <p className="dim small">
            The secret is stored on the server and never sent back to this page.
            Anything that reaches the board after this does so by name. The port
            is assumed to be CIP's unless you give one.
          </p>
        </div>
      )}
    </section>
  );
}

/** A reason short enough for a table cell, without losing which reason it is. */
function shortReason(why?: string): string {
  if (!why) return 'no answer';
  if (why.includes('no hello')) return 'no answer';
  if (why.includes('secret')) return 'refused';
  return 'no answer';
}
