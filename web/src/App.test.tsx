// @vitest-environment jsdom

/* The wiring between the film, the playhead and the timeline.
 *
 * These exist because of a bug the rest of the suite could not see. The
 * playhead followed the film through an effect that reached for the video
 * element and called addEventListener — but the video element does not exist
 * until a film is picked, which happens after that effect has already run and
 * returned early. So the listener was never attached, the playhead stopped
 * following the picture, and every other test still passed: the core was
 * correct, the renderer was correct, and the two were simply not connected.
 *
 * That class of fault is invisible to a unit test of a pure function and to a
 * draw-list assertion. It needs the component actually mounted.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { App } from './App';

const score = {
  title: 'Demo',
  duration: 120,
  fps: 24,
  tracks: [
    {
      instrument: 'wind.main', type: 'cue',
      cues: [{ t: 10, action: 'gust', params: { intensity: 0.5 }, duration: 4 }],
    },
    {
      instrument: 'light.ambient', type: 'curve',
      points: [
        { t: 0, value: { r: 0, g: 0, b: 0 } },
        { t: 20, value: { r: 1, g: 0.5, b: 0 } },
      ],
    },
  ],
};

const rig = { name: 'test', instruments: [{ id: 'wind.main', kind: 'wind', latency: 0 }] };
const media = [{ name: 'sintel.mp4', size: 100 }];

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const body = url.startsWith('/api/score') ? score
      : url.startsWith('/api/rig') ? rig
        : url.startsWith('/api/media') ? media
          : {};
    return { ok: true, json: async () => body, text: async () => JSON.stringify(body) } as Response;
  }));
  /* jsdom has no media pipeline: currentTime is a plain property and duration
   * is NaN unless we say otherwise. Both matter — the code checks duration is
   * finite before touching the element. */
  Object.defineProperty(HTMLMediaElement.prototype, 'duration', {
    configurable: true, get: () => 120,
  });
  HTMLMediaElement.prototype.play = vi.fn(async () => {});
  HTMLMediaElement.prototype.pause = vi.fn();
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const timecode = () => document.querySelector('.tc')!.textContent;

async function openFilm() {
  render(<App />);
  await screen.findByText(/Componium/);
  const picker = await waitFor(() => {
    const s = document.querySelector('select') as HTMLSelectElement;
    if (!s || s.options.length < 2) throw new Error('films not loaded');
    return s;
  });
  fireEvent.change(picker, { target: { value: 'sintel.mp4' } });
  return await waitFor(() => {
    const v = document.querySelector('[data-testid="film"]') as HTMLVideoElement;
    if (!v) throw new Error('no video');
    return v;
  });
}

describe('the playhead follows the film', () => {
  it('shows a film once one is picked', async () => {
    const video = await openFilm();
    expect(video.getAttribute('src')).toContain('sintel.mp4');
  });

  /* The regression. A timeupdate from the element must move the clock — which
   * requires the handler to be attached to the element that actually mounted,
   * not to whatever was there when an effect first ran. */
  it('advances the timecode when the film reports a new time', async () => {
    const video = await openFilm();
    expect(timecode()).toBe('00:00:00:00');

    video.currentTime = 61.5;
    fireEvent.timeUpdate(video);

    await waitFor(() => expect(timecode()).toBe('00:01:01:12'));
  });

  it('keeps following on the next update, not only the first', async () => {
    const video = await openFilm();
    for (const [at, want] of [[10, '00:00:10:00'], [20.5, '00:00:20:12'], [3, '00:00:03:00']] as const) {
      video.currentTime = at;
      fireEvent.timeUpdate(video);
      await waitFor(() => expect(timecode()).toBe(want));
    }
  });

  /* Scrubbing the element's own controls fires seeked rather than timeupdate
   * while paused, and the playhead has to keep up with that too. */
  it('follows a seek made in the film"s own controls', async () => {
    const video = await openFilm();
    video.currentTime = 42;
    fireEvent.seeked(video);
    await waitFor(() => expect(timecode()).toBe('00:00:42:00'));
  });

  it('starts from wherever the film opens', async () => {
    const video = await openFilm();
    video.currentTime = 5;
    fireEvent.loadedMetadata(video);
    await waitFor(() => expect(timecode()).toBe('00:00:05:00'));
  });
});

describe('the timeline drives the film', () => {
  /* The other direction: a keystroke moves the playhead, and the picture has
   * to go with it or the two are showing different moments. */
  it('moves the film when the playhead is stepped a frame', async () => {
    const video = await openFilm();
    fireEvent.keyDown(window, { key: 'ArrowRight' });
    await waitFor(() => expect(video.currentTime).toBeCloseTo(1 / 24, 6));
    expect(timecode()).toBe('00:00:00:01');
  });

  it('moves it a second with shift', async () => {
    const video = await openFilm();
    fireEvent.keyDown(window, { key: 'ArrowRight', shiftKey: true });
    await waitFor(() => expect(video.currentTime).toBeCloseTo(1, 6));
  });

  it('goes to the end and stays inside the film', async () => {
    const video = await openFilm();
    fireEvent.keyDown(window, { key: 'End' });
    await waitFor(() => expect(video.currentTime).toBe(120));
    fireEvent.keyDown(window, { key: 'ArrowRight' });
    await waitFor(() => expect(video.currentTime).toBe(120));
  });
});

describe('the timeline itself', () => {
  it('draws a lane per instrument, with the colour track expanded', async () => {
    render(<App />);
    await waitFor(() => {
      const names = Array.from(document.querySelectorAll('.tl-name')).map((n) => n.textContent);
      expect(names).toEqual(['wind.main', 'light.ambient']);
    });
    const channels = Array.from(document.querySelectorAll('.tl-chan')).map((n) => n.textContent);
    expect(channels).toEqual(['r', 'g', 'b']);
  });

  it('offers no chevron for a track with one channel', async () => {
    render(<App />);
    await waitFor(() => expect(document.querySelectorAll('.tl-name').length).toBe(2));
    /* wind.main is a cue track and light.ambient has three channels, so
     * exactly one chevron and one gap. */
    expect(document.querySelectorAll('.tl-chev').length).toBe(1);
    expect(document.querySelectorAll('.tl-chev-gap').length).toBe(1);
  });

  it('starts with nothing to undo and nothing to save', async () => {
    render(<App />);
    const undo = await screen.findByRole('button', { name: 'Undo' }) as HTMLButtonElement;
    expect(undo.disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Saved' }) as HTMLButtonElement).disabled).toBe(true);
  });
});

describe('the inspector', () => {
  /* Dragging finds a shape; a typed field pins it down. Both belong in an
   * editor, and neither should need a mode to be chosen first. */
  it('opens on a plain click and shows the values', async () => {
    render(<App />);
    await waitFor(() => expect(document.querySelectorAll('.tl-name').length).toBe(2));

    const surface = document.querySelector('.tl-surface') as HTMLElement;
    Object.defineProperty(surface, 'clientWidth', { configurable: true, value: 1000 });
    surface.getBoundingClientRect = () => ({
      left: 0, top: 0, width: 1000, height: 300, right: 1000, bottom: 300, x: 0, y: 0, toJSON: () => ({}),
    });

    /* The gust runs 10s to 14s of a 120s film, on the first lane, which the
     * default view shows a tenth of — so fit first, then aim at 12s. */
    fireEvent.keyDown(window, { key: 'f' });
    fireEvent.pointerDown(surface, { clientX: 100, clientY: 26 + 20, button: 0 });
    fireEvent.pointerUp(window, { clientX: 100, clientY: 26 + 20 });

    await waitFor(() => expect(document.querySelector('.insp')).not.toBeNull());
    expect(document.querySelector('.insp-what')!.textContent).toBe('wind.main');
    const labels = Array.from(document.querySelectorAll('.insp-label')).map((l) => l.textContent);
    expect(labels).toContain('Time');
    expect(labels).toContain('Length');
    expect(labels).toContain('intensity');
  });

  it('is there before anything is selected', async () => {
    // The whole reason it moved out of the corner. A pane that appears only
    // when there is something in it is a pane you wait for, and the lanes
    // beside it change width every time you click.
    render(<App />);
    await waitFor(() => expect(document.querySelectorAll('.tl-name').length).toBe(2));
    expect(document.querySelector('.insp.is-empty')).not.toBeNull();
    // And it is beside the lanes rather than over them.
    expect(document.querySelector('.tl-split .insp')).not.toBeNull();
    expect(document.querySelector('.tl-split .tl-lanes')).not.toBeNull();
  });

  it('empties, and stays where it is', async () => {
    render(<App />);
    await waitFor(() => expect(document.querySelectorAll('.tl-name').length).toBe(2));
    const surface = document.querySelector('.tl-surface') as HTMLElement;
    Object.defineProperty(surface, 'clientWidth', { configurable: true, value: 1000 });
    surface.getBoundingClientRect = () => ({
      left: 0, top: 0, width: 1000, height: 300, right: 1000, bottom: 300, x: 0, y: 0, toJSON: () => ({}),
    });
    fireEvent.keyDown(window, { key: 'f' });
    fireEvent.pointerDown(surface, { clientX: 100, clientY: 46, button: 0 });
    fireEvent.pointerUp(window, { clientX: 100, clientY: 46 });
    await waitFor(() => expect(document.querySelector('.insp')).not.toBeNull());

    fireEvent.click(document.querySelector('.insp-close')!);
    /* The pane stays. It is a place to edit rather than something that appears
     * when there is editing to do — if it came and went, the lanes beside it
     * would change width on every click. What the button clears is the
     * selection, and empty is a state the pane draws. */
    await waitFor(() => expect(document.querySelector('.insp.is-empty')).not.toBeNull());
    expect(document.querySelector('.insp')).not.toBeNull();
    expect(document.querySelector('.insp-close')).toBeNull();
  });
});

describe('arranging the tracks', () => {
  it('reorders by dragging one group onto another', async () => {
    render(<App />);
    await waitFor(() => expect(document.querySelectorAll('.tl-name').length).toBe(2));
    const names = () => Array.from(document.querySelectorAll('.tl-name')).map((n) => n.textContent);
    expect(names()).toEqual(['wind.main', 'light.ambient']);

    const heads = document.querySelectorAll('.tl-head.is-head');
    const wind = heads[0];
    const light = heads[1];
    const data = { effectAllowed: '', dropEffect: '', setData: () => {}, getData: () => 'wind.main' };

    fireEvent.dragStart(wind, { dataTransfer: data });
    fireEvent.dragOver(light, { dataTransfer: data });
    fireEvent.drop(light, { dataTransfer: data });

    await waitFor(() => expect(names()).toEqual(['light.ambient', 'wind.main']));
  });

  it('saves the arrangement beside the score rather than in it', async () => {
    render(<App />);
    await waitFor(() => expect(document.querySelectorAll('.tl-name').length).toBe(2));
    const heads = document.querySelectorAll('.tl-head.is-head');
    const data = { effectAllowed: '', dropEffect: '', setData: () => {}, getData: () => 'wind.main' };
    fireEvent.dragStart(heads[0], { dataTransfer: data });
    fireEvent.drop(heads[1], { dataTransfer: data });

    await waitFor(() => {
      const puts = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls
        .filter((c) => c[0] === '/api/layout' && (c[1] as RequestInit | undefined)?.method === 'PUT');
      expect(puts.length).toBeGreaterThan(0);
      const body = JSON.parse((puts[0][1] as RequestInit).body as string);
      expect(body.order).toEqual(['light.ambient', 'wind.main']);
    }, { timeout: 2000 });
  });
});
