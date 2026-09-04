// @vitest-environment jsdom

/* The Boards page, mounted.
 *
 * The page that can move a relay onto the wrong pin, so what it sends matters
 * more than what it shows. Most of this is about the request body: that looking
 * at a board never configures it, that writing sends what the table holds, and
 * that a board's refusal reaches the person who caused it.
 *
 * The fixtures are transcripts. Every response below was copied from a real
 * exchange with a node over /api/node rather than invented, because a mocked
 * response is only worth its accuracy: an earlier admin test invented a rig
 * shape and passed for a page that showed a dash for every device.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor, act } from '@testing-library/react';
import { Nodes } from './Nodes';

/** What a freshly flashed board answers. Verbatim. */
const empty = { firmware: '0.3', chip: 'software', instruments: [] };

/** What the same board answered after being configured. Verbatim. */
const configured = {
  name: 'wind.main',
  firmware: '0.3',
  chip: 'software',
  instruments: [
    /* Deliberately not the pins blank() defaults to (18, 5 and 21). A fixture
     * whose values match the fallbacks cannot tell a number that came from the
     * board from one the page made up, and this test exists for exactly that
     * distinction. */
    { index: 0, id: 'wind.main', kind: 'wind', latencyMs: 1200,
      type: 'pwm', gpio: 19, freqHz: 18000, rampUpMs: 1800, rampDownMs: 2900, safe: 0.25 },
    { index: 1, id: 'light.strip', kind: 'light', latencyMs: 20,
      type: 'ws28xx', gpio: 27, pixels: 60 },
    { index: 2, id: 'fog.left', kind: 'fog', latencyMs: 2000,
      type: 'relay', gpio: 23, active: 'low' },
  ],
};

/** The kinds, in the order and shape /api/rig/options really sends them. */
const options = {
  kinds: [
    { kind: 'fog', drivers: ['virtual', 'cip'] },
    { kind: 'light', drivers: ['virtual', 'sacn', 'cip'] },
    { kind: 'mist', drivers: ['virtual', 'cip'] },
    { kind: 'motion', drivers: ['virtual', 'cip'] },
    { kind: 'scent', drivers: ['virtual', 'cip'] },
    { kind: 'shake', drivers: ['virtual', 'cip'] },
    { kind: 'wind', drivers: ['virtual', 'cip'] },
  ],
  modes: ['dimmer', 'rgb', 'rgbw'],
  editable: true,
};

/** Every body posted to /api/node. */
let posted: Record<string, unknown>[] = [];

/** What the next /api/node call returns. Swapped per test. */
let answer: { ok: boolean; body: unknown } = { ok: true, body: empty };

beforeEach(() => {
  posted = [];
  answer = { ok: true, body: empty };
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (url.startsWith('/api/rig/options')) {
      return { ok: true, json: async () => options } as Response;
    }
    if (url.startsWith('/api/node')) {
      posted.push(JSON.parse(init!.body as string));
      /* The real handler writes plain text on failure and JSON on success, and
       * the page reads text() either way. A mock that always returned JSON
       * would hide whether the refusal is ever shown. */
      return {
        ok: answer.ok,
        text: async () => (answer.ok ? JSON.stringify(answer.body) : String(answer.body)),
      } as Response;
    }
    return { ok: false, text: async () => 'no' } as Response;
  }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const value = (label: string) =>
  (screen.getByLabelText(label) as HTMLInputElement | HTMLSelectElement).value;

/** Fill in a board and ask it what it has. */
async function reach(secret = 'bench secret') {
  render(<Nodes />);
  fireEvent.change(screen.getByLabelText('Board address'), {
    target: { value: '127.0.0.1:5599' },
  });
  fireEvent.change(screen.getByLabelText('Shared secret'), { target: { value: secret } });
  fireEvent.click(screen.getByRole('button', { name: 'Ask what it has' }));
  await waitFor(() => expect(posted).toHaveLength(1));
}

describe('reaching a board', () => {
  it('will not ask until it has somewhere to ask', async () => {
    // Awaited because mounting fetches the kinds, and a test that returns
    // before that lands leaves React updating an unmounted tree.
    await act(async () => { render(<Nodes />); });
    expect((screen.getByRole('button', { name: 'Ask what it has' }) as HTMLButtonElement).disabled)
      .toBe(true);
  });

  it('asks without configuring', async () => {
    /* The distinction the whole endpoint turns on. A page that opened a board
     * with configure set would empty it, because the table starts empty. */
    await reach();
    expect(posted[0].configure).toBe(false);
    expect(posted[0].addr).toBe('127.0.0.1:5599');
  });

  it('says what a fresh board is, rather than showing an empty list', async () => {
    await reach();
    await waitFor(() => expect(screen.getByText(/nothing yet/)).toBeTruthy());
  });

  it('keeps the secret out of the page', async () => {
    /* Not stored server side either, but that is the server's test. Here: it
     * is never rendered back, and the field does not show it while typing. */
    await reach('hunter2');
    expect((screen.getByLabelText('Shared secret') as HTMLInputElement).type).toBe('password');
    expect(document.body.textContent).not.toContain('hunter2');
  });

  it('shows the board its own words when it refuses', async () => {
    answer = { ok: false, body: 'cip: a and b both claim gpio 18' };
    await reach();
    await waitFor(() => expect(screen.getByText(/both claim gpio 18/)).toBeTruthy());
  });
});

/** Reach a configured board and start editing what it holds. */
async function editing() {
  answer = { ok: true, body: configured };
  await reach();
  await waitFor(() => expect(screen.getByText(/wind\.main, light\.strip, fog\.left/)).toBeTruthy());
  fireEvent.click(screen.getByRole('button', { name: "Fetch the board's current configuration" }));
  await waitFor(() => expect(screen.getByLabelText('Device 1 name')).toBeTruthy());
}

describe('what is wired to it', () => {
  it('starts from what the board says, so nobody retypes it', async () => {
    await editing();
    expect(value('Device 1 name')).toBe('wind.main');
    expect(value('Device 1 kind')).toBe('wind');
    expect(value('Device 1 latency')).toBe('1200');
    expect(value('Device 3 name')).toBe('fog.left');
  });

  it('shows how the board says it is wired, not what a new row would be', async () => {
    /* The fault this exists for: a strip configured on gpio 5 read back as a
     * pwm output on gpio 18, because the page filled in everything the board
     * did not announce from the defaults for a new row. Nothing was wrong with
     * the board, and the obvious reading was that it had forgotten. */
    await editing();
    expect(value('Device 2 type')).toBe('ws28xx');
    expect(value('Device 2 gpio')).toBe('27');
    expect(value('Device 2 pixels')).toBe('60');
    expect(value('Device 1 frequency')).toBe('18000');

    expect(value('Device 1 type')).toBe('pwm');
    expect(value('Device 1 gpio')).toBe('19');
    expect(value('Device 3 type')).toBe('relay');
    expect(value('Device 3 gpio')).toBe('23');
    expect(value('Device 3 active level')).toBe('low');
  });

  it('fetching and writing back changes nothing', async () => {
    /* The reason every field has to come back. One that arrives empty is one
     * the next write sets to empty, so a fan fetched and saved without being
     * touched would lose its ramps and its safe value. */
    await editing();
    fireEvent.click(screen.getByRole('button', { name: 'Write it to the board' }));
    await waitFor(() => expect(posted).toHaveLength(2));
    const sent = posted[1] as { devices: Record<string, unknown>[] };
    const fan = sent.devices.find((d) => d.id === 'wind.main')!;
    expect(fan.rampUpMs).toBe(1800);
    expect(fan.rampDownMs).toBe(2900);
    expect(fan.safe).toBe(0.25);
    expect(fan.freqHz).toBe(18000);
    expect(fan.gpio).toBe(19);
  });

  it("does not give a strip a fan ramp time", async () => {
    /* The fields a board does not report still have to come from somewhere, and
     * the somewhere has to be that device's own type. Falling back to pwm
     * defaults puts a fan's ramp up and ramp down on a strip, and the next
     * write puts them on the board. */
    await editing();
    fireEvent.click(screen.getByRole('button', { name: 'Write it to the board' }));
    await waitFor(() => expect(posted).toHaveLength(2));
    const sent = posted[1] as { devices: Record<string, unknown>[] };
    const strip = sent.devices.find((d) => d.id === 'light.strip')!;
    expect(strip.rampUpMs).toBeUndefined();
    expect(strip.rampDownMs).toBeUndefined();
    // And the fan keeps its own, which is what makes this about type rather
    // than about dropping ramps everywhere.
    const fan = sent.devices.find((d) => d.id === 'wind.main')!;
    expect(fan.rampUpMs).toBe(1800);
  });

  it('says so when a board does not report its wiring', async () => {
    /* Older firmware announces what it carries and not how. The pins shown are
     * then guesses, and a guess presented as an answer is how somebody writes
     * gpio 18 onto a board that had a strip on 5. */
    answer = { ok: true, body: { ...configured, instruments: [
      { index: 0, id: 'wind.main', kind: 'wind', latencyMs: 1200 },
    ] } };
    await reach();
    await waitFor(() => expect(screen.getByText(/not how it is wired/)).toBeTruthy());
  });

  it('offers the kinds the server has, not a list of its own', async () => {
    /* Hard coding them here was right the day it was written and would be
     * wrong the first time a kind is added. */
    await editing();
    const select = screen.getByLabelText('Device 1 kind');
    expect([...select.querySelectorAll('option')].map((o) => o.value))
      .toEqual(options.kinds.map((k) => k.kind));
  });

  it('asks only for the settings that type has', async () => {
    await editing();
    expect(screen.getByLabelText('Device 1 frequency')).toBeTruthy();
    expect(screen.queryByLabelText('Device 1 pixels')).toBeNull();

    fireEvent.change(screen.getByLabelText('Device 1 type'), { target: { value: 'ws28xx' } });
    expect(screen.getByLabelText('Device 1 pixels')).toBeTruthy();
    expect(screen.queryByLabelText('Device 1 frequency')).toBeNull();

    fireEvent.change(screen.getByLabelText('Device 1 type'), { target: { value: 'relay' } });
    expect(screen.getByLabelText('Device 1 active level')).toBeTruthy();
    expect(screen.queryByLabelText('Device 1 pixels')).toBeNull();
  });

  it('keeps the name and pin when the type changes', async () => {
    /* Changing the type does have to reset the settings, since a strip's pixel
     * count means nothing to a relay. What somebody typed is not a setting. */
    await editing();
    fireEvent.change(screen.getByLabelText('Device 1 gpio'), { target: { value: '27' } });
    fireEvent.change(screen.getByLabelText('Device 1 type'), { target: { value: 'relay' } });
    expect(value('Device 1 name')).toBe('wind.main');
    expect(value('Device 1 gpio')).toBe('27');
  });

  it('adds and removes', async () => {
    await editing();
    fireEvent.click(screen.getByRole('button', { name: 'Add a device' }));
    expect(screen.getByLabelText('Device 4 name')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Remove fog.left' }));
    expect(screen.queryByLabelText('Device 4 name')).toBeNull();
  });

  it('writes the table to the board', async () => {
    await editing();
    fireEvent.change(screen.getByLabelText('Device 2 latency'), { target: { value: '40' } });
    fireEvent.click(screen.getByRole('button', { name: 'Write it to the board' }));
    await waitFor(() => expect(posted).toHaveLength(2));

    const sent = posted[1] as { configure: boolean; devices: Record<string, unknown>[] };
    expect(sent.configure).toBe(true);
    expect(sent.devices).toHaveLength(3);
    expect(sent.devices.map((d) => d.id)).toEqual(['wind.main', 'light.strip', 'fog.left']);
    expect(sent.devices[1].latencyMs).toBe(40);
  });

  it('says the board took it, and stops saying so once it is edited again', async () => {
    /* The difference between what is on the board and what is on the screen is
     * the one thing this page cannot show directly, so it must not claim they
     * agree when they do not. */
    await editing();
    fireEvent.click(screen.getByRole('button', { name: 'Write it to the board' }));
    await waitFor(() => expect(screen.getByText(/the board took it/)).toBeTruthy());
    // A pin it is not already on, or no change event fires and the test passes
    // for a reason that has nothing to do with what it is checking.
    fireEvent.change(screen.getByLabelText('Device 1 gpio'), { target: { value: '26' } });
    expect(screen.queryByText(/the board took it/)).toBeNull();
  });

  it('does not claim a refused configuration was taken', async () => {
    await editing();
    answer = { ok: false, body: 'cip: gpio 6 is spi flash on this chip' };
    fireEvent.click(screen.getByRole('button', { name: 'Write it to the board' }));
    await waitFor(() => expect(screen.getByText(/spi flash/)).toBeTruthy());
    expect(screen.queryByText(/the board took it/)).toBeNull();
  });
});
