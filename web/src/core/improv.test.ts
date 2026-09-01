// @vitest-environment node

/* The firmware's protocol numbers, against the flasher's own.
 *
 * This exists because of a bug that could not be diagnosed from its symptom.
 * The firmware had the Improv RPC commands written from memory and every one
 * was a slot out: it read 2 as "identify" when 2 is "request current state".
 * The flasher asked what state the board was in, got silence, waited ten
 * seconds and reported no Improv support. Nothing else was wrong. There was
 * nothing in a log, nothing on the board, and nothing to see except a step in
 * a dialog that did not appear.
 *
 * A comment saying "these come from the spec" would not have helped, because
 * the first version had that comment. So the numbers are read out of both
 * sides and compared: `improv-wifi-serial-sdk` is what the flasher actually
 * uses, and it is a dev dependency for exactly this. If the SDK renumbers
 * anything, or somebody edits the firmware from recollection, this fails here
 * rather than on a bench with a board that will not talk.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);

/** The enums, read out of the SDK's own declarations. */
function sdkEnums(): Record<string, Record<string, number>> {
  const path = require.resolve('improv-wifi-serial-sdk/dist/const.d.ts');
  const src = readFileSync(path, 'utf8');
  const out: Record<string, Record<string, number>> = {};
  const blocks = src.matchAll(/enum\s+(\w+)\s*\{([^}]*)\}/g);
  for (const [, name, body] of blocks) {
    const members: Record<string, number> = {};
    for (const [, key, value] of body.matchAll(/(\w+)\s*=\s*(\d+)/g)) {
      members[key] = Number(value);
    }
    out[name] = members;
  }
  return out;
}

/** The #defines, read out of the firmware. */
function firmwareDefines(): Record<string, number> {
  const src = readFileSync(new URL('../../../firmware/esp32/main/improv.c', import.meta.url), 'utf8');
  const out: Record<string, number> = {};
  /* Trailing comments are ordinary in this file, so a value ends where the hex
   * ends rather than where the line does. */
  for (const [, name, value] of src.matchAll(/^#define\s+(\w+)\s+0x([0-9A-Fa-f]+)\b/gm)) {
    out[name] = parseInt(value, 16);
  }
  return out;
}

const sdk = sdkEnums();
const fw = firmwareDefines();

describe('the firmware and the flasher agree on the protocol', () => {
  it('reads both sides at all', () => {
    // A parse that quietly found nothing would make every test below pass.
    expect(Object.keys(sdk.ImprovSerialRPCCommand ?? {}).length).toBeGreaterThan(4);
    expect(Object.keys(fw).length).toBeGreaterThan(8);
  });

  it('the RPC commands', () => {
    const want = sdk.ImprovSerialRPCCommand;
    expect(fw.CMD_WIFI_SETTINGS).toBe(want.SEND_WIFI_SETTINGS);
    expect(fw.CMD_REQUEST_STATE).toBe(want.REQUEST_CURRENT_STATE);
    expect(fw.CMD_REQUEST_INFO).toBe(want.REQUEST_INFO);
    expect(fw.CMD_REQUEST_SCAN).toBe(want.REQUEST_WIFI_NETWORKS);
  });

  it('the packet types', () => {
    const want = sdk.ImprovSerialMessageType;
    expect(fw.TYPE_CURRENT_STATE).toBe(want.CURRENT_STATE);
    expect(fw.TYPE_ERROR_STATE).toBe(want.ERROR_STATE);
    expect(fw.TYPE_RPC).toBe(want.RPC);
    expect(fw.TYPE_RPC_RESULT).toBe(want.RPC_RESULT);
  });

  it('the states', () => {
    const want = sdk.ImprovSerialCurrentState;
    expect(fw.STATE_READY).toBe(want.READY);
    expect(fw.STATE_PROVISIONING).toBe(want.PROVISIONING);
    expect(fw.STATE_PROVISIONED).toBe(want.PROVISIONED);
  });

  it('the errors', () => {
    const want = sdk.ImprovSerialErrorState;
    expect(fw.ERROR_NONE).toBe(want.NO_ERROR);
    expect(fw.ERROR_INVALID_RPC).toBe(want.INVALID_RPC_PACKET);
    expect(fw.ERROR_UNKNOWN_RPC).toBe(want.UNKNOWN_RPC_COMMAND);
    expect(fw.ERROR_CANNOT_JOIN).toBe(want.UNABLE_TO_CONNECT);
  });

  it('answers every command the flasher actually sends', () => {
    /* The four the SDK calls unprompted. Anything the firmware does not
     * answer is a step in the dialog that silently does not appear, which is
     * the exact shape of the bug this file exists for. */
    const src = readFileSync(
      new URL('../../../firmware/esp32/main/improv.c', import.meta.url), 'utf8');
    for (const name of ['CMD_WIFI_SETTINGS', 'CMD_REQUEST_STATE',
                        'CMD_REQUEST_INFO', 'CMD_REQUEST_SCAN']) {
      expect(src, name + ' is defined but never handled')
        .toContain('case ' + name + ':');
    }
  });
});
