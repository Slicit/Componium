/* What the model said about a film, as something a person can judge.
 *
 * The description is the one pass that costs a GPU, and the only one that
 * cannot be repeated once a film has been analysed and put away. Deciding
 * whether to pay for it again is therefore a real decision, and it was
 * impossible to make: the file has been written beside every score since the
 * vision seam existed, and there has never been a way to look at it without a
 * shell on the box.
 */

export interface Observation {
  t: number;
  labels?: string[];
  seen?: string;
}

/** Labels that describe the scene rather than name an effect in it. */
const SCENE = 'scene-';

export interface Tally {
  label: string;
  count: number;
}

/**
 * How many times each label was seen, commonest first.
 *
 * The summary that answers the question actually being asked — did it find the
 * things this film has in it — without reading three thousand lines. A film
 * with one `dust` in it is the shape of a description that was looked at too
 * thinly, and a tally shows that in a glance where a list does not.
 *
 * Ties break alphabetically so the order is stable between two runs of the
 * same film, which is the comparison this exists for.
 */
export function tally(observations: readonly Observation[]): Tally[] {
  const counts = new Map<string, number>();
  for (const o of observations ?? []) {
    for (const label of o.labels ?? []) {
      counts.set(label, (counts.get(label) ?? 0) + 1);
    }
  }
  return [...counts.entries()]
    .map(([label, count]) => ({ label, count }))
    .sort((a, b) => (b.count - a.count) || a.label.localeCompare(b.label));
}

/** Effects the model named, as opposed to how it described the scene. */
export function effects(t: readonly Tally[]): Tally[] {
  return t.filter((x) => !x.label.startsWith(SCENE));
}

/** How it described the scene: calm, active, and so on. */
export function scenes(t: readonly Tally[]): Tally[] {
  return t.filter((x) => x.label.startsWith(SCENE));
}

/**
 * Observations whose labels or sentence contain this text.
 *
 * Searching the sentence and not only the labels is the point. A description
 * that says "sand kicked up by the crabs" and labels it nothing at all is the
 * interesting case — it means the model saw the thing and the vocabulary did
 * not catch it, which is a mapping problem rather than a model one, and no
 * amount of looking at labels will show it.
 */
export function matching(
  observations: readonly Observation[],
  query: string,
): Observation[] {
  const want = (query ?? '').trim().toLowerCase();
  if (!want) return [...(observations ?? [])];
  return (observations ?? []).filter((o) =>
    (o.seen ?? '').toLowerCase().includes(want)
    || (o.labels ?? []).some((l) => l.toLowerCase().includes(want)));
}

/**
 * The share of frames the model said nothing about.
 *
 * High is not automatically wrong — most of a film is somebody talking — but
 * it is the number that separates "the film is calm" from "the frames were
 * too large to read", which is a distinction worth being able to make after
 * an hour of GPU time.
 */
export function quietShare(observations: readonly Observation[]): number {
  const all = observations ?? [];
  if (!all.length) return 0;
  const quiet = all.filter(
    (o) => !(o.labels ?? []).some((l) => !l.startsWith(SCENE))).length;
  return quiet / all.length;
}
