// @vitest-environment jsdom

/* The room redraws when the score is edited.
 *
 * Commands mutate the score in place — they hold references to the tracks and
 * cues they act on, because every edit re-sorts the track — so the score object
 * handed to the room is the same object before and after an edit. An effect
 * watching only that object never runs again, and adding a cue at the playhead
 * changed nothing in the room until you scrubbed away and back. That reads as
 * the room being out of sync with the timeline, which is exactly how it was
 * reported.
 *
 * Room3D is a WebGL class jsdom cannot build, so these watch the seam it is
 * driven through instead: update() is called with a fresh scene state.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, waitFor } from '@testing-library/react';
import { Room } from './Room';
import type { Rig, Score } from '../../core/score';

const updates: unknown[] = [];

vi.mock('./Room3D.js', () => {
  class FakeRoom {
    setInstruments() {}
    setPicture() {}
    setProjection() {}
    setMuted() {}
    setForced() {}
    setBrightness() {}
    setWash() {}
    onView() {}
    getView() { return { pos: [0, 0, 0], target: [0, 0, 0] }; }
    setView() {}
    update(state: unknown) { updates.push(state); }
    onMeter() {}
    dispose() {}
  }
  return { Room3D: FakeRoom, webglAvailable: () => true, HOME_VIEW: { pos: [0, 0, 0], target: [0, 0, 0] } };
});

const rig = {
  name: 'demo',
  instruments: [{ id: 'fog.left', kind: 'fog', latency: 0 }],
} as Rig;

function scoreWithFog(): Score {
  return {
    title: 'demo', duration: 100,
    tracks: [{ instrument: 'fog.left', type: 'cue', cues: [] }],
  } as unknown as Score;
}

beforeEach(() => { updates.length = 0; });
afterEach(() => { cleanup(); });

/* Stable across renders on purpose. A fresh Set or Map per render is itself a
 * changed dependency, which would make the effect re-run for a reason that has
 * nothing to do with the edit — and would make this test pass with the fix
 * taken out, which is how the first version of it was found to be worthless. */
const NO_MUTES = new Set<string>();
const NO_FORCES = new Map<string, number>();

function draw(score: Score, revision: number, time = 10) {
  return (
    <Room
      score={score} rig={rig} time={time}
      muted={NO_MUTES} forced={NO_FORCES}
      brightness={60} revision={revision}
    />
  );
}

describe('the room and an edit', () => {
  it('redraws when the revision changes, though the score is the same object', async () => {
    const score = scoreWithFog();
    const { rerender } = render(draw(score, 1));
    await waitFor(() => expect(updates.length).toBeGreaterThan(0));
    const before = updates.length;

    /* Exactly what a command does: reach into the score and change it, leaving
     * the object identity alone. */
    (score.tracks[0] as { cues: unknown[] }).cues.push(
      { t: 10, action: 'burst', params: { output: 0.6 }, duration: 4 });
    rerender(draw(score, 2));

    await waitFor(() => expect(updates.length).toBeGreaterThan(before));
  });

  it('reports the cue as active once it has been added', async () => {
    const score = scoreWithFog();
    const { rerender } = render(draw(score, 1));
    await waitFor(() => expect(updates.length).toBeGreaterThan(0));

    const idle = updates[updates.length - 1] as Record<string, { active: boolean }>;
    expect(idle['fog.left'].active).toBe(false);

    (score.tracks[0] as { cues: unknown[] }).cues.push(
      { t: 10, action: 'burst', params: { output: 0.6 }, duration: 4 });
    rerender(draw(score, 2));

    await waitFor(() => {
      const now = updates[updates.length - 1] as Record<string, { active: boolean; level: number }>;
      expect(now['fog.left'].active).toBe(true);
      // The fogger's parameter is output, and it has to reach the room as a
      // level or the burst is drawn at nothing.
      expect(now['fog.left'].level).toBeCloseTo(0.6);
    });
  });

  it('still redraws when only the playhead moves', async () => {
    const score = scoreWithFog();
    const { rerender } = render(draw(score, 1, 10));
    await waitFor(() => expect(updates.length).toBeGreaterThan(0));
    const before = updates.length;
    rerender(draw(score, 1, 20));
    await waitFor(() => expect(updates.length).toBeGreaterThan(before));
  });
});
