/* Whether a keystroke belongs to something being typed into.
 *
 * A shortcut layer has to keep its hands off a text field, and the obvious way
 * to check is `event.target`. That is wrong the moment anything on the page
 * uses a shadow root, because an event crossing a shadow boundary is
 * *retargeted*: the browser rewrites `target` to the shadow host, so an input
 * inside a web component arrives claiming to be an `<esp-web-install-button>`
 * and the guard waves it through.
 *
 * Which is exactly what happened. The browser flasher's Wi-Fi form lives in a
 * shadow root, so backspace, delete and the arrow keys were caught by the
 * studio's transport shortcuts and cancelled. Characters typed fine; you just
 * could not correct one, and the only way to get a password in was to paste it.
 *
 * `composedPath()` is the honest question. It reports the real element the
 * event started on, shadow boundaries and all.
 */

/** The elements a keystroke is text for. */
const TYPED_INTO = /^(INPUT|TEXTAREA|SELECT)$/;

export function isTyping(event: Event): boolean {
  const path = typeof event.composedPath === 'function' ? event.composedPath() : [];
  /* The first entry is the true origin. Walking further would treat every key
   * pressed anywhere inside a form as text, which is not the question. */
  const first = (path[0] ?? event.target) as HTMLElement | null;
  if (!first || typeof first !== 'object') return false;
  if (TYPED_INTO.test(first.tagName ?? '')) return true;
  /* A rich text area is a div that says so. */
  return first.isContentEditable === true;
}
