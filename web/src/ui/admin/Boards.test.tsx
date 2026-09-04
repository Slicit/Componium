// @vitest-environment jsdom

/* The shelf of boards, mounted.
 *
 * The complaint this answers: attaching a board left no trace, so every visit
 * started by finding an address again. What matters here is that a board
 * survives, that online and offline are visible and distinguishable from "not
 * asked yet", that forgetting one is deliberate, and that the secret never
 * comes back into the page.
 *
 * The fixtures are the shapes the Go handlers really send, taken from their
 * tests rather than imagined.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor, act } from '@testing-library/react';
import { Boards } from './Boards';

/** What /api/boards returns. Note there is no secret in it, ever. */
const saved = {
  editable: true,
  boards: [
    { name: 'bench', addr: '192.168.1.145:5570', note: 'the cracked case', hasSecret: true },
    { name: 'ceiling', addr: '192.168.1.146:5570', hasSecret: true },
    { name: 'spare', addr: '192.168.1.147:5570', hasSecret: false },
  ],
};

/** What /api/boards/check returns. */
const checked = {
  boards: [
    {
      name: 'bench', addr: '192.168.1.145:5570', online: true, firmware: '0.3',
      instruments: [{ index: 0, id: 'wind.main', kind: 'wind', latencyMs: 1200 }],
    },
    {
      name: 'ceiling', addr: '192.168.1.146:5570', online: false,
      why: 'cip: no hello from 192.168.1.146:5570 within 2s: i/o timeout',
    },
    { name: 'spare', addr: '192.168.1.147:5570', online: false, why: 'no secret is stored' },
  ],
};

let put: unknown[] = [];
let list = saved;

beforeEach(() => {
  put = [];
  list = JSON.parse(JSON.stringify(saved));
  vi.stubGlobal('confirm', vi.fn(() => true));
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url === '/api/boards' && init?.method === 'PUT') {
      const body = JSON.parse(init.body as string);
      put.push(body);
      /* Stateful, because the real one is: a save returns the shelf as it now
       * stands, and a fixture that forgets would let a broken delete pass. */
      list = {
        editable: true,
        boards: body.boards.map((b: { name: string; addr: string; note?: string }) => ({
          name: b.name, addr: b.addr, note: b.note, hasSecret: true,
        })),
      };
      return { ok: true, text: async () => JSON.stringify(list) } as Response;
    }
    if (url === '/api/boards') {
      return { ok: true, json: async () => list } as Response;
    }
    if (url === '/api/boards/check') {
      return { ok: true, json: async () => checked } as Response;
    }
    return { ok: false, text: async () => 'no' } as Response;
  }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

async function shelf(onPick: (name: string) => void = () => {}) {
  render(<Boards onPick={onPick} picked="" />);
  await waitFor(() => expect(screen.getByText('bench')).toBeTruthy());
}

describe('the shelf', () => {
  it('lists the boards that were attached', async () => {
    // The whole point: they are still here.
    await shelf();
    expect(screen.getByText('ceiling')).toBeTruthy();
    expect(screen.getByText('192.168.1.145:5570')).toBeTruthy();
    expect(screen.getByText('the cracked case')).toBeTruthy();
  });

  it('says which are online and which are not', async () => {
    await shelf();
    await waitFor(() => expect(screen.getByLabelText('bench is online')).toBeTruthy());
    expect(screen.getByLabelText('ceiling is offline')).toBeTruthy();
  });

  it('does not call a board offline before it has been asked', async () => {
    /* Three states, not two. A shelf that has only just loaded is not a shelf
     * that is down, and drawing them the same way means the page opens on a
     * column of red for boards that are all working. */
    let release: (v: unknown) => void = () => {};
    const hangs = new Promise((r) => { release = r; });
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url === '/api/boards') return { ok: true, json: async () => list } as Response;
      if (url === '/api/boards/check') { await hangs; return { ok: true, json: async () => checked } as Response; }
      return { ok: false, text: async () => 'no' } as Response;
    }));

    render(<Boards onPick={() => {}} picked="" />);
    await waitFor(() => expect(screen.getByText('bench')).toBeTruthy());

    const dot = screen.getByLabelText('bench not checked');
    expect(dot.className).toContain('is-unknown');
    expect(dot.className).not.toContain('is-off');

    await act(async () => { release(null); });
    await waitFor(() => expect(screen.getByLabelText('bench is online')).toBeTruthy());
  });

  it('shows what an online board is carrying', async () => {
    await shelf();
    await waitFor(() => expect(screen.getByText('wind.main')).toBeTruthy());
  });

  it('marks a board with no secret, because it can never answer', async () => {
    /* A board that has a secret ignores anyone without it, so a missing secret
     * is not a smaller problem than a wrong address: it is the same problem. */
    await shelf();
    expect(screen.getByText(/no secret/)).toBeTruthy();
  });

  it('attaches a board and remembers it', async () => {
    await shelf();
    fireEvent.click(screen.getByRole('button', { name: 'Attach a board' }));
    fireEvent.change(screen.getByLabelText('New board name'), { target: { value: 'new one' } });
    fireEvent.change(screen.getByLabelText('New board address'), { target: { value: '192.168.1.9' } });
    fireEvent.change(screen.getByLabelText('New board secret'), { target: { value: 'hunter2' } });
    fireEvent.click(screen.getByRole('button', { name: 'Attach' }));

    await waitFor(() => expect(put).toHaveLength(1));
    const sent = put[0] as { boards: { name: string; secret?: string }[] };
    expect(sent.boards.map((b) => b.name)).toContain('new one');
    // The secret goes up exactly once, on the save that stores it.
    expect(sent.boards.find((b) => b.name === 'new one')?.secret).toBe('hunter2');
  });

  it('never shows a secret it was given', async () => {
    await shelf();
    fireEvent.click(screen.getByRole('button', { name: 'Attach a board' }));
    fireEvent.change(screen.getByLabelText('New board name'), { target: { value: 'new one' } });
    fireEvent.change(screen.getByLabelText('New board address'), { target: { value: '192.168.1.9' } });
    const field = screen.getByLabelText('New board secret') as HTMLInputElement;
    expect(field.type).toBe('password');
    fireEvent.change(field, { target: { value: 'hunter2' } });
    fireEvent.click(screen.getByRole('button', { name: 'Attach' }));
    await waitFor(() => expect(put).toHaveLength(1));
    // Gone from the page as soon as it is stored: the server never sends it back.
    await waitFor(() => expect(screen.queryByLabelText('New board secret')).toBeNull());
    expect(document.body.textContent).not.toContain('hunter2');
  });

  it('forgets a board, and asks first', async () => {
    /* The secret goes with it and there is no reading one back off a board, so
     * an accidental click costs a USB cable. */
    await shelf();
    fireEvent.click(screen.getByRole('button', { name: 'Forget bench' }));
    expect(window.confirm).toHaveBeenCalled();

    await waitFor(() => expect(put).toHaveLength(1));
    const sent = put[0] as { boards: { name: string }[] };
    expect(sent.boards.map((b) => b.name)).toEqual(['ceiling', 'spare']);
    await waitFor(() => expect(screen.queryByText('bench')).toBeNull());
  });

  it('keeps a board when the question is declined', async () => {
    await shelf();
    vi.stubGlobal('confirm', vi.fn(() => false));
    fireEvent.click(screen.getByRole('button', { name: 'Forget bench' }));
    expect(put).toHaveLength(0);
    expect(screen.getByText('bench')).toBeTruthy();
  });

  it('does not send a secret back when only a note changed', async () => {
    /* The page never receives one, so it cannot return one. If the server did
     * not keep the stored secret, editing a note would lock us out. */
    await shelf();
    fireEvent.click(screen.getByRole('button', { name: 'Forget spare' }));
    await waitFor(() => expect(put).toHaveLength(1));
    const sent = put[0] as { boards: { secret?: string }[] };
    for (const b of sent.boards) expect(b.secret).toBeUndefined();
  });

  it('hands the chosen board up by name', async () => {
    // Reached by name from here on, so the secret stays on the server.
    const picked: string[] = [];
    await shelf((n: string) => picked.push(n));
    fireEvent.click(screen.getByRole('button', { name: 'bench' }));
    expect(picked).toEqual(['bench']);
  });

  it('says when there is nowhere to remember them', async () => {
    list = { editable: false, boards: [] } as typeof saved;
    await act(async () => { render(<Boards onPick={() => {}} picked="" />); });
    await waitFor(() => expect(screen.getByText(/cannot be remembered/)).toBeTruthy());
    expect(screen.queryByRole('button', { name: 'Attach a board' })).toBeNull();
  });
});
