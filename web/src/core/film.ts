/* Which film a score was made from.
 *
 * Nothing in the score file records it. The metadata carries a duration, a
 * hash and a frame rate, but not the name of the film it came from, so the
 * binding lives entirely in the filename: the analysis writes
 * `<film stem>.componium` next to the film it read. That is the rule
 * `Jobs.ScorePath` applies going one way, and this is the same rule applied
 * going the other.
 *
 * It matters because the studio opens holding a score and no film. Without
 * this the picture pane shows its "pick a film" hint over a score that is
 * plainly already about one particular film, and the timeline scrubs against
 * nothing.
 */

/** Either separator, because the studio may be serving from Windows. */
const SEPARATOR = /[\\/]/;

/** The stem of a path: no directory, no extension. */
function stem(path: string): string {
  const parts = path.split(SEPARATOR);
  const base = parts[parts.length - 1] ?? '';
  const dot = base.lastIndexOf('.');
  /* `dot > 0` rather than `>= 0`: a leading dot is the whole name of a hidden
   * file, not the boundary before an extension. */
  return dot > 0 ? base.slice(0, dot) : base;
}

/**
 * The film in `films` that this score was built from, or '' when none is.
 *
 * Returns a name rather than the entry so the caller can hand it straight to
 * the picker, and '' rather than null so it can be assigned to the select
 * without a branch — an empty value is already how the picker says "none".
 */
export function filmForScore(
  scorePath: string | undefined,
  films: readonly { name: string }[],
): string {
  if (!scorePath) return '';
  const want = stem(scorePath);
  if (!want) return '';
  for (const f of films) {
    if (stem(f.name) === want) return f.name;
  }
  return '';
}
