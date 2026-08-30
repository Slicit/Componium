/* Things jsdom does not have that the studio uses.
 *
 * Kept to the minimum, and each one is a genuine gap rather than a
 * convenience: a stub that papers over real behaviour would make a passing
 * test mean less than nothing.
 */

/**
 * jsdom implements no PointerEvent at all, so a pointerdown dispatched at a
 * component never reaches React and every interaction silently does nothing.
 * That is not a difference worth designing around — the app is right to use
 * pointer events — so the event type is supplied here instead.
 */
if (typeof window !== 'undefined' && typeof window.PointerEvent === 'undefined') {
  class PointerEventPolyfill extends MouseEvent {
    readonly pointerId: number;
    readonly pointerType: string;
    readonly isPrimary: boolean;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 1;
      this.pointerType = params.pointerType ?? 'mouse';
      this.isPrimary = params.isPrimary ?? true;
    }
  }
  window.PointerEvent = PointerEventPolyfill as unknown as typeof PointerEvent;
  /* Capture is a no-op here: there is no real pointer to capture, and the
   * code under test only ever asks for it inside a try. */
  Element.prototype.setPointerCapture ??= () => {};
  Element.prototype.releasePointerCapture ??= () => {};
  Element.prototype.hasPointerCapture ??= () => false;
}

/**
 * jsdom has no canvas, and says so by throwing from getContext on every
 * render — pages of stack trace around tests that are passing.
 *
 * Returning null is what a browser without a 2D context does, and the timeline
 * already handles it: it draws nothing and carries on. That is the right
 * behaviour to exercise here anyway. What the canvas *would* have drawn is
 * tested properly in render/lanes.test.ts, against the draw list, which is the
 * whole reason the renderer emits data instead of painting directly.
 */
if (typeof HTMLCanvasElement !== 'undefined') {
  HTMLCanvasElement.prototype.getContext = (() => null) as unknown as
    HTMLCanvasElement['getContext'];
}
