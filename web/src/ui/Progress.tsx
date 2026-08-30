/* A progress bar that holds still.
 *
 * The label used to sit beside the bar, and the bar took whatever width the
 * label left it — so every time the label changed, which is every few seconds
 * while a film is analysed, the bar jumped to a new length. A bar that changes
 * size while you watch it is worse than no bar, because the one thing it is
 * for is telling you at a glance whether anything is moving.
 *
 * So the label goes inside. The bar is then a fixed shape whose fill is the
 * only thing that moves, and the text can say as much as it likes without
 * touching the geometry.
 *
 * The fill is drawn under the text rather than clipping it: a filled bar with
 * light text on it and an empty bar with light text on it have to be equally
 * readable, and the simplest way to get that is a fill dim enough to read
 * through. Two copies of the text, one clipped to the fill, is the prettier
 * trick and one more thing to keep in agreement.
 */

export function Progress(props: { value: number; label: string; title?: string }) {
  const pct = Math.max(0, Math.min(100, Math.round((props.value || 0) * 100)));
  return (
    <div
      className="progress"
      role="progressbar"
      aria-valuenow={pct}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={props.label}
      title={props.title ?? props.label}
    >
      <div className="progress-fill" style={{ width: pct + '%' }} />
      <span className="progress-text">
        <span className="progress-what">{props.label}</span>
        <span className="progress-pct">{pct}%</span>
      </span>
    </div>
  );
}
