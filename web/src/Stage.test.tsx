// @vitest-environment jsdom

/* The stage: which film opens, what is on it, and remembering an arrangement.
 *
 * The first test here is a regression. The studio opens holding a score and no
 * film, and nothing connected the two — so the picture pane showed its "pick a
 * film" hint over a score plainly already about one particular film, and there
 * was no video element on the page at all. Every unit test passed throughout:
 * the score loaded, the media list loaded, and the two were simply never
 * introduced.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { App } from './App';

const score = {
  title: 'Sintel',
  duration: 120,
  fps: 24,
  /* The binding between a score and its film is the filename and nothing
   * else, so the path is the whole point of this fixture. */
  path: '/scores/sintel.componium',
  tracks: [
    {
      instrument: 'light.ambient', type: 'curve',
      points: [{ t: 0, value: { r: 0, g: 0, b: 0 } }, { t: 20, value: { r: 1, g: 0, b: 0 } }],
    },
  ],
};

const rig = { name: 'test', instruments: [{ id: 'wind.main', kind: 'wind', latency: 0 }] };
const media = [{ name: 'big-buck-bunny.mp4', size: 10 }, { name: 'sintel.mp4', size: 100 }];

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const body = url.startsWith('/api/score') ? score
      : url.startsWith('/api/rig') ? rig
        : url.startsWith('/api/media') ? media
          : {};
    return { ok: true, json: async () => body, text: async () => JSON.stringify(body) } as Response;
  }));
  Object.defineProperty(HTMLMediaElement.prototype, 'duration', {
    configurable: true, get: () => 120,
  });
  HTMLMediaElement.prototype.play = vi.fn(async () => {});
  HTMLMediaElement.prototype.pause = vi.fn();
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

async function open() {
  render(<App />);
  await screen.findByText(/Componium/);
  await waitFor(() => {
    const s = document.querySelector('select') as HTMLSelectElement;
    if (!s || s.options.length < 2) throw new Error('films not loaded');
  });
}

const stage = () => document.querySelector('.stage') as HTMLElement;
const button = (label: string) =>
  Array.from(document.querySelectorAll('button'))
    .find((b) => b.textContent?.trim().toLowerCase().startsWith(label)) as HTMLButtonElement;

describe('opening a film', () => {
  it('opens the film the score was made from, with no clicking', async () => {
    await open();
    const video = await waitFor(() => {
      const v = document.querySelector('video');
      if (!v) throw new Error('no video yet');
      return v;
    });
    expect(video.getAttribute('src')).toContain('sintel.mp4');
  });

  it('shows that film as the one selected in the picker', async () => {
    await open();
    await waitFor(() => {
      expect((document.querySelector('select') as HTMLSelectElement).value).toBe('sintel.mp4');
    });
  });

  it('leaves the picker alone when no film matches the score', async () => {
    /* A score whose film has been deleted must not silently open a different
     * one — picking the nearest film would scrub the timeline against the
     * wrong picture, which is worse than showing no picture. */
    score.path = '/scores/deleted.componium';
    await open();
    expect((document.querySelector('select') as HTMLSelectElement).value).toBe('');
    expect(document.querySelector('video')).toBeNull();
    score.path = '/scores/sintel.componium';
  });
});

describe('the sliders toggle', () => {
  it('starts showing the force sliders', async () => {
    await open();
    await waitFor(() => expect(document.querySelector('.force')).not.toBeNull());
  });

  it('hides them, giving the room the space', async () => {
    await open();
    await waitFor(() => expect(document.querySelector('.force')).not.toBeNull());
    fireEvent.click(button('sliders'));
    expect(document.querySelector('.force')).toBeNull();
    /* The room itself must still be there — hiding the sliders is about space
     * for the room, so taking the room too would defeat the purpose. */
    expect(document.querySelector('.stage-room')).not.toBeNull();
  });

  it('is disabled while the room is hidden, because that is where they live', async () => {
    await open();
    fireEvent.click(button('room'));
    expect(document.querySelector('.stage-room')).toBeNull();
    expect(button('sliders').disabled).toBe(true);
  });

  it('remembers being hidden across a reload', async () => {
    await open();
    fireEvent.click(button('sliders'));
    cleanup();
    await open();
    await waitFor(() => expect(document.querySelector('.stage-room')).not.toBeNull());
    expect(document.querySelector('.force')).toBeNull();
  });
});

describe('saved viewports', () => {
  const panel = () => document.querySelector('.views-panel') as HTMLElement;

  async function saveAs(name: string) {
    fireEvent.click(button('views'));
    const input = within(panel()).getByLabelText('Name this arrangement');
    fireEvent.change(input, { target: { value: name } });
    fireEvent.click(within(panel()).getByText('Save'));
  }

  it('saves the current arrangement under a name', async () => {
    await open();
    fireEvent.click(button('sliders'));
    await saveAs('big room');
    expect(within(panel()).getByText('big room')).toBeTruthy();
  });

  it('puts the arrangement back when the name is clicked', async () => {
    await open();
    fireEvent.click(button('sliders'));
    await saveAs('big room');
    /* Change the stage after saving, so recalling has something to undo. */
    fireEvent.click(within(panel()).getByText('big room'));
    fireEvent.click(button('sliders'));
    expect(document.querySelector('.force')).not.toBeNull();

    fireEvent.click(button('views'));
    fireEvent.click(within(panel()).getByText('big room'));
    expect(document.querySelector('.force')).toBeNull();
  });

  it('deletes one', async () => {
    await open();
    await saveAs('gone');
    fireEvent.click(within(panel()).getByLabelText('Delete gone'));
    expect(within(panel()).queryByText('gone')).toBeNull();
  });

  it('survives a reload', async () => {
    await open();
    await saveAs('kept');
    cleanup();
    await open();
    fireEvent.click(button('views'));
    expect(within(panel()).getByText('kept')).toBeTruthy();
  });

  it('refuses a nameless arrangement rather than storing a blank row', async () => {
    await open();
    fireEvent.click(button('views'));
    const save = within(panel()).getByText('Save') as HTMLButtonElement;
    expect(save.disabled).toBe(true);
  });

  it('offers to replace rather than duplicate an existing name', async () => {
    await open();
    await saveAs('same');
    /* saveAs leaves the panel open — it only clears the name — so this must
     * not click "views" again, which would close it. */
    const input = within(panel()).getByLabelText('Name this arrangement');
    fireEvent.change(input, { target: { value: 'same' } });
    expect(within(panel()).getByText('Replace')).toBeTruthy();
  });

  it('resets the stage to the default arrangement', async () => {
    await open();
    fireEvent.click(button('sliders'));
    fireEvent.click(button('views'));
    fireEvent.click(within(panel()).getByText('Reset to default'));
    expect(document.querySelector('.force')).not.toBeNull();
    expect(stage().style.gridTemplateColumns).toContain('6fr');
    expect(stage().style.height).toBe('300px');
  });
});
