/* What a run did, and what each part of it cost.
 *
 * An analysis is several passes with wildly different costs — a decode
 * measured in minutes, a model measured in minutes of a GPU, and two passes
 * that finish before you have let go of the mouse. "It took nineteen minutes"
 * is not the useful sentence; which part of it was the nineteen minutes is.
 *
 * It is also the only place the reuse shows. A run that skipped the model
 * looks, from outside, exactly like a run that happened to be quick.
 */

export interface Step {
  name: string;
  started: string;
  seconds: number;
  note?: string;
  state?: string;
}

/** A duration in the shortest form that stays readable. */
export function howLong(seconds: number): string {
  if (!isFinite(seconds) || seconds <= 0) return '';
  if (seconds < 1) return Math.round(seconds * 1000) + 'ms';
  if (seconds < 60) return seconds.toFixed(1) + 's';
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m + 'm' + String(s).padStart(2, '0');
}

/** When it began, in the reader's own clock rather than UTC. */
export function began(started: string): string {
  const t = new Date(started);
  return isNaN(t.getTime())
    ? ''
    : t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function Steps(props: { steps: Step[]; total?: number }) {
  const { steps } = props;
  if (!steps?.length) return null;

  const total = props.total ?? steps.reduce((n, s) => n + (s.seconds || 0), 0);
  /* Bars are drawn against the longest step rather than the total, because
   * the question is which one was slow and a step worth 2% of a run is
   * invisible against the total. */
  const longest = Math.max(...steps.map((s) => s.seconds || 0), 0.001);

  return (
    <div className="steps">
      {steps.map((s, i) => (
        <div
          key={i}
          className={'step' + (s.state === 'failed' ? ' failed' : '')
            + (s.seconds ? '' : ' running')}
          title={s.note || undefined}
        >
          <span className="step-at dim small">{began(s.started)}</span>
          <span className="step-name">{s.name}</span>
          <span className="step-bar">
            <span
              className="step-fill"
              style={{ width: Math.max(2, ((s.seconds || 0) / longest) * 100) + '%' }}
            />
          </span>
          <span className="step-secs dim small">
            {s.seconds ? howLong(s.seconds) : '…'}
          </span>
        </div>
      ))}
      {total > 0 && (
        <div className="step step-total">
          <span className="step-at" />
          <span className="step-name dim small">
            {steps.length} steps
          </span>
          <span className="step-bar" />
          <span className="step-secs small">{howLong(total)}</span>
        </div>
      )}
      {steps.some((s) => s.note) && (
        <ul className="step-notes dim small">
          {steps.filter((s) => s.note).map((s, i) => (
            <li key={i}><strong>{s.name}</strong> — {s.note}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
