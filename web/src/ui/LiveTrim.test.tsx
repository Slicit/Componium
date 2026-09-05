// @vitest-environment jsdom

/* The knobs, from the operator's side.
 *
 * Worth testing here rather than only on the server, because every way this
 * could be annoying is in the browser: a handle that snaps back mid drag, a
 * slider that drags its neighbour with it, a request per frame over wifi. The
 * arithmetic is tested in Go; this is about the knob.
 */

import { render, screen, waitFor, fireEvent, cleanup, act } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { LiveTrim } from './LiveTrim';

const LIGHTS = ['light.ambient', 'light.event'];

let sent: any[] = [];
let held: Record<string, unknown> = {};

beforeEach(() => {
  sent = [];
  held = {};
  vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => {
    if (!init || init.method !== 'POST') {
      return Promise.resolve({
        ok: true, json: () => Promise.resolve({ trim: held }),
      } as Response);
    }
    sent.push(JSON.parse(String(init.body)));
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}) } as Response);
  }));
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

/** Open the panel, which is shut until somebody wants it. */
async function open(lights = LIGHTS) {
  render(<LiveTrim lights={lights} />);
  await waitFor(() => expect(screen.getByText(/^trim/)).toBeTruthy());
  fireEvent.click(screen.getByText(/^trim/));
}

const slider = (kind: string, id: string) =>
  screen.getByLabelText(kind + ' trim for ' + id + ', percent') as HTMLInputElement;

describe('the live colour trim', () => {
  it('offers a pair of knobs for each light and none for anything else', async () => {
    await open();
    for (const id of LIGHTS) {
      expect(slider('Brightness', id)).toBeTruthy();
      expect(slider('Saturation', id)).toBeTruthy();
    }
    // A rig of foggers and fans has nothing here to adjust, so the button is
    // not offered at all rather than opening onto an empty panel.
    cleanup();
    // Flushed, because the component still asks the server what it holds
    // before deciding it has nothing to show, and a state update landing
    // after the test body is how a suite starts warning and later flaking.
    await act(async () => { render(<LiveTrim lights={[]} />); });
    expect(screen.queryByText(/^trim/)).toBeNull();
  });

  it('sends the instrument that was moved, and only that one', async () => {
    await open();
    fireEvent.change(slider('Saturation', 'light.ambient'), { target: { value: '40' } });
    await vi.advanceTimersByTimeAsync(200);

    expect(sent).toEqual([
      { instrument: 'light.ambient', brightness: 0, saturation: 40 },
    ]);
    // The neighbour did not move with it, which is the whole reason this is
    // per instrument.
    expect(slider('Saturation', 'light.event').value).toBe('0');
  });

  it('keeps each light on its own numbers', async () => {
    await open();
    fireEvent.change(slider('Saturation', 'light.ambient'), { target: { value: '55' } });
    fireEvent.change(slider('Brightness', 'light.event'), { target: { value: '-30' } });
    await vi.advanceTimersByTimeAsync(200);

    expect(slider('Saturation', 'light.ambient').value).toBe('55');
    expect(slider('Brightness', 'light.ambient').value).toBe('0');
    expect(slider('Brightness', 'light.event').value).toBe('-30');
    expect(slider('Saturation', 'light.event').value).toBe('0');
  });

  it('starts where the server left it, not at zero', async () => {
    // The setting somebody spent ten minutes finding survives a disarm, so the
    // page has to ask rather than assume.
    held = { 'light.event': { brightness: -20, saturation: 45 } };
    await open();
    expect(slider('Brightness', 'light.event').value).toBe('-20');
    expect(slider('Saturation', 'light.event').value).toBe('45');
    expect(slider('Saturation', 'light.ambient').value).toBe('0');
  });

  it('does not flood the server while a handle is being dragged', async () => {
    await open();
    // A drag is dozens of change events. The server needs the last one, not
    // all of them: a request per frame over wifi is how a slider gets laggy
    // enough to be unusable.
    for (let v = 1; v <= 30; v += 1) {
      fireEvent.change(slider('Saturation', 'light.ambient'), { target: { value: String(v) } });
    }
    await vi.advanceTimersByTimeAsync(200);

    expect(sent.length).toBeLessThan(5);
    expect(sent.at(-1)).toEqual(
      { instrument: 'light.ambient', brightness: 0, saturation: 30 });
    // And the handle followed the finger the whole way rather than waiting.
    expect(slider('Saturation', 'light.ambient').value).toBe('30');
  });

  it('puts one light back without touching the other', async () => {
    await open();
    fireEvent.change(slider('Brightness', 'light.ambient'), { target: { value: '70' } });
    fireEvent.change(slider('Brightness', 'light.event'), { target: { value: '20' } });
    await vi.advanceTimersByTimeAsync(200);

    fireEvent.click(screen.getByLabelText('Reset trim for light.ambient'));
    await vi.advanceTimersByTimeAsync(200);

    expect(sent.at(-1)).toEqual(
      { instrument: 'light.ambient', brightness: 0, saturation: 0 });
    expect(slider('Brightness', 'light.ambient').value).toBe('0');
    expect(slider('Brightness', 'light.event').value).toBe('20');
  });

  it('says on the closed button that something is being changed', async () => {
    /* The panel spends most of its life shut, and a room quietly running two
     * stops brighter than the score says is worth knowing about without
     * opening anything. */
    render(<LiveTrim lights={LIGHTS} />);
    await waitFor(() => expect(screen.getByText('trim')).toBeTruthy());

    fireEvent.click(screen.getByText('trim'));
    fireEvent.change(slider('Saturation', 'light.event'), { target: { value: '35' } });
    await waitFor(() => expect(screen.getByText('trim •')).toBeTruthy());
  });

  it('holds its range at a hundred each way', async () => {
    await open();
    for (const id of LIGHTS) {
      for (const el of [slider('Brightness', id), slider('Saturation', id)]) {
        expect(el.min).toBe('-100');
        expect(el.max).toBe('100');
      }
    }
  });
});
