/* Time, frames and timecode.
 *
 * Everything in a score is stored in seconds, because that is what the format
 * and the player use. Everything a person reads or nudges is in frames,
 * because that is how anyone who edits video thinks. This module is the only
 * place the two meet, so that rounding happens once and in a known way.
 *
 * No DOM, no framework, no dependencies. It is meant to be exhaustively
 * testable in node, which matters more here than usual: the browser this is
 * developed against cannot be trusted to run anything.
 */

/** Frames per second, as the score records it. */
export type Fps = number;

/** A moment, in seconds. The score's own unit. */
export type Seconds = number;

/* Films are not all 24fps and a score can arrive without the field, so there
 * has to be a fallback. 24 is what our own composer writes and what most of
 * what anyone will load is; a wrong guess costs display accuracy, never data. */
export const DEFAULT_FPS = 24;

export function fpsOf(score: { meta?: { media?: { fps?: number } } } | null | undefined): Fps {
  const f = score?.meta?.media?.fps;
  return typeof f === 'number' && f > 0 && isFinite(f) ? f : DEFAULT_FPS;
}

/** Which frame contains this moment. */
export function frameAt(t: Seconds, fps: Fps): number {
  if (!isFinite(t) || t <= 0) return 0;
  /* floor, not round: a moment belongs to the frame it falls inside, and a
   * moment exactly on a boundary belongs to the frame it starts. Rounding
   * would make the frame under the playhead flip half a frame early. */
  return Math.floor(t * fps + 1e-9);
}

/** The start of a frame, in seconds. */
export function frameStart(frame: number, fps: Fps): Seconds {
  return Math.max(0, Math.round(frame)) / fps;
}

/**
 * Snap a moment to the nearest frame boundary.
 *
 * Deliberately not rounded to a fixed number of decimals. A frame boundary at
 * 24fps is 0.0416666…, and rounding it to six places moves it by a third of a
 * microsecond — harmless on its own, but it means the result is no longer
 * exactly `n / fps`, so snapping twice can land on a different frame than
 * snapping once. Rounding belongs where a number is written into the score,
 * not where one is computed.
 */
export function snapToFrame(t: Seconds, fps: Fps): Seconds {
  return Math.round(t * fps) / fps;
}

/** Move a moment by whole frames, never off the front of the film. */
export function stepFrames(t: Seconds, frames: number, fps: Fps, duration: Seconds): Seconds {
  const next = frameStart(frameAt(t, fps) + frames, fps);
  return clamp(next, 0, duration);
}

/* Six places is past the point where a difference could survive a round trip
 * through the score's millisecond timecodes, and it keeps arithmetic from
 * writing 0.30000000000000004 into anything a person will read. */
export function round6(v: number): number {
  return Math.round(v * 1e6) / 1e6;
}

export function round3(v: number): number {
  return Math.round(v * 1e3) / 1e3;
}

export function clamp(v: number, lo: number, hi: number): number {
  return v < lo ? lo : v > hi ? hi : v;
}

export function clamp01(v: number): number {
  return !isFinite(v) ? 0 : v < 0 ? 0 : v > 1 ? 1 : v;
}

/**
 * SMPTE-style timecode, `HH:MM:SS:FF`.
 *
 * Hours are dropped below an hour, because a fifteen minute film reading
 * `00:14:48:03` wastes the widest column in the interface on three characters
 * that are always zero.
 */
export function timecode(t: Seconds, fps: Fps, opts: { hours?: boolean } = {}): string {
  const neg = t < 0;
  const total = Math.abs(t);
  const frames = frameAt(total, fps);
  const ff = frames % Math.round(fps);
  const whole = Math.floor(frames / Math.round(fps));
  const ss = whole % 60;
  const mm = Math.floor(whole / 60) % 60;
  const hh = Math.floor(whole / 3600);
  const showHours = opts.hours ?? hh > 0;
  const p2 = (n: number) => String(n).padStart(2, '0');
  const body = showHours
    ? `${p2(hh)}:${p2(mm)}:${p2(ss)}:${p2(ff)}`
    : `${p2(mm)}:${p2(ss)}:${p2(ff)}`;
  return neg ? '-' + body : body;
}

/** The old display format, kept because the score's own timecodes use it. */
export function clockMs(t: Seconds): string {
  const total = Math.max(0, Math.round(t * 1000));
  const ms = total % 1000;
  const s = Math.floor(total / 1000) % 60;
  const m = Math.floor(total / 60000);
  const p2 = (n: number) => String(n).padStart(2, '0');
  return `${p2(m)}:${p2(s)}.${String(ms).padStart(3, '0')}`;
}

/**
 * Parse what someone typed into a time field.
 *
 * Deliberately generous, because a person correcting a cue time types the
 * shortest thing that identifies it. All of these work:
 *
 *   `90`            90 seconds
 *   `1:30`          a minute and a half
 *   `01:30:12`      with frames, when the field is in timecode
 *   `1:02:03:04`    hours
 *   `1.5s`          seconds with a suffix
 *   `240f`          a frame number
 *
 * Returns null rather than a wrong answer when it cannot tell, so the caller
 * can leave the field alone instead of moving a cue somewhere arbitrary.
 */
export function parseTime(input: string, fps: Fps): Seconds | null {
  const raw = String(input ?? '').trim().toLowerCase();
  if (!raw) return null;

  const frames = raw.match(/^(\d+)\s*f$/);
  if (frames) return round6(frameStart(Number(frames[1]), fps));

  const secs = raw.match(/^(\d+(?:\.\d+)?)\s*s$/);
  if (secs) return round6(Number(secs[1]));

  if (/^\d+(\.\d+)?$/.test(raw)) return round6(Number(raw));

  const parts = raw.split(':');
  if (parts.length < 2 || parts.length > 4) return null;
  if (!parts.every((p) => /^\d+(\.\d+)?$/.test(p))) return null;
  const n = parts.map(Number);

  // mm:ss.mmm — the score's own written form, distinguished by the decimal.
  if (n.length === 2 && parts[1].includes('.')) return round6(n[0] * 60 + n[1]);
  // mm:ss
  if (n.length === 2) return round6(n[0] * 60 + n[1]);
  // mm:ss:ff
  if (n.length === 3 && !parts[2].includes('.')) {
    return round6(n[0] * 60 + n[1] + n[2] / fps);
  }
  // hh:mm:ss
  if (n.length === 3) return round6(n[0] * 3600 + n[1] * 60 + n[2]);
  // hh:mm:ss:ff
  return round6(n[0] * 3600 + n[1] * 60 + n[2] + n[3] / fps);
}

/**
 * A readable duration, for labels like a cue's length.
 * Short things get their frames, long things do not.
 */
export function durationLabel(d: Seconds, fps: Fps): string {
  if (d <= 0) return 'instant';
  if (d < 1) return `${Math.round(d * fps)}f`;
  if (d < 60) return `${round3(d)}s`;
  const m = Math.floor(d / 60);
  const s = Math.round(d % 60);
  return `${m}m${String(s).padStart(2, '0')}`;
}
