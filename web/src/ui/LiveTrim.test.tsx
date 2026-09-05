// @vitest-environment jsdom

/* The two knobs, from the operator's side.
 *
 * Worth testing here rather than only on the server, because every way this
 * could be annoying is in the browser: a handle that snaps back mid drag, a
 * slider that resets its neighbour, a value that only takes effect after a
 * re-arm. The arithmetic is tested in Go; this is about the knob.
 */

import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { LiveTrim } from './LiveTrim';

let sent: any[] = [];

beforeEach(() => {
  sent = [];
  vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => {
    if (!init || init.method !== 'POST') {
      return Promise.resolve({
        ok: true, json: () => Promise.resolve({ brightness: 0, saturation: 0 }),
      } as Response);
    }
    sent.push(JSON.parse(String(init.body)));
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}) } as Response);
  }));
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const bright = () => screen.getByLabelText('Brightness trim, percent') as HTMLInputElement;
const colour = () => screen.getByLabelText('Saturation trim, percent') as HTMLInputElement;

describe('the live colour trim', () => {
  it('starts where the server left it, not at zero', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: true, json: () => Promise.resolve({ brightness: -20, saturation: 45 }),
    } as Response)));
    render(<LiveTrim />);
    // The setting somebody spent ten minutes finding survives a disarm, so the
    // page has to ask rather than assume.
    await waitFor(() => expect(bright().value).toBe('-20'));
    expect(colour().value).toBe('45');
  });

  it('sends what was moved and leaves the other alone', async () => {
    render(<LiveTrim />);
    await waitFor(() => expect(bright()).toBeTruthy());

    fireEvent.change(colour(), { target: { value: '40' } });
    await vi.advanceTimersByTimeAsync(200);

    expect(sent.at(-1)).toEqual({ brightness: 0, saturation: 40 });
    expect(bright().value).toBe('0');
  });

  it('does not flood the server while a handle is being dragged', async () => {
    render(<LiveTrim />);
    await waitFor(() => expect(colour()).toBeTruthy());

    // A drag is dozens of change events. The server needs the last one, not
    // all of them, and a request per frame over wifi is how a slider gets
    // laggy enough to be unusable.
    for (let v = 1; v <= 30; v += 1) {
      fireEvent.change(colour(), { target: { value: String(v) } });
    }
    await vi.advanceTimersByTimeAsync(200);

    expect(sent.length).toBeLessThan(5);
    expect(sent.at(-1)).toEqual({ brightness: 0, saturation: 30 });
    // And the handle followed the finger the whole way, rather than waiting.
    expect(colour().value).toBe('30');
  });

  it('goes back to the score as written', async () => {
    render(<LiveTrim />);
    await waitFor(() => expect(bright()).toBeTruthy());

    fireEvent.change(bright(), { target: { value: '70' } });
    await vi.advanceTimersByTimeAsync(200);
    fireEvent.click(screen.getByText('reset'));
    await vi.advanceTimersByTimeAsync(200);

    expect(sent.at(-1)).toEqual({ brightness: 0, saturation: 0 });
    expect(bright().value).toBe('0');
  });

  it('says nothing is being changed until something is', async () => {
    render(<LiveTrim />);
    await waitFor(() => expect(bright()).toBeTruthy());
    // Reset is the tell: greyed while the room is doing exactly what the score
    // says, live the moment it is not.
    const reset = () => screen.getByText('reset') as HTMLButtonElement;
    expect(reset().disabled).toBe(true);

    fireEvent.change(bright(), { target: { value: '5' } });
    await waitFor(() => expect(reset().disabled).toBe(false));
  });

  it('holds its range at a hundred each way', async () => {
    render(<LiveTrim />);
    await waitFor(() => expect(bright()).toBeTruthy());
    for (const el of [bright(), colour()]) {
      expect(el.min).toBe('-100');
      expect(el.max).toBe('100');
    }
  });
});
