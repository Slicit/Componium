import { describe, it, expect } from 'vitest';
import { insertPreset } from './edits';
import { presetById } from './presets';
import { History } from './history';
import type { Track } from './score';

const fade = presetById('fog-fade')!;
const gust = presetById('wind-gust')!;

const cueTrack = (): Track =>
  ({ instrument: 'fog.left', type: 'cue', cues: [] }) as Track;

const curveTrack = (): Track =>
  ({ instrument: 'wind.main', type: 'curve', points: [] }) as Track;

function run(_track: Track, cmd: ReturnType<typeof insertPreset>) {
  const h = new History();
  if (cmd) h.run(cmd);
  return h;
}

describe('inserting a preset', () => {
  it('puts a cue on an event device', () => {
    const track = cueTrack();
    run(track, insertPreset(track, fade, 100, ['output']));
    expect(track.cues).toHaveLength(1);
    expect(track.cues![0].action).toBe('burst');
    expect(track.cues![0].t).toBe(100);
    expect(track.cues![0].duration).toBe(fade.seconds);
  });

  it('puts points on a continuous device', () => {
    const track = curveTrack();
    run(track, insertPreset(track, gust, 10, ['intensity']));
    expect(track.points!.length).toBe(gust.shape.length);
    expect(track.points![0].t).toBe(10);
  });

  it('is one undoable step, however many things it added', () => {
    // Fourteen points arriving as fourteen undos would be unusable.
    const track = curveTrack();
    const h = run(track, insertPreset(track, gust, 10, ['intensity']));
    expect(track.points!.length).toBeGreaterThan(1);
    h.undo();
    expect(track.points).toHaveLength(0);
  });

  it('undoes back to exactly what was there before', () => {
    const track = curveTrack();
    track.points = [{ t: 0, value: { intensity: 0.2 } }, { t: 50, value: { intensity: 0.3 } }];
    const before = JSON.stringify(track.points);
    const h = run(track, insertPreset(track, gust, 10, ['intensity']));
    h.undo();
    expect(JSON.stringify(track.points)).toBe(before);
  });

  it('clears whatever occupied the span, rather than interleaving', () => {
    /* Mixing the new shape into the old points produces a curve that is
     * neither of them. Replacing is the option a person can take back. */
    const track = curveTrack();
    track.points = [
      { t: 0, value: { intensity: 0.9 } },
      { t: 11, value: { intensity: 0.9 } },   // inside the span
      { t: 12, value: { intensity: 0.9 } },   // inside the span
      { t: 50, value: { intensity: 0.9 } },
    ];
    run(track, insertPreset(track, gust, 10, ['intensity']));
    const survivors = track.points!.filter((p) => p.value.intensity === 0.9);
    expect(survivors.map((p) => p.t)).toEqual([0, 50]);
  });

  it('clears a clashing cue on an event track', () => {
    const track = cueTrack();
    track.cues = [
      { t: 101, action: 'burst', params: { output: 0.2 }, duration: 1 },
      { t: 500, action: 'burst', params: { output: 0.2 }, duration: 1 },
    ];
    run(track, insertPreset(track, fade, 100, ['output']));
    expect(track.cues!.map((c) => c.t).sort((a, b) => a - b)).toEqual([100, 500]);
  });

  it('leaves the score sorted', () => {
    const track = curveTrack();
    track.points = [{ t: 0, value: { intensity: 0.1 } }, { t: 90, value: { intensity: 0.1 } }];
    run(track, insertPreset(track, gust, 40, ['intensity']));
    const times = track.points!.map((p) => p.t);
    expect([...times].sort((a, b) => a - b)).toEqual(times);
  });

  it('names itself after the preset, so undo says what it undoes', () => {
    const track = curveTrack();
    const cmd = insertPreset(track, gust, 10, ['intensity']);
    expect(cmd!.label).toContain(gust.name);
  });

  it('gives every channel the shape', () => {
    const track = ({ instrument: 'motion.platform', type: 'curve', points: [] }) as Track;
    const sway = presetById('motion-sway')!;
    run(track, insertPreset(track, sway, 0, ['heave', 'roll', 'pitch']));
    for (const p of track.points!) {
      expect(Object.keys(p.value).sort()).toEqual(['heave', 'pitch', 'roll']);
    }
  });

  it('takes a length and a scale', () => {
    const track = curveTrack();
    run(track, insertPreset(track, gust, 0, ['intensity'], { seconds: 30, scale: 0.5 }));
    const times = track.points!.map((p) => p.t);
    expect(Math.max(...times)).toBe(30);
    expect(Math.max(...track.points!.map((p) => p.value.intensity))).toBeCloseTo(0.5);
  });
});
