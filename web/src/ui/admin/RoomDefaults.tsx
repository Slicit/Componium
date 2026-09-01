/* How the 3D preview opens.
 *
 * Two numbers, and they are here rather than only on the room's own sliders
 * because a slider is where you make a judgement and a setting is where you
 * keep one. The room's sliders still move; this is what they come back to.
 */

import { useState } from 'react';
import {
  SETTINGS, settingOf, writeSetting, clearSetting, type SettingName,
} from '../../core/settings';

function Row({ name }: { name: SettingName }) {
  const spec = SETTINGS[name];
  const [value, setValue] = useState(() => settingOf(name));
  const isDefault = value === spec.value;

  const set = (n: number) => { setValue(n); writeSetting(name, n); };

  return (
    <div className="adm-set">
      <div className="adm-set-head">
        <label htmlFor={'set-' + name}>{spec.label}</label>
        <span className="adm-set-value">{value}{spec.unit}</span>
      </div>
      <input
        id={'set-' + name}
        type="range"
        min={spec.min}
        max={spec.max}
        value={value}
        onChange={(e) => set(Number(e.target.value))}
      />
      <p className="dim small">{spec.hint}</p>
      <button
        className="adm-reset"
        disabled={isDefault}
        onClick={() => { clearSetting(name); setValue(spec.value); }}
        title={isDefault ? 'Already the default' : 'Back to ' + spec.value + (spec.unit ?? '')}
      >
        {isDefault ? 'default' : 'reset to ' + spec.value + (spec.unit ?? '')}
      </button>
    </div>
  );
}

export function RoomDefaults() {
  return (
    <div className="adm-page">
      <h2>Room preview</h2>
      <p className="dim">
        Kept in this browser rather than in the rig, because they are judgements
        about a preview on a particular screen and not facts about the hardware.
        The room opens at these; its own sliders still move from there.
      </p>
      <section className="adm-card">
        <Row name="roomLight" />
        <Row name="roomWash" />
      </section>
    </div>
  );
}
