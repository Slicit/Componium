// @vitest-environment jsdom

import { describe, it, expect, beforeEach } from 'vitest';
import { SETTINGS, settingOf, writeSetting, clearSetting } from './settings';

beforeEach(() => { localStorage.clear(); });

describe('settings', () => {
  it('answers the default when nobody has said', () => {
    expect(settingOf('roomLight')).toBe(15);
    expect(settingOf('roomWash')).toBe(75);
  });

  it('remembers what was chosen', () => {
    writeSetting('roomLight', 40);
    expect(settingOf('roomLight')).toBe(40);
  });

  it('keeps the keys the studio already wrote', () => {
    /* Renaming one silently resets a number somebody had tuned, which is the
     * rudest possible way to ship a settings page. The wash was stored under
     * this key before there was anywhere to edit it. */
    localStorage.setItem('componium.roomWash', '30');
    expect(settingOf('roomWash')).toBe(30);
  });

  it('treats an out of range value as no value', () => {
    // A number from an older build. Clamping it would keep a value nobody
    // chose; the default is at least a value somebody did.
    localStorage.setItem('componium.roomLight', '600');
    expect(settingOf('roomLight')).toBe(15);
    localStorage.setItem('componium.roomLight', 'blue');
    expect(settingOf('roomLight')).toBe(15);
  });

  it('goes back to the default when cleared', () => {
    writeSetting('roomWash', 10);
    clearSetting('roomWash');
    expect(settingOf('roomWash')).toBe(75);
  });

  it('survives storage being switched off', () => {
    // Private mode throws on both read and write, and the studio has to open.
    const store = globalThis.localStorage;
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() { throw new Error('denied'); },
    });
    try {
      expect(settingOf('roomLight')).toBe(15);
      expect(() => writeSetting('roomLight', 20)).not.toThrow();
      expect(() => clearSetting('roomLight')).not.toThrow();
    } finally {
      Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: store });
    }
  });

  it('every setting has a default inside its own range', () => {
    // The table is the one place these numbers live now. A default outside
    // its range would be read back as "no value" for ever.
    for (const [name, spec] of Object.entries(SETTINGS)) {
      expect(spec.value, name).toBeGreaterThanOrEqual(spec.min);
      expect(spec.value, name).toBeLessThanOrEqual(spec.max);
      expect(spec.hint.length, name).toBeGreaterThan(20);
    }
  });
});
