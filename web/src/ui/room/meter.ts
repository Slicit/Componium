/* How fast the room is drawing, and what a drawing costs.
 *
 * Two numbers rather than one, because the room draws on demand. It renders
 * when something moved and not otherwise, so frames per second on its own says
 * 0 for a still room that is perfectly healthy, and during playback it reports
 * how often the playhead moved rather than how fast the renderer is. Neither
 * of those is the question anybody has when they look at an FPS counter.
 *
 * So: the rate is how often a frame was actually drawn, and the cost is how
 * long one took. A rate far below sixty with a cost far below sixteen
 * milliseconds is a room that is being asked for frames slowly, not a room
 * that cannot keep up, and those are opposite problems.
 */

/** How long a reading covers, in milliseconds. */
const WINDOW = 500;

export class Meter {
  private drawn = 0;
  private spent = 0;
  private since: number;
  private window: number;

  /** Frames actually drawn, per second, over the last window. */
  rate = 0;
  /** Milliseconds the average drawn frame took. */
  cost = 0;

  constructor(now = 0, window: number = WINDOW) {
    this.since = now;
    this.window = window;
  }

  /** A frame was drawn, and it took this long. */
  drew(ms: number, now: number): boolean {
    this.drawn++;
    this.spent += ms;
    return this.tick(now);
  }

  /** A frame came round and was skipped. It still spends time. */
  skipped(now: number): boolean {
    return this.tick(now);
  }

  /** True when the reading moved, so a caller can avoid pointless renders. */
  private tick(now: number): boolean {
    const elapsed = now - this.since;
    if (elapsed < this.window) return false;
    this.rate = (this.drawn * 1000) / elapsed;
    /* Kept from the last window that drew anything. A still room has no new
     * cost to report, and printing zero would say drawing is free. */
    if (this.drawn) this.cost = this.spent / this.drawn;
    this.drawn = 0;
    this.spent = 0;
    this.since = now;
    return true;
  }
}
