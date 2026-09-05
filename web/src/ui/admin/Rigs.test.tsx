// @vitest-environment jsdom

/* The shelf, from the operator's side.
 *
 * Most of these are about not losing a file. A rig says what is wired to the
 * mains and there is no copy of it anywhere unless somebody exported one, so
 * the interesting cases are the refusals and the confirmations.
 */

import { render, screen, waitFor, fireEvent, cleanup, act } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { Rigs } from './Rigs';

let shelf: { shelf: boolean; current: string; rigs: string[] };
let sent: { url: string; body: string }[] = [];
let refuse: string | null = null;

beforeEach(() => {
  shelf = { shelf: true, current: 'demo-rig.toml', rigs: ['demo-rig.toml', 'bench.toml'] };
  sent = [];
  refuse = null;
  vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => {
    if (!init || init.method !== 'POST') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(shelf) } as Response);
    }
    sent.push({ url, body: String(init.body) });
    if (refuse) {
      return Promise.resolve({
        ok: false, status: 409, text: () => Promise.resolve(refuse as string),
      } as Response);
    }
    return Promise.resolve({
      ok: true, text: () => Promise.resolve(JSON.stringify(shelf)),
    } as Response);
  }));
  vi.stubGlobal('confirm', vi.fn(() => true));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function open() {
  render(<Rigs />);
  await waitFor(() => expect(screen.getByText('bench')).toBeTruthy());
}

describe('the shelf', () => {
  it('lists every rig and says which one a show would play', async () => {
    await open();
    expect(screen.getByText('demo-rig')).toBeTruthy();
    expect(screen.getByText('in use')).toBeTruthy();
    expect((screen.getByLabelText('Use demo-rig') as HTMLInputElement).checked).toBe(true);
    expect((screen.getByLabelText('Use bench') as HTMLInputElement).checked).toBe(false);
  });

  it('switches which rig is in use', async () => {
    await open();
    fireEvent.click(screen.getByLabelText('Use bench'));
    await waitFor(() => expect(sent.length).toBe(1));
    expect(sent[0].url).toBe('/api/rigs');
    expect(JSON.parse(sent[0].body)).toEqual({ rig: 'bench.toml' });
  });

  it('starts a new rig from one that already works', async () => {
    /* How a second rig actually comes to exist: the room as it stands, plus
     * one change. Starting from blank means retyping every address. */
    await open();
    fireEvent.click(screen.getByTitle('Start a new rig from bench'));
    fireEvent.change(screen.getByLabelText('New rig name'), {
      target: { value: 'experiment' },
    });
    fireEvent.click(screen.getByText('Create'));

    await waitFor(() => expect(sent.length).toBe(1));
    expect(sent[0].url).toBe('/api/rigs/new');
    expect(JSON.parse(sent[0].body)).toMatchObject({
      name: 'experiment', from: 'bench.toml',
    });
  });

  it('will not create a rig with no name', async () => {
    await open();
    fireEvent.click(screen.getByText('New rig'));
    expect((screen.getByText('Create') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('New rig name'), { target: { value: '  ' } });
    expect((screen.getByText('Create') as HTMLButtonElement).disabled).toBe(true);
  });

  it('asks before deleting, because there is no copy of a rig', async () => {
    await open();
    fireEvent.click(screen.getByLabelText('Remove bench'));
    expect(window.confirm).toHaveBeenCalled();
    await waitFor(() => expect(sent.length).toBe(1));
    expect(sent[0].url).toBe('/api/rigs/delete');
  });

  it('does not delete when the question is answered no', async () => {
    vi.stubGlobal('confirm', vi.fn(() => false));
    await open();
    fireEvent.click(screen.getByLabelText('Remove bench'));
    expect(sent.length).toBe(0);
  });

  it('will not offer to remove the last rig on the shelf', async () => {
    /* A shelf with nothing on it is a studio that will not open, recoverable
     * only by putting a file there by hand. */
    shelf = { shelf: true, current: 'only.toml', rigs: ['only.toml'] };
    render(<Rigs />);
    await waitFor(() => expect(screen.getByText('only')).toBeTruthy());
    expect((screen.getByLabelText('Remove only') as HTMLButtonElement).disabled).toBe(true);
  });

  it('offers every rig as a file to take away', async () => {
    // The machine that runs the room for real is not the machine somebody
    // sets it up on.
    await open();
    const link = screen.getAllByText('export')[1] as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/api/rigs/export?rig=bench');
    expect(link.hasAttribute('download')).toBe(true);
  });

  it('says why a refusal happened rather than failing quietly', async () => {
    refuse = 'bench.toml is already on the shelf';
    await open();
    fireEvent.click(screen.getByText('New rig'));
    fireEvent.change(screen.getByLabelText('New rig name'), { target: { value: 'bench' } });
    fireEvent.click(screen.getByText('Create'));
    await waitFor(() =>
      expect(screen.getByText('bench.toml is already on the shelf')).toBeTruthy());
  });

  it('says there is nothing to manage when the studio holds one file', async () => {
    /* Started with -rig pointing at a file. Showing an empty shelf would look
     * like the rigs had gone missing. */
    shelf = { shelf: false, current: 'demo-rig.toml', rigs: [] };
    await act(async () => { render(<Rigs />); });
    expect(screen.queryByText('New rig')).toBeNull();
    expect(screen.getByText(/single\s+file/)).toBeTruthy();
  });
});
