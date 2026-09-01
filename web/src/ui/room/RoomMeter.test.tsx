// @vitest-environment jsdom

/* The frame counter in the corner of the room.
 *
 * Room3D is a WebGL class jsdom cannot build, so this watches the seam it is
 * driven through: the renderer hands the wrapper a reading, and the wrapper
 * puts it on screen. The arithmetic itself is in meter.test.ts, where it can
 * be checked against a clock that does not have to be real.
 */

import { describe, it, expect, afterEach, vi } from 'vitest';
import { render, cleanup, screen, waitFor, act } from '@testing-library/react';
import { Room } from './Room';
import type { Rig, Score } from '../../core/score';

let report: ((reading: { rate: number; cost: number }) => void) | null = null;

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
    update() {}
    onMeter(fn: (reading: { rate: number; cost: number }) => void) { report = fn; }
    dispose() {}
  }
  return { Room3D: FakeRoom, webglAvailable: () => true, HOME_VIEW: { pos: [0, 0, 0], target: [0, 0, 0] } };
});

const rig = { name: 'demo', instruments: [] } as unknown as Rig;
const score = { title: 'demo', duration: 100, tracks: [] } as unknown as Score;
const muted = new Set<string>();
const forced = new Map<string, number>();

function show() {
  return render(
    <Room score={score} rig={rig} time={0} muted={muted} forced={forced}
          brightness={15} view={null} onView={() => {}} revision={0} />,
  );
}

afterEach(() => { cleanup(); report = null; });

describe('the frame counter', () => {
  it('says nothing until the renderer has measured something', async () => {
    show();
    await waitFor(() => expect(report).not.toBeNull());
    expect(screen.queryByText(/fps/)).toBeNull();
  });

  it('shows the rate and what a frame cost', async () => {
    show();
    await waitFor(() => expect(report).not.toBeNull());
    act(() => { report!({ rate: 59.6, cost: 3.24 }); });
    expect(screen.getByText('60 fps')).toBeTruthy();
    expect(screen.getByText('3.2 ms')).toBeTruthy();
  });

  it('keeps reporting as the reading moves', async () => {
    show();
    await waitFor(() => expect(report).not.toBeNull());
    act(() => { report!({ rate: 60, cost: 3 }); });
    act(() => { report!({ rate: 4, cost: 3 }); });
    /* Four drawn frames a second beside a three millisecond frame is the
     * shape of a room being asked for frames slowly rather than one that
     * cannot keep up, which is the entire reason both numbers are shown. */
    expect(screen.getByText('4 fps')).toBeTruthy();
    expect(screen.getByText('3.0 ms')).toBeTruthy();
  });
});
