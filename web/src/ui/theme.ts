/* The palette, in one place, because the canvas cannot read CSS variables.
 *
 * Everything drawn to the canvas takes its colours from here and everything
 * drawn in the DOM takes them from the matching custom properties in
 * index.css. Two lists that must agree is a smell, but the alternative —
 * reading computed styles per frame — costs a layout flush on every draw.
 */
import type { Theme } from '../render/lanes';

export const dark: Theme = {
  ink: '#e4e9f0',
  muted: '#8c96a5',
  line: '#232b35',
  grid: '#2c3542',
  event: '#d8a24a',
  eventSoft: '#141920',
  playhead: '#f0f4fa',
  warn: '#d66e63',
  channel: {
    r: '#e3736b',
    g: '#79c073',
    b: '#6b93e3',
    intensity: '#d8a24a',
    heave: '#c99ae0',
    surge: '#7fc7c2',
  },
};

/**
 * A colour per instrument kind, for the overview strip.
 *
 * Without this every curve track came out the same red, because the strip fell
 * back to the red channel's colour — so a stack of eight bands said nothing
 * about which was which, which is most of what a map is for.
 */
export const kindColour: Record<string, string> = {
  light: '#e8b45e',
  wind: '#7fc7c2',
  shake: '#e08a5c',
  motion: '#c99ae0',
  mist: '#8fb8d8',
  fog: '#9aa8bd',
  water: '#6bb4e3',
  scent: '#c79ae0',
};
