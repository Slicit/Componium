// @vitest-environment jsdom

/* The admin section, mounted.
 *
 * Navigation is the kind of thing that typechecks perfectly and goes nowhere:
 * a menu of links whose hashes nothing reads, a page that renders under every
 * route, a "current" class that is never applied. None of that is visible to a
 * test of the route functions on their own, so this mounts the section and
 * clicks about in it.
 *
 * The studio itself is not mounted here. It fetches a score, a rig, a media
 * list and a job queue on the way up, and none of that is what this is about.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { Admin, PAGES } from './Admin';
import { Nav } from '../Nav';
import { parseRoute } from '../../core/route';

/* The shape the server actually sends, field for field.
 *
 * Worth saying because the first version of this file invented one. The page
 * rendered an address and a universe that `/api/rig` had never carried, the
 * test passed against the fixture, and every device in the real studio showed
 * a dash. A mocked response is only worth what its accuracy is worth. */
const rig = {
  name: 'bench',
  editable: true,
  instruments: [
    { id: 'light.ambient', kind: 'light', driver: 'sacn', addr: '192.168.1.90:5568',
      universe: 1, start: 1, mode: 'rgb', latency: 0.02, position: [0, 1.4, -0.1] },
    { id: 'wind.main', kind: 'wind', driver: 'cip', addr: '192.168.1.91:5570',
      latency: 1.2, position: [0, 1.6, 0.6] },
    { id: 'fog.left', kind: 'fog', driver: 'virtual', latency: 0, position: [-1.6, 0.15, 1] },
  ],
};

const options = {
  kinds: [
    { kind: 'fog', drivers: ['virtual', 'cip'] },
    { kind: 'light', drivers: ['virtual', 'sacn', 'cip'] },
    { kind: 'wind', drivers: ['virtual', 'cip'] },
  ],
  modes: ['dimmer', 'rgb', 'rgbw'],
  editable: true,
};

/** Every rig PUT the page made. */
const saves: unknown[] = [];

beforeEach(() => {
  localStorage.clear();
  saves.length = 0;
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/rig/options')) {
      return { ok: true, json: async () => options } as Response;
    }
    if (url.startsWith('/api/rig')) {
      if (init?.method === 'PUT') {
        saves.push(JSON.parse(init.body as string));
        return { ok: true, json: async () => ({ saved: true }) } as Response;
      }
      return { ok: true, json: async () => rig } as Response;
    }
    if (url.startsWith('/api/firmware')) {
      return { ok: true, json: async () => ({ available: false, why: 'not built here' }) } as Response;
    }
    return { ok: false, json: async () => ({}) } as Response;
  }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const show = (hash: string) => render(<Admin route={parseRoute(hash)} />);

describe('the side menu', () => {
  it('lists every page', () => {
    show('#/admin');
    for (const p of PAGES) expect(screen.getByRole('link', { name: p.label })).toBeTruthy();
  });

  it('marks the page you are on, and only that one', () => {
    show('#/admin/firmware');
    const current = screen.getAllByRole('link').filter((a) => a.classList.contains('is-current'));
    expect(current.map((a) => a.textContent)).toEqual(['Firmware']);
  });

  it('links somewhere the router can read back', () => {
    show('#/admin');
    const link = screen.getByRole('link', { name: 'Firmware' }) as HTMLAnchorElement;
    expect(parseRoute(link.getAttribute('href')!)).toEqual({ section: 'admin', page: 'firmware' });
  });

  it('opens the first page when the hash names none', () => {
    show('#/admin');
    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe('Devices');
  });

  it('opens the first page rather than failing on a hash nobody recognises', () => {
    /* A hash is a thing people edit and paste half of. Landing somewhere
     * useful beats an empty panel. */
    show('#/admin/nonsense');
    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe('Devices');
  });

  it('shows one page at a time', () => {
    show('#/admin/room');
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe('Room preview');
  });
});

async function devices() {
  show('#/admin/devices');
  await waitFor(() => expect(screen.getByLabelText('Instrument 1 id')).toBeTruthy());
}

const value = (label: string) =>
  (screen.getByLabelText(label) as HTMLInputElement | HTMLSelectElement).value;

describe('devices', () => {
  it('shows what the rig says, and which of it is real', async () => {
    await devices();
    expect(value('Instrument 1 id')).toBe('light.ambient');
    expect(value('Instrument 1 address')).toBe('192.168.1.90:5568');
    expect(value('Instrument 1 universe')).toBe('1');
    expect(value('Instrument 2 address')).toBe('192.168.1.91:5570');
    /* The useful sentence during bring up: two of these will move something
     * and one will only write a log line. */
    expect(screen.getByText(/2 on real hardware/)).toBeTruthy();
  });

  it('offers only the drivers that kind can be driven by', async () => {
    await devices();
    const forFog = screen.getByLabelText('Instrument 3 driver');
    expect([...forFog.querySelectorAll('option')].map((o) => o.value))
      .toEqual(['virtual', 'cip']);
    /* sACN builds a DMX light and nothing else, so offering it here would be
     * offering a rig that will not start. */
    const forLight = screen.getByLabelText('Instrument 1 driver');
    expect([...forLight.querySelectorAll('option')].map((o) => o.value))
      .toEqual(['virtual', 'sacn', 'cip']);
  });

  it('moves a driver its kind cannot use rather than stranding it', async () => {
    await devices();
    fireEvent.change(screen.getByLabelText('Instrument 1 kind'), { target: { value: 'fog' } });
    expect(value('Instrument 1 driver')).toBe('virtual');
  });

  it('keeps a driver the new kind can still use', async () => {
    await devices();
    fireEvent.change(screen.getByLabelText('Instrument 2 kind'), { target: { value: 'fog' } });
    expect(value('Instrument 2 driver')).toBe('cip');
  });

  it('asks for an address only where there is something to reach', async () => {
    await devices();
    expect(screen.queryByLabelText('Instrument 3 address')).toBeNull();
    fireEvent.change(screen.getByLabelText('Instrument 3 driver'), { target: { value: 'cip' } });
    expect(screen.getByLabelText('Instrument 3 address')).toBeTruthy();
  });

  it('adds and removes', async () => {
    await devices();
    fireEvent.click(screen.getByRole('button', { name: 'Add a device' }));
    expect(screen.getByLabelText('Instrument 4 id')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Remove fog.left' }));
    expect(screen.queryByLabelText('Instrument 4 id')).toBeNull();
  });

  it('will not save until something changed', async () => {
    await devices();
    expect((screen.getByRole('button', { name: 'Save the rig' }) as HTMLButtonElement).disabled)
      .toBe(true);
  });

  it('sends the whole rig, including what it cannot edit', async () => {
    await devices();
    fireEvent.change(screen.getByLabelText('Instrument 2 address'),
                     { target: { value: '192.168.1.99:5570' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save the rig' }));
    await waitFor(() => expect(saves).toHaveLength(1));
    const sent = saves[0] as typeof rig;
    expect(sent.instruments).toHaveLength(3);
    expect(sent.instruments[1].addr).toBe('192.168.1.99:5570');
    /* Position is not editable here and must still make the round trip, or
     * saving would move every device to the middle of the room. */
    expect(sent.instruments[0].position).toEqual([0, 1.4, -0.1]);
  });

  it('says what was wrong rather than pretending it saved', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      if (url.startsWith('/api/rig/options')) {
        return { ok: true, json: async () => options } as Response;
      }
      if (init?.method === 'PUT') {
        return {
          ok: false,
          json: async () => ({ problems: ['wind.main: needs an address, host:port'] }),
        } as Response;
      }
      return { ok: true, json: async () => rig } as Response;
    }));
    await devices();
    fireEvent.change(screen.getByLabelText('Instrument 2 address'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save the rig' }));
    await waitFor(() =>
      expect(screen.getByText(/needs an address/)).toBeTruthy());
    expect(screen.getByText('Not saved')).toBeTruthy();
  });

  it('is read only when there is no file to write', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      if (url.startsWith('/api/rig/options')) {
        return { ok: true, json: async () => ({ ...options, editable: false }) } as Response;
      }
      return { ok: true, json: async () => ({ ...rig, editable: false }) } as Response;
    }));
    show('#/admin/devices');
    await waitFor(() => expect(screen.getByText(/Read only/)).toBeTruthy());
    expect((screen.getByLabelText('Instrument 1 id') as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByRole('button', { name: 'Save the rig' })).toBeNull();
  });

  it('says that a running show will not notice', async () => {
    /* The conductor reads the rig once, when it starts. A page that let you
     * change an address and said nothing would be lying by omission. */
    await devices();
    expect(screen.getByText(/reads the rig when it starts/)).toBeTruthy();
  });
});

describe('firmware', () => {
  it('says why there is nothing to flash rather than showing a dead button', async () => {
    show('#/admin/firmware');
    await waitFor(() => expect(screen.getByText(/not built here/)).toBeTruthy());
    expect(screen.queryByRole('button', { name: 'Install' })).toBeNull();
  });

  it('explains the secure context instead of a button that cannot work', () => {
    /* jsdom has no navigator.serial, which is exactly the state a browser is
     * in when the studio is served over plain HTTP. */
    show('#/admin/firmware');
    expect(screen.getByText(/served over plain HTTP/)).toBeTruthy();
    expect(screen.getByText(/ssh -L 8722:localhost:8722/)).toBeTruthy();
  });
});

describe('room defaults', () => {
  it('opens at the stored value and writes changes back', () => {
    localStorage.setItem('componium.roomWash', '40');
    show('#/admin/room');
    const wash = screen.getByLabelText('Ambient wash') as HTMLInputElement;
    expect(wash.value).toBe('40');
    fireEvent.change(wash, { target: { value: '60' } });
    expect(localStorage.getItem('componium.roomWash')).toBe('60');
  });

  it('can be put back', () => {
    show('#/admin/room');
    const light = screen.getByLabelText('Room light') as HTMLInputElement;
    fireEvent.change(light, { target: { value: '90' } });
    fireEvent.click(screen.getAllByRole('button', { name: /reset to 15/ })[0]);
    expect(localStorage.getItem('componium.roomLight')).toBeNull();
    expect((screen.getByLabelText('Room light') as HTMLInputElement).value).toBe('15');
  });
});

describe('the top bar', () => {
  it('offers the studio and the admin, and says which you are in', () => {
    render(<Nav route={parseRoute('#/admin/firmware')} />);
    const admin = screen.getByRole('link', { name: 'Admin' });
    expect(admin.classList.contains('is-current')).toBe(true);
    expect(screen.getByRole('link', { name: 'Studio' }).classList.contains('is-current')).toBe(false);
  });

  it('gets back to the studio with a hash the router reads as home', () => {
    render(<Nav route={parseRoute('#/admin')} />);
    const studio = screen.getByRole('link', { name: 'Studio' }) as HTMLAnchorElement;
    expect(parseRoute(studio.getAttribute('href')!)).toEqual({ section: '', page: '' });
  });
});
