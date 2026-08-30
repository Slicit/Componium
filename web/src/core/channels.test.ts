import { describe, it, expect } from 'vitest';
import { channelsForKind, channelsOf } from './score';
import type { Rig, Track } from './score';

const rig: Rig = {
  name: 'demo',
  instruments: [
    { id: 'fog.left', kind: 'fog', latency: 0 },
    { id: 'mist.main', kind: 'mist', latency: 0 },
    { id: 'wind.main', kind: 'wind', latency: 0 },
    { id: 'shake.seat', kind: 'shake', latency: 0 },
    { id: 'motion.platform', kind: 'motion', latency: 0 },
    { id: 'light.ambient', kind: 'light', latency: 0 },
    { id: 'scent.main', kind: 'scent', latency: 0 },
  ],
} as Rig;

const empty = (instrument: string): Track =>
  ({ instrument, type: 'curve', points: [] }) as Track;

describe('channelsForKind', () => {
  it('gives a fogger its output, not an intensity it has no use for', () => {
    // Adding a fog track offered only "intensity" — the name a fan takes —
    // and no way to reach the output a fogger actually has.
    expect(channelsForKind('fog')).toEqual(['output']);
    expect(channelsForKind('mist')).toEqual(['output']);
  });

  it('keeps intensity for the devices that are dimmed', () => {
    expect(channelsForKind('wind')).toEqual(['intensity']);
    expect(channelsForKind('shake')).toEqual(['intensity']);
  });

  it('gives motion its three axes', () => {
    expect(channelsForKind('motion')).toEqual(['heave', 'roll', 'pitch']);
  });

  it('falls back to intensity for a kind it has never met', () => {
    expect(channelsForKind('haptic')).toEqual(['intensity']);
    expect(channelsForKind('')).toEqual(['intensity']);
  });
});

describe('channelsOf on an empty track', () => {
  it('asks the rig what the instrument is', () => {
    expect(channelsOf(empty('fog.left'), rig)).toEqual(['output']);
    expect(channelsOf(empty('wind.main'), rig)).toEqual(['intensity']);
    expect(channelsOf(empty('motion.platform'), rig)).toEqual(['heave', 'roll', 'pitch']);
  });

  it('reads the kind off the id when there is no rig', () => {
    // The id is conventionally kind.name, which is enough to be useful when
    // the studio is opened without a rig.
    expect(channelsOf(empty('fog.left'), null)).toEqual(['output']);
    expect(channelsOf(empty('mist.main'), null)).toEqual(['output']);
  });

  it('hands back a copy, so a caller cannot rewrite the table', () => {
    const first = channelsOf(empty('fog.left'), rig);
    first.push('nonsense');
    expect(channelsOf(empty('fog.left'), rig)).toEqual(['output']);
  });
});

describe('channelsOf on a track with points', () => {
  it('reports what the points actually carry, whatever the kind', () => {
    // A score written before this table existed, or by hand, is still read as
    // it was written rather than corrected into the table's opinion.
    const track = {
      instrument: 'fog.left', type: 'curve',
      points: [{ t: 0, value: { intensity: 0.4 } }, { t: 1, value: { intensity: 0.6 } }],
    } as Track;
    expect(channelsOf(track, rig)).toEqual(['intensity']);
  });
});
