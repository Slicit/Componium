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

const rig = {
  name: 'bench',
  instruments: [
    { id: 'light.ambient', kind: 'light', driver: 'sacn', universe: 1, latency: 0.02 },
    { id: 'wind.main', kind: 'wind', driver: 'cip', addr: '192.168.1.90:5570', latency: 1.2 },
    { id: 'fog.left', kind: 'fog', driver: 'virtual' },
  ],
};

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url.startsWith('/api/rig')) {
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

describe('devices', () => {
  it('shows what the rig says, and which of it is real', async () => {
    show('#/admin/devices');
    await waitFor(() => expect(screen.getByText('light.ambient')).toBeTruthy());
    expect(screen.getByText('192.168.1.90:5570')).toBeTruthy();
    expect(screen.getByText('universe 1')).toBeTruthy();
    /* The count is the useful sentence during bring up: two of these will
     * move something and one will only write a log line. */
    expect(screen.getByText(/2 on real hardware/)).toBeTruthy();
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
