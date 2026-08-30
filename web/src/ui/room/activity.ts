/* Whether the room needs drawing again.
 *
 * The renderer used to run sixty times a second regardless. Most of those
 * frames produced the identical image: the film paused, no cue firing, the
 * camera still. A full scene draw — and, once the projector exists, a shadow
 * pass with it — to arrive back where it started.
 *
 * The rule has one wrinkle worth keeping honest, which is why it lives here
 * with tests rather than inline: a frame has to be drawn *after* the last one
 * that moved. Otherwise a cue ending leaves the room showing the frame before
 * it ended, and it stays that way until something unrelated happens to ask for
 * a draw. That is a stale picture rather than a saved one.
 */

export class Activity {
  /** Whether the previous frame had anything happening in it. */
  private was = true;
  /** Whether this frame does. */
  private now = false;
  /** Something changed the room from outside the loop and said so. */
  private told = true;

  /** Something is in motion this frame: a cue, the camera, a playing film. */
  moved(): void {
    this.now = true;
  }

  /**
   * The room was changed by something that is not motion.
   *
   * A slider moved, a film was chosen, the camera was placed. These do not
   * repeat, so they are remembered until a frame has been drawn for them.
   */
  changed(): void {
    this.told = true;
  }

  /**
   * Should this frame be drawn? Asked once per frame; answering resets it.
   *
   * True while anything is moving, and once more after everything stops.
   */
  take(): boolean {
    const active = this.now || this.told;
    const draw = active || this.was;
    this.was = active;
    this.now = false;
    this.told = false;
    return draw;
  }
}
