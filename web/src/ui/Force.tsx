/* Forcing a device by hand.
 *
 * One slider per instrument, overriding whatever the score says. It answers
 * "what does 40% of that fan actually look like", which otherwise means
 * hunting for a cue that happens to be that strong, or editing the score and
 * undoing it afterwards.
 *
 * Zero releases rather than forcing off. A slider cannot express "no opinion"
 * on its own, and rather than bolt a checkbox onto every row the bottom of the
 * travel hands the device back to the score. Force-off already exists and is
 * called mute.
 */

import type { Rig } from '../core/score';

export function Force(props: {
  rig: Rig | null;
  forced: Map<string, number>;
  onChange: (next: Map<string, number>) => void;
}) {
  const { rig, forced, onChange } = props;
  const instruments = rig?.instruments ?? [];
  if (!instruments.length) return null;

  const set = (id: string, value: number) => {
    const next = new Map(forced);
    if (value > 0) next.set(id, value / 100); else next.delete(id);
    onChange(next);
  };

  return (
    <div className="force">
      <div className="force-head">
        <span className="dim small">Force a device, 0 releases it back to the score</span>
        <button className="small-btn" onClick={() => onChange(new Map())}>Release all</button>
      </div>
      {instruments.map((inst) => {
        const at = Math.round((forced.get(inst.id) ?? 0) * 100);
        return (
          <label className={'force-row' + (at > 0 ? ' forcing' : '')} key={inst.id}>
            <span className="force-name" title={inst.kind}>{inst.id}</span>
            <input
              type="range" min={0} max={100} step={1} value={at}
              onChange={(e) => set(inst.id, Number(e.target.value))}
            />
            <span className="force-value">{at > 0 ? at + '%' : 'auto'}</span>
          </label>
        );
      })}
    </div>
  );
}
