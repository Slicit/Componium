// @vitest-environment jsdom

/* The film reaches the television, and stops reaching it.
 *
 * Room3D is a WebGL class jsdom cannot build, and the texture itself is a GPU
 * upload nothing here can look at. What can be checked is the seam: whether
 * the element the picture pane is playing is handed to the renderer when the
 * switch is on, and taken away again when it is off.
 *
 * That is the whole of the wiring, and it is where this kind of thing breaks —
 * the element does not exist when the room mounts, because a film is picked
 * later, and an effect that reads a ref at the wrong moment sees null and
 * never looks again. The playhead once stopped following the picture for
 * exactly that reason.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, waitFor } from '@testing-library/react';
import { Room } from './Room';
import type { Rig, Score } from '../../core/score';

const shown: (HTMLVideoElement | null)[] = [];
const thrown: (HTMLVideoElement | null)[] = [];
const washed: number[] = [];

vi.mock('./Room3D.js', () => {
  class FakeRoom {
    setInstruments() {}
    setPicture(video: HTMLVideoElement | null) { shown.push(video); }
    setProjection(video: HTMLVideoElement | null) { thrown.push(video); }
    setMuted() {}
    setForced() {}
    setBrightness() {}
    setWash(v: number) { washed.push(v); }
    onView() {}
    getView() { return { pos: [0, 0, 0], target: [0, 0, 0] }; }
    setView() {}
    update() {}
    onMeter() {}
    dispose() {}
  }
  return {
    Room3D: FakeRoom,
    webglAvailable: () => true,
    HOME_VIEW: { pos: [0, 0, 0], target: [0, 0, 0] },
  };
});

const rig = { name: 'demo', instruments: [] } as unknown as Rig;
const score = { title: 'demo', duration: 100, tracks: [] } as unknown as Score;

const NO_MUTES = new Set<string>();
const NO_FORCES = new Map<string, number>();

beforeEach(() => { shown.length = 0; thrown.length = 0; washed.length = 0; });
afterEach(() => { cleanup(); });

function show(picture: HTMLVideoElement | null,
              projection: HTMLVideoElement | null = null) {
  return (
    <Room
      score={score}
      rig={rig}
      time={0}
      muted={NO_MUTES}
      forced={NO_FORCES}
      brightness={50}
      picture={picture}
      projection={projection}
    />
  );
}

describe('the film on the television', () => {
  it('is off unless it is asked for', async () => {
    render(show(null));
    await waitFor(() => expect(shown.length).toBeGreaterThan(0));
    expect(shown.every((v) => v === null)).toBe(true);
  });

  it('is handed the element the picture pane is playing', async () => {
    const video = document.createElement('video');
    render(show(video));
    await waitFor(() => expect(shown).toContain(video));
  });

  it('arrives even though the film is picked after the room is built', async () => {
    // The case the wiring exists for. The room mounts with no film; one is
    // chosen later and the element only then comes into being.
    const { rerender } = render(show(null));
    await waitFor(() => expect(shown.length).toBeGreaterThan(0));
    const video = document.createElement('video');
    rerender(show(video));
    await waitFor(() => expect(shown).toContain(video));
  });

  it('is taken away when the switch goes off', async () => {
    const video = document.createElement('video');
    const { rerender } = render(show(video));
    await waitFor(() => expect(shown).toContain(video));
    rerender(show(null));
    await waitFor(() => expect(shown[shown.length - 1]).toBe(null));
  });

  it('is not re-sent on a render that changed something else', async () => {
    // setPicture disposes and rebuilds a video texture, so being called for
    // every scrub would rebuild it sixty times a second.
    const video = document.createElement('video');
    const { rerender } = render(show(video));
    await waitFor(() => expect(shown).toContain(video));
    const before = shown.length;
    rerender(
      <Room
        score={score}
        rig={rig}
        time={42}
        muted={NO_MUTES}
        forced={NO_FORCES}
        brightness={80}
        picture={video}
      />,
    );
    await waitFor(() => expect(shown.length).toBe(before));
  });
});

describe('throwing the film into the room', () => {
  it('is off unless it is asked for', async () => {
    render(show(null));
    await waitFor(() => expect(thrown.length).toBeGreaterThan(0));
    expect(thrown.every((v) => v === null)).toBe(true);
  });

  it('is switched independently of the screen', async () => {
    // Either can be on without the other. They ask the room for the same
    // frames, and the room decides that a video is uploaded once.
    const video = document.createElement('video');
    render(show(null, video));
    await waitFor(() => expect(thrown).toContain(video));
    expect(shown.every((v) => v === null)).toBe(true);
  });

  it('can be on at the same time as the screen', async () => {
    const video = document.createElement('video');
    render(show(video, video));
    await waitFor(() => expect(thrown).toContain(video));
    expect(shown).toContain(video);
  });

  it('is taken away when the switch goes off', async () => {
    const video = document.createElement('video');
    const { rerender } = render(show(video, video));
    await waitFor(() => expect(thrown).toContain(video));
    rerender(show(video, null));
    await waitFor(() => expect(thrown[thrown.length - 1]).toBe(null));
  });

  it('is not re-sent when only the screen changes', async () => {
    // setProjection rebuilds the light's map, so it must not be called for
    // something that has nothing to do with it.
    const video = document.createElement('video');
    const { rerender } = render(show(null, video));
    await waitFor(() => expect(thrown).toContain(video));
    const before = thrown.length;
    rerender(show(video, video));
    await waitFor(() => expect(shown).toContain(video));
    expect(thrown.length).toBe(before);
  });
});

describe('how strong the wash is', () => {
  it('reaches the room as a fraction, not a percentage', async () => {
    render(
      <Room score={score} rig={rig} time={0} muted={NO_MUTES} forced={NO_FORCES}
            brightness={50} wash={60} />,
    );
    await waitFor(() => expect(washed.length).toBeGreaterThan(0));
    expect(washed[washed.length - 1]).toBeCloseTo(0.6, 6);
  });

  it('has a default a caller need not state', async () => {
    // A room with no opinion about the wash still gets one, and it is the same
    // one the renderer holds, so the prop and its absence agree.
    render(
      <Room score={score} rig={rig} time={0} muted={NO_MUTES} forced={NO_FORCES}
            brightness={50} />,
    );
    await waitFor(() => expect(washed.length).toBeGreaterThan(0));
    expect(washed[washed.length - 1]).toBeCloseTo(0.75, 6);
  });

  it('follows the slider', async () => {
    const { rerender } = render(
      <Room score={score} rig={rig} time={0} muted={NO_MUTES} forced={NO_FORCES}
            brightness={50} wash={30} />,
    );
    await waitFor(() => expect(washed.length).toBeGreaterThan(0));
    rerender(
      <Room score={score} rig={rig} time={0} muted={NO_MUTES} forced={NO_FORCES}
            brightness={50} wash={0} />,
    );
    await waitFor(() => expect(washed[washed.length - 1]).toBe(0));
  });
});
