// @vitest-environment jsdom

/* The wheel over the timeline belongs to the timeline.
 *
 * This exists because the bug it guards against is invisible in the source:
 * the handler called preventDefault() and had always called it, and the page
 * scrolled anyway. React registers wheel at the root as a passive listener, so
 * preventDefault() inside an onWheel handler is silently ignored — the
 * timeline panned and the window scrolled at once, and whatever you were
 * looking at slid off the screen while you looked at it.
 *
 * Nothing about the handler shows that. The only observable difference is
 * whether the event comes back prevented, which is what these assert.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import { App } from './App';

const score = {
  title: 'Demo',
  duration: 300,
  fps: 24,
  path: '/scores/demo.componium',
  tracks: [
    {
      instrument: 'light.ambient', type: 'curve',
      points: [{ t: 0, value: { r: 0, g: 0, b: 0 } }, { t: 60, value: { r: 1, g: 0, b: 0 } }],
    },
  ],
};

const rig = { name: 'test', instruments: [{ id: 'wind.main', kind: 'wind', latency: 0 }] };

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const body = url.startsWith('/api/score') ? score
      : url.startsWith('/api/rig') ? rig
        : url.startsWith('/api/versions') ? { versions: [] }
          : url.startsWith('/api/library') ? { entries: [], scores: '', free: 0 }
            : [];
    return { ok: true, json: async () => body, text: async () => JSON.stringify(body) } as Response;
  }));
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

async function surface(): Promise<HTMLElement> {
  render(<App />);
  await screen.findByText(/Componium/);
  return waitFor(() => {
    const el = document.querySelector('.tl-surface');
    if (!el) throw new Error('no timeline surface');
    return el as HTMLElement;
  });
}

function wheelOver(el: HTMLElement, init: WheelEventInit = {}): WheelEvent {
  const e = new WheelEvent('wheel', {
    bubbles: true, cancelable: true, clientX: 100, clientY: 60, deltaY: 120, ...init,
  });
  el.dispatchEvent(e);
  return e;
}

describe('the wheel over the timeline', () => {
  it('is taken by the timeline rather than scrolling the page', async () => {
    const el = await surface();
    expect(wheelOver(el).defaultPrevented).toBe(true);
  });

  it('is taken for a zoom gesture too', async () => {
    // Trackpad pinch arrives as ctrl+wheel, and a pinch that also scrolled the
    // page would be the same fault wearing a different hat.
    const el = await surface();
    expect(wheelOver(el, { ctrlKey: true }).defaultPrevented).toBe(true);
    expect(wheelOver(el, { shiftKey: true }).defaultPrevented).toBe(true);
    expect(wheelOver(el, { metaKey: true }).defaultPrevented).toBe(true);
  });

  it('is taken for a sideways wheel', async () => {
    const el = await surface();
    expect(wheelOver(el, { deltaX: 120, deltaY: 0 }).defaultPrevented).toBe(true);
  });

  it('leaves the page alone everywhere else', async () => {
    // The capture is scoped to the timeline. A wheel over the library or the
    // header should still scroll the window, or the studio becomes a page you
    // cannot scroll.
    await surface();
    const header = document.querySelector('header') as HTMLElement;
    expect(header).not.toBeNull();
    expect(wheelOver(header).defaultPrevented).toBe(false);
  });

  it('keeps taking the wheel after the view changes', async () => {
    /* The listener is subscribed once and reaches the handler through a ref.
     * Keying the effect on the callback instead would tear it down and rebuild
     * it on every render, and a wheel landing between the two does nothing. */
    const el = await surface();
    for (let i = 0; i < 5; i++) {
      expect(wheelOver(el, { deltaY: 120 }).defaultPrevented).toBe(true);
    }
  });
});
