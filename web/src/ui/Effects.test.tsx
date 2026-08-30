// @vitest-environment jsdom

/* Preview is an action, not a consequence of looking.
 *
 * Choosing a preset used to start it looping forever, so the room showed the
 * preview instead of the score for as long as anything was selected — and you
 * have to keep a preset selected in order to insert it, so the preview was
 * permanently in the way of seeing what you were inserting it next to.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, act } from '@testing-library/react';
import { Effects, PREVIEW_LOOPS } from './Effects';

/* rAF driven by hand: a preview counts passes off the clock, and a test that
 * waited three real seconds to watch one finish would be three seconds long. */
let now = 0;
let callbacks: FrameRequestCallback[] = [];

function tick(seconds: number) {
  now += seconds * 1000;
  act(() => {
    const due = callbacks;
    callbacks = [];
    for (const cb of due) cb(now);
  });
}

const previews: Array<[string, number | null]> = [];

beforeEach(() => {
  now = 0;
  callbacks = [];
  previews.length = 0;
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    callbacks.push(cb);
    return callbacks.length;
  });
  vi.stubGlobal('cancelAnimationFrame', () => {});
  vi.spyOn(performance, 'now').mockImplementation(() => now);
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function show(over: Partial<Parameters<typeof Effects>[0]> = {}) {
  return render(
    <Effects
      instrument="wind.main"
      kind="wind"
      at={12}
      fps={24}
      canInsert
      onInsert={() => {}}
      onPreview={(id, level) => { previews.push([id, level]); }}
      {...over}
    />,
  );
}

const pick = (name = 'Gust') => fireEvent.click(screen.getByText(name));
const play = () => screen.getByLabelText('Preview in the room');
const stopBtn = () => screen.getByLabelText('Stop the preview');

describe('choosing a preset', () => {
  it('does not start previewing on its own', () => {
    show();
    pick();
    tick(0.1);
    expect(previews).toHaveLength(0);
  });

  it('offers a play button once something is chosen', () => {
    show();
    expect(screen.queryByLabelText('Preview in the room')).toBeNull();
    pick();
    expect(play()).toBeTruthy();
  });
});

describe('previewing', () => {
  it('drives the room only once asked to', () => {
    show();
    pick();
    fireEvent.click(play());
    tick(0.1);
    expect(previews.length).toBeGreaterThan(0);
    expect(previews[0][0]).toBe('wind.main');
    expect(typeof previews[0][1]).toBe('number');
  });

  it('stops itself after the set number of passes', () => {
    show({ loops: 2 });
    pick();
    fireEvent.click(play());
    // Gust is 4s; two passes with a 0.6s gap each is 9.2s.
    for (let i = 0; i < 24; i++) tick(0.5);
    expect(screen.queryByLabelText('Stop the preview')).toBeNull();
    expect(play()).toBeTruthy();
  });

  it('hands the device back when it finishes', () => {
    show({ loops: 1 });
    pick();
    fireEvent.click(play());
    for (let i = 0; i < 16; i++) tick(0.5);
    // The last thing the room is told is to release the device, or it stays
    // driven by a level that is in no score anywhere.
    expect(previews[previews.length - 1]).toEqual(['wind.main', null]);
  });

  it('three passes by default', () => {
    expect(PREVIEW_LOOPS).toBe(3);
  });

  it('can be stopped by hand, and releases the device', () => {
    show();
    pick();
    fireEvent.click(play());
    tick(0.2);
    fireEvent.click(stopBtn());
    expect(previews[previews.length - 1]).toEqual(['wind.main', null]);
    expect(play()).toBeTruthy();
  });

  it('says which pass it is on', () => {
    show({ loops: 3 });
    pick();
    fireEvent.click(play());
    tick(0.2);
    expect(screen.getByText(/pass 1 of 3/)).toBeTruthy();
  });

  it('stops when a different preset is chosen', () => {
    show();
    pick('Gust');
    fireEvent.click(play());
    tick(0.2);
    pick('Building wind');
    expect(screen.queryByLabelText('Stop the preview')).toBeNull();
    expect(previews[previews.length - 1]).toEqual(['wind.main', null]);
  });

  it('releases the device when the panel goes away', () => {
    const { unmount } = show();
    pick();
    fireEvent.click(play());
    tick(0.2);
    unmount();
    expect(previews[previews.length - 1]).toEqual(['wind.main', null]);
  });
});

describe('inserting', () => {
  it('does not need a preview first', () => {
    const inserted: string[] = [];
    show({ onInsert: (p) => inserted.push(p.name) });
    pick();
    fireEvent.click(screen.getByText('Insert at playhead'));
    expect(inserted).toEqual(['Gust']);
  });

  it('is offered only once a preset is chosen', () => {
    show();
    expect(screen.queryByText('Insert at playhead')).toBeNull();
    pick();
    expect(screen.getByText('Insert at playhead')).toBeTruthy();
  });
});
