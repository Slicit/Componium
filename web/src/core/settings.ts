/* The settings a person can change, and where they are kept.
 *
 * One table rather than a `useState` and a `localStorage.getItem` in whichever
 * component happened to need it first. That is how the room ended up with a
 * wash that remembered itself and a brightness that did not, and with the same
 * default number written in four files: knowledge duplicated is knowledge that
 * drifts, and it did.
 *
 * These are per browser on purpose. They are judgements about a preview on a
 * particular screen in a particular room, not facts about the rig, which lives
 * in the rig file where every machine can read it.
 */

export interface Setting {
  /** What it is called in the interface. */
  label: string;
  /** What it does, in one sentence, for the row beneath it. */
  hint: string;
  min: number;
  max: number;
  /** What it is worth when nobody has said. */
  value: number;
  /** How it reads, e.g. '%'. */
  unit?: string;
}

export const SETTINGS = {
  roomLight: {
    label: 'Room light',
    hint: 'How lit the preview room is. Nothing to do with the film on the '
        + 'screen, which is always as bright as its source.',
    min: 0, max: 100, value: 15, unit: '%',
  },
  roomWash: {
    label: 'Ambient wash',
    hint: 'How strongly the two ceiling strips throw the score’s colour '
        + 'into the room. A hint of the scene rather than a light to see by.',
    min: 0, max: 100, value: 75, unit: '%',
  },
} as const satisfies Record<string, Setting>;

export type SettingName = keyof typeof SETTINGS;

/* The keys already in use, kept exactly. Renaming one would silently reset a
 * number somebody had already tuned, which is the rudest possible way to ship
 * a settings page. */
const KEYS: Record<SettingName, string> = {
  roomLight: 'componium.roomLight',
  roomWash: 'componium.roomWash',
};

export function settingOf(name: SettingName): number {
  const spec = SETTINGS[name];
  try {
    const raw = localStorage.getItem(KEYS[name]);
    if (raw === null) return spec.value;
    const n = Number(raw);
    /* A stored value out of range is a stored value from an older build, and
     * the honest answer to one is the default rather than a clamp: clamping
     * keeps a number nobody chose. */
    if (!Number.isFinite(n) || n < spec.min || n > spec.max) return spec.value;
    return n;
  } catch {
    return spec.value;   /* private mode, or storage switched off */
  }
}

export function writeSetting(name: SettingName, value: number): void {
  try { localStorage.setItem(KEYS[name], String(value)); } catch { /* private mode */ }
}

export function clearSetting(name: SettingName): void {
  try { localStorage.removeItem(KEYS[name]); } catch { /* private mode */ }
}
