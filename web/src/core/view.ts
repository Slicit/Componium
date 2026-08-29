/* The visible time window.
 *
 * One object owns "which part of the film am I looking at", and everything
 * else — the ruler, the lanes, the overview strip, the renderer's decision
 * about how much detail to draw — reads it. Keeping that in one place is what
 * makes the overview strip and the main surface agree by construction rather
 * than by two pieces of code doing the same arithmetic.
 *
 * The window is the reason the timeline can hold a feature film at all. A two
 * hour score is around 45,000 curve points; drawing all of them, all the time,
 * is what the previous timeline did and why it could not scale. Nothing off
 * screen is ever drawn.
 */

import { clamp, round6, type Seconds, type Fps } from './time';

/* The default view spans a tenth of the film. A whole feature at once is a
 * smear that answers no question anyone actually has, and it is also the
 * expensive thing to draw. Zooming out to the whole film is still allowed —
 * the renderer switches to an envelope per pixel column and stays cheap — but
 * it is somewhere you go deliberately, not where you start. */
export const DEFAULT_VIEW_FRACTION = 0.1;

/* Zooming in stops when a frame is about 40 pixels wide on a typical lane.
 * Past that there is nothing further to see and the numbers start to lose
 * meaning against a video that cannot seek that precisely anyway. */
export const MIN_SPAN_FRAMES = 24;

export interface Range {
  start: Seconds;
  end: Seconds;
}

export class TimeView {
  /** Total length of the film, in seconds. */
  duration: Seconds;
  /** Left edge of the window. */
  start: Seconds;
  /** How much time the window shows. */
  span: Seconds;
  fps: Fps;

  constructor(duration: Seconds, fps: Fps) {
    this.duration = Math.max(0.001, duration);
    this.fps = fps > 0 ? fps : 24;
    this.span = this.defaultSpan();
    this.start = 0;
  }

  /**
   * Where the view starts: a tenth of the film — unless a tenth is smaller
   * than the closest we ever zoom, in which case show the whole thing.
   *
   * Without that second clause a thirty second test clip opens showing one
   * second of itself, which looks broken rather than deliberate. The rule is
   * "don't render the whole film by default", and for a film that is barely
   * longer than one window there is nothing to save by obeying it.
   */
  defaultSpan(): Seconds {
    const tenth = this.duration * DEFAULT_VIEW_FRACTION;
    return tenth < this.minSpan() ? this.duration : tenth;
  }

  minSpan(): Seconds {
    return Math.min(this.duration, MIN_SPAN_FRAMES / this.fps);
  }

  get end(): Seconds {
    return this.start + this.span;
  }

  /** How much of the whole film is on screen, 0 to 1. */
  get fraction(): number {
    return this.span / this.duration;
  }

  /** Is the whole film visible? */
  get fitted(): boolean {
    return this.span >= this.duration - 1e-6;
  }

  /* --- mapping --- */

  /** Where a moment falls, as a fraction of the window. Can be outside 0..1. */
  fractionOf(t: Seconds): number {
    return (t - this.start) / this.span;
  }

  /** Where a moment falls in a lane `width` pixels wide. */
  toX(t: Seconds, width: number): number {
    return this.fractionOf(t) * width;
  }

  /** What moment a pixel is on. */
  fromX(x: number, width: number): Seconds {
    return this.start + (x / Math.max(1, width)) * this.span;
  }

  /** How many seconds one pixel covers. The renderer's detail switch. */
  secondsPerPixel(width: number): number {
    return this.span / Math.max(1, width);
  }

  /** Is any of this range on screen? The test every draw loop starts with. */
  intersects(a: Seconds, b: Seconds): boolean {
    return b >= this.start && a <= this.end;
  }

  /* --- moving --- */

  /**
   * Clamp the window back inside the film.
   *
   * Allows the window to be larger than the film — it is then centred on it —
   * because forbidding that makes a short clip behave differently from a long
   * one for no reason a user would understand.
   */
  clampInside(): this {
    this.span = clamp(this.span, this.minSpan(), this.duration);
    this.start = clamp(this.start, 0, Math.max(0, this.duration - this.span));
    this.start = round6(this.start);
    this.span = round6(this.span);
    return this;
  }

  set(start: Seconds, span: Seconds): this {
    this.start = start;
    this.span = span;
    return this.clampInside();
  }

  fit(): this {
    return this.set(0, this.duration);
  }

  reset(): this {
    return this.set(0, this.defaultSpan());
  }

  pan(deltaSeconds: Seconds): this {
    this.start += deltaSeconds;
    return this.clampInside();
  }

  /** Pan by a fraction of the visible window, for wheel and key scrolling. */
  panByFraction(f: number): this {
    return this.pan(this.span * f);
  }

  /**
   * Zoom about a point, keeping the moment under it still.
   *
   * `anchor` is where the cursor is as a fraction of the window, so zooming on
   * the wheel keeps whatever is under the pointer under the pointer. That is
   * the single interaction that makes a timeline feel like a map rather than a
   * pair of buttons.
   */
  zoomAt(anchor: number, factor: number): this {
    const at = this.start + this.span * clamp(anchor, 0, 1);
    const wanted = clamp(this.span * factor, this.minSpan(), this.duration);
    this.start = at - (at - this.start) * (wanted / this.span);
    this.span = wanted;
    return this.clampInside();
  }

  /** Zoom about the centre. */
  zoom(factor: number): this {
    return this.zoomAt(0.5, factor);
  }

  /** Frame a range, with a little air either side so it is not flush. */
  zoomTo(a: Seconds, b: Seconds, pad = 0.08): this {
    const lo = Math.min(a, b);
    const hi = Math.max(a, b);
    const room = Math.max((hi - lo) * pad, this.minSpan() * 0.25);
    return this.set(lo - room, hi - lo + room * 2);
  }

  /**
   * Bring a moment into view, scrolling as little as possible.
   *
   * `edge` keeps it off the very border, so a playhead entering from the right
   * does not sit on the last pixel; and when it has to jump — a seek, not a
   * play-through — it lands the moment a third in, which leaves most of the
   * window showing what happens next.
   */
  reveal(t: Seconds, edge = 0.06): this {
    const margin = this.span * edge;
    if (t < this.start + margin) {
      const far = t < this.start - this.span * 0.5;
      this.start = far ? t - this.span * 0.33 : t - margin;
    } else if (t > this.end - margin) {
      const far = t > this.end + this.span * 0.5;
      this.start = far ? t - this.span * 0.33 : t - this.span + margin;
    } else {
      return this;
    }
    return this.clampInside();
  }

  snapshot(): { start: Seconds; span: Seconds } {
    return { start: this.start, span: this.span };
  }
}

/**
 * Ruler ticks that stay readable at every zoom.
 *
 * Picks a step from a fixed ladder — frames, then seconds, then the usual
 * 5/10/15/30, then minutes — so the labels land on values a person recognises.
 * A naive "divide the width by 100" produces ticks every 3.7 seconds, which is
 * numerically fine and unreadable.
 */
export interface Tick {
  t: Seconds;
  major: boolean;
}

export function ticks(view: TimeView, width: number, targetPx = 90): Tick[] {
  const fps = view.fps;
  const frame = 1 / fps;
  const ladder: Seconds[] = [
    frame, frame * 2, frame * 5, frame * 10,
    1, 2, 5, 10, 15, 30,
    60, 120, 300, 600, 900, 1800, 3600,
  ];
  const wanted = view.secondsPerPixel(width) * targetPx;
  let step = ladder[ladder.length - 1];
  for (const s of ladder) {
    if (s >= wanted) { step = s; break; }
  }
  /* Every fifth tick is major, except in seconds-and-up where the tens are the
   * natural emphasis. */
  const majorEvery = step < 1 ? 5 : step < 60 ? (step === 15 || step === 30 ? 2 : 5) : 5;

  const out: Tick[] = [];
  const first = Math.floor(view.start / step);
  const last = Math.ceil(view.end / step);
  /* A hard ceiling. A pathological width or duration must never be able to
   * push this into a million-iteration loop while the pointer is moving. */
  if (last - first > 4000) return out;

  for (let i = first; i <= last; i++) {
    const t = round6(i * step);
    if (t < 0 || t > view.duration) continue;
    out.push({ t, major: i % majorEvery === 0 });
  }
  return out;
}
