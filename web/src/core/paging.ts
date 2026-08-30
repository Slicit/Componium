/* Showing part of a list, and finding things in it.
 *
 * A library grows and a page of it does not. Ten films is what fits above the
 * fold beside the timeline; a hundred is a wall you scroll past to reach the
 * controls underneath.
 */

export const PAGE_SIZES = [10, 25, 50] as const;
export const DEFAULT_PAGE_SIZE = 10;
/** The page size meaning "do not paginate". */
export const ALL = 0;

/**
 * Which items match a query.
 *
 * Case-insensitive, and matches anywhere in the name rather than only at the
 * start: film files are named for releases, so what a person remembers is
 * usually in the middle — "scargiver", not "Rebel.Moon.Part.Two".
 *
 * Whitespace-separated terms must all match, in any order. Typing "rebel 1080"
 * should find the film whether or not those words are adjacent in its name.
 */
export function matches<T>(items: readonly T[], query: string, name: (item: T) => string): T[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return [...items];
  return items.filter((item) => {
    const haystack = name(item).toLowerCase();
    return terms.every((term) => haystack.includes(term));
  });
}

export interface Page<T> {
  items: T[];
  /** 1-based, and always within range. */
  page: number;
  pages: number;
  /** How many matched before paging, for saying "11 to 20 of 34". */
  total: number;
  first: number;
  last: number;
}

/**
 * One page of a list.
 *
 * The page number is clamped rather than trusted. Deleting the last film on
 * the last page, or typing a filter that matches less than a page, otherwise
 * leaves the view on a page that no longer exists — showing nothing, with no
 * indication that anything is wrong or that going back would help.
 */
export function paginate<T>(items: readonly T[], page: number, size: number): Page<T> {
  const total = items.length;
  if (size === ALL || size <= 0 || total === 0) {
    return { items: [...items], page: 1, pages: 1, total, first: total ? 1 : 0, last: total };
  }
  const pages = Math.max(1, Math.ceil(total / size));
  const clamped = Math.min(Math.max(1, Math.floor(page) || 1), pages);
  const from = (clamped - 1) * size;
  return {
    items: items.slice(from, from + size),
    page: clamped,
    pages,
    total,
    first: from + 1,
    last: Math.min(from + size, total),
  };
}
