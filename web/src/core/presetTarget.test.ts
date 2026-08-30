import { describe, it, expect } from 'vitest';
import { insertPreset } from './edits';
import { presetById } from './presets';
import { History } from './history';
import type { Rig, Track } from './score';

/* Reported as "I click insert at playhead and nothing shows on the timeline".
 *
 * A fog track added by hand is a curve track — addTrack makes curves — and
 * every fog preset carries an action, so the insertion was built as a cue and
 * written into a curve track. The timeline draws a curve from its points, so
 * nothing appeared. Worse than invisible: normalise refuses a curve track
 * carrying cues, so the score would not have saved either.
 *
 * The preset says what shape it is. The track says what it can hold. The track
 * wins.
 */

const rig = {
  name: 'demo',
  instruments: [
    { id: 'fog.left', kind: 'fog' },
    { id: 'mist.main', kind: 'mist' },
    { id: 'light.event', kind: 'light' },
    { id: 'motion.platform', kind: 'motion' },
  ],
} as Rig;

function apply(cmd: ReturnType<typeof insertPreset>) {
  const h = new History();
  if (cmd) h.run(cmd);
  return h;
}

const curve = (instrument: string): Track =>
  ({ instrument, type: 'curve', points: [] }) as Track;
const cues = (instrument: string): Track =>
  ({ instrument, type: 'cue', cues: [] }) as Track;

describe('the track decides what is built', () => {
  it('puts points on a curve track even when the preset is an event', () => {
    const track = curve('fog.left');
    apply(insertPreset(track, presetById('fog-fade')!, 10, ['output'], {}, rig));
    expect(track.points!.length).toBeGreaterThan(1);
    expect(track.cues ?? []).toHaveLength(0);
  });

  it('puts a cue on a cue track even when the preset is a level shape', () => {
    const track = cues('light.event');
    apply(insertPreset(track, presetById('light-flash')!, 10, ['intensity'], {}, rig));
    expect(track.cues).toHaveLength(1);
    expect(track.cues![0].action).toBe('flash');
    expect(track.points ?? []).toHaveLength(0);
  });

  it('never leaves a curve track holding cues', () => {
    // The shape the format refuses outright.
    const track = curve('mist.main');
    apply(insertPreset(track, presetById('mist-splash')!, 5, ['output'], {}, rig));
    expect(track.cues ?? []).toHaveLength(0);
    expect(track.points!.length).toBeGreaterThan(1);
  });

  it('never leaves a cue track holding points', () => {
    const track = cues('fog.left');
    apply(insertPreset(track, presetById('fog-fade')!, 5, ['output'], {}, rig));
    expect(track.points ?? []).toHaveLength(0);
    expect(track.cues).toHaveLength(1);
  });

  it('refuses rather than inventing a verb for a kind that has none', () => {
    // Motion is driven as a curve; there is no cue action for it, and making
    // one up produces a cue addressed to an instrument that never heard of it.
    const track = cues('motion.platform');
    expect(insertPreset(track, presetById('motion-drop')!, 5, ['heave'], {}, rig)).toBeNull();
  });

  it('reads the kind off the id when there is no rig', () => {
    const track = cues('fog.left');
    apply(insertPreset(track, presetById('fog-fade')!, 5, ['output']));
    expect(track.cues).toHaveLength(1);
    expect(track.cues![0].action).toBe('burst');
  });

  it('leaves a curve track with enough points to be a curve', () => {
    // One point is not a curve, and the format says so.
    const track = curve('fog.left');
    apply(insertPreset(track, presetById('fog-burst')!, 5, ['output'], {}, rig));
    expect(track.points!.length).toBeGreaterThanOrEqual(2);
  });
});
