// @vitest-environment jsdom

/* When the timeline redraws.
 *
 * A draw is not free. Measured on a two hour score at the zoom people actually
 * review at — the whole film on screen — building one draw list costs about
 * five milliseconds, which is a third of a frame budget before the canvas has
 * rasterised anything. Zoomed in it is under one.
 *
 * So the question these ask is not "does it draw" but "does it draw when
 * nothing it draws has changed". The effect that runs it had no dependency
 * array, so it ran on every commit of the component: every unrelated piece of
 * app state, every slider drag somewhere else on the page, every poll of the
 * library, paid for a full rebuild of the timeline.
 *
 * The reason it was written that way is real and worth keeping in mind: the
 * view is a stable mutable object, so its identity never changes when the
 * window is panned or zoomed. Depending on the object would miss every scroll.
 * Depending on the two numbers it holds does not.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, act } from '@testing-library/react';
import { Timeline } from './Timeline';
import { TimeView } from '../core/view';
import { useEditing } from './useEditing';
import { History } from '../core/history';
import type { Rig, Score } from '../core/score';

const paints = vi.fn();
vi.mock('../render/drawlist', async () => {
  const real = await vi.importActual<typeof import('../render/drawlist')>('../render/drawlist');
  return {
    ...real,
    paint: (...args: Parameters<typeof real.paint>) => { paints(); real.paint(...args); },
  };
});

const rig = { name: 'demo', instruments: [{ id: 'wind.main', kind: 'wind', latency: 0 }] } as Rig;

function scoreOf(): Score {
  return {
    title: 'demo', duration: 120, fps: 24,
    tracks: [{
      instrument: 'wind.main', type: 'cue',
      cues: [{ t: 10, action: 'gust', params: { intensity: 0.5 }, duration: 2 }],
    }],
  } as unknown as Score;
}

/* jsdom has no canvas and reports every element as zero width, and the draw
 * bails on both. Given a size and a context it does the work, which is what
 * has to be counted.
 *
 * The context answers anything: a hand written list of methods makes a test
 * that breaks whenever the renderer reaches for a new one, which is a test
 * about the renderer rather than about how often it runs. Nothing here checks
 * what was drawn - that is what render/lanes.test.ts is for, against the draw
 * list, which is the whole reason the renderer emits data instead of painting.
 */
function fakeContext(): CanvasRenderingContext2D {
  const measured = { width: 10 };
  const gradient = { addColorStop() {} };
  const noop = () => undefined;
  return new Proxy({} as Record<string, unknown>, {
    get(_target, key) {
      if (key === 'measureText') return () => measured;
      if (key === 'createLinearGradient') return () => gradient;
      if (key === 'canvas') return {};
      return noop;
    },
    set() { return true; },
  }) as unknown as CanvasRenderingContext2D;
}

function makeDrawable() {
  HTMLCanvasElement.prototype.getContext =
    (() => fakeContext()) as unknown as HTMLCanvasElement['getContext'];
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
    configurable: true, get() { return 1000; },
  });
}

let view: TimeView;
const NO_OVERLAYS = { calm: false, latency: false };

/* The editing state is a hook, so it has to be held by a component.
 *
 * The score and the empty collections are built once and reused, because a
 * fresh object every render is itself a changed dependency — it would make
 * every one of these pass for the wrong reason, which is how the first
 * version of a test like this is usually worthless. */
const SCORE = scoreOf();
const NO_COLLAPSE = new Set<string>();
const NO_ORDER: string[] = [];
const history = new History();

let edit: ReturnType<typeof useEditing>;
function Harness(props: { time: number; revision: number; overlays: typeof NO_OVERLAYS }) {
  edit = useEditing({
    score: SCORE, rig, view, history, time: props.time, fps: 24,
    onSeek: () => {}, onChanged: () => {},
  });
  return (
    <Timeline
      score={SCORE} rig={rig} view={view} time={props.time}
      collapsed={NO_COLLAPSE} order={NO_ORDER} onSeek={() => {}} onView={() => {}}
      edit={edit} revision={props.revision} overlays={props.overlays}
    />
  );
}

beforeEach(() => {
  paints.mockClear();
  makeDrawable();
  view = new TimeView(120, 24).fit();
});
afterEach(() => cleanup());

describe('the timeline draws when something it draws has changed', () => {
  it('draws once on mount', () => {
    render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    expect(paints).toHaveBeenCalled();
  });

  it('draws again when the playhead moves', () => {
    const { rerender } = render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    rerender(<Harness time={5} revision={0} overlays={NO_OVERLAYS} />);
    expect(paints.mock.calls.length).toBeGreaterThan(before);
  });

  it('draws again when the score is edited', () => {
    // The score is mutated in place by every command, so the object is the
    // same before and after. The revision counter is what says it changed.
    const { rerender } = render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    rerender(<Harness time={0} revision={1} overlays={NO_OVERLAYS} />);
    expect(paints.mock.calls.length).toBeGreaterThan(before);
  });

  it('draws again when the window is panned', () => {
    // The view is one mutable object for the life of the component, so this
    // is the case that a naive dependency on it would miss entirely.
    //
    // Zoomed in first, because panning a view that already shows the whole
    // film is clamped straight back to where it started — there is nowhere to
    // go, nothing changes, and not redrawing is the right answer.
    view.set(0, 30);
    const { rerender } = render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    act(() => { view.pan(10); });
    rerender(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    expect(paints.mock.calls.length).toBeGreaterThan(before);
    expect(view.start).toBe(10);
  });

  it('does not draw when a pan had nowhere to go', () => {
    // The whole film is already on screen, so the window does not move. A
    // redraw here would be work done to produce an identical picture.
    const { rerender } = render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    act(() => { view.pan(10); });
    rerender(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    expect(paints.mock.calls.length).toBe(before);
  });

  it('draws again when the window is zoomed', () => {
    const { rerender } = render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    act(() => { view.zoom(0.5); });
    rerender(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    expect(paints.mock.calls.length).toBeGreaterThan(before);
  });

  it('draws again when an overlay is toggled', () => {
    const { rerender } = render(<Harness time={0} revision={0} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    rerender(<Harness time={0} revision={0} overlays={{ calm: true, latency: false }} />);
    expect(paints.mock.calls.length).toBeGreaterThan(before);
  });

  it('does NOT draw when nothing it draws has changed', () => {
    // The one that matters. A commit caused by anything else on the page —
    // a slider being dragged, the library polling, a room control moving —
    // used to cost a full rebuild of the timeline.
    const { rerender } = render(<Harness time={3} revision={2} overlays={NO_OVERLAYS} />);
    const before = paints.mock.calls.length;
    for (let i = 0; i < 20; i++) {
      rerender(<Harness time={3} revision={2} overlays={NO_OVERLAYS} />);
    }
    expect(paints.mock.calls.length).toBe(before);
  });
});
