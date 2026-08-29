/* Timecode handling, kept separate from the page so it can be tested under
 * node without a DOM.
 *
 * These accept and produce the same forms the Go parser does. A person typing
 * a time in the studio should get the same result as typing it in the file,
 * and a mismatch here would be invisible until a cue landed in the wrong
 * place.
 */

'use strict';

function pad(n) { return String(n).padStart(2, '0'); }

function toTimecode(seconds) {
  if (!(seconds >= 0)) seconds = 0;
  /* Round to whole milliseconds first and decompose afterwards. The other
   * order needs a carry when .9995 rounds up, and getting that wrong produces
   * timecodes like 00:00:60.000. */
  const total = Math.round(seconds * 1000);
  const h = Math.floor(total / 3600000);
  const m = Math.floor((total % 3600000) / 60000);
  const s = Math.floor((total % 60000) / 1000);
  const ms = total % 1000;
  return pad(h) + ':' + pad(m) + ':' + pad(s) + '.' + String(ms).padStart(3, '0');
}

function fromTimecode(text) {
  text = String(text).trim();
  if (!text) return null;
  if (text.indexOf(':') === -1) {
    const n = Number(text);
    return Number.isFinite(n) && n >= 0 ? n : null;
  }
  const parts = text.split(':');
  if (parts.length > 3) return null;
  let total = 0;
  for (const part of parts) {
    if (part.trim() === '') return null;
    const v = Number(part);
    if (!Number.isFinite(v) || v < 0) return null;
    total = total * 60 + v;
  }
  return total;
}

function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { toTimecode: toTimecode, fromTimecode: fromTimecode, clamp01: clamp01 };
}
