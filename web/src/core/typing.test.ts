// @vitest-environment jsdom

import { describe, it, expect } from 'vitest';
import { isTyping } from './typing';

/** An event as it arrives from a real element. */
function pressOn(el: Element): KeyboardEvent {
  const e = new KeyboardEvent('keydown', { key: 'Backspace', bubbles: true, composed: true });
  el.dispatchEvent(e);
  return e;
}

describe('knowing when somebody is typing', () => {
  it('an input is', () => {
    const input = document.createElement('input');
    document.body.append(input);
    let seen = false;
    window.addEventListener('keydown', (e) => { seen = isTyping(e); }, { once: true });
    pressOn(input);
    expect(seen).toBe(true);
  });

  it('a canvas is not', () => {
    const canvas = document.createElement('canvas');
    document.body.append(canvas);
    let seen = true;
    window.addEventListener('keydown', (e) => { seen = isTyping(e); }, { once: true });
    pressOn(canvas);
    expect(seen).toBe(false);
  });

  it('an input inside a shadow root is, even though target says otherwise', () => {
    /* The bug this file exists for. An event crossing a shadow boundary is
     * retargeted to the host, so `target.tagName` reads as the custom element
     * and a guard written against it lets the keystroke through to the
     * shortcut layer. Backspace in the flasher's password field was being
     * cancelled and turned into "delete the selection". */
    const host = document.createElement('some-widget');
    document.body.append(host);
    const root = host.attachShadow({ mode: 'open' });
    const input = document.createElement('input');
    root.append(input);

    let target = '';
    let seen = false;
    window.addEventListener('keydown', (e) => {
      target = (e.target as HTMLElement).tagName;
      seen = isTyping(e);
    }, { once: true });
    pressOn(input);

    // The retargeting is real, and is why the old guard could not work.
    expect(target).toBe('SOME-WIDGET');
    expect(seen).toBe(true);
  });

  it('something editable is', () => {
    const div = document.createElement('div');
    /* jsdom does not implement contentEditable behaviour, only the property. */
    Object.defineProperty(div, 'isContentEditable', { value: true });
    document.body.append(div);
    let seen = false;
    window.addEventListener('keydown', (e) => { seen = isTyping(e); }, { once: true });
    pressOn(div);
    expect(seen).toBe(true);
  });

  it('survives an event with no path at all', () => {
    // Synthetic events from a test or a library may have neither.
    expect(isTyping({ target: null } as unknown as Event)).toBe(false);
    expect(isTyping({} as unknown as Event)).toBe(false);
  });
});
