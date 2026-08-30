/* Fitting a film onto the television in the room.
 *
 * The screen in the room is a fixed rectangle, near enough 16:9. Films are
 * not: a scope film is 2.39:1 and a lot of animation is 16:9, and stretching
 * one to fill the other makes the room lie about the thing it is previewing.
 *
 * So the picture is fitted inside the screen and the screen's own dark panel
 * shows around it, which is what a television does and needs no explaining to
 * anyone who has seen one.
 */

/** The screen mesh in the room, as an aspect. Kept in step with Room3D. */
export const SCREEN_ASPECT = 3.22 / 1.82;

export interface Fit {
  x: number;
  y: number;
}

/**
 * How much to shrink the screen in each direction to hold this film.
 *
 * One of the two is always 1: the picture touches the screen's edge on the
 * axis it fills, and is inset on the other. Returns a square fit for anything
 * it cannot measure — a video with no dimensions yet is the normal state for
 * the first few frames after a film is chosen, and guessing wildly there
 * would show a visible snap once the metadata arrived.
 */
export function containScale(screenAspect: number, filmAspect: number): Fit {
  if (!isFinite(screenAspect) || screenAspect <= 0) return { x: 1, y: 1 };
  if (!isFinite(filmAspect) || filmAspect <= 0) return { x: 1, y: 1 };
  if (filmAspect > screenAspect) {
    /* Wider than the screen: full width, and let the panel show above and
     * below. A scope film on a 16:9 television, exactly as it looks. */
    return { x: 1, y: screenAspect / filmAspect };
  }
  return { x: filmAspect / screenAspect, y: 1 };
}

/** The aspect of a video element, or 0 while it has not loaded enough to say. */
export function aspectOf(video: { videoWidth: number; videoHeight: number } | null): number {
  if (!video) return 0;
  const { videoWidth: w, videoHeight: h } = video;
  if (!w || !h) return 0;
  return w / h;
}
