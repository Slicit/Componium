/* Where in the studio you are, kept in the address bar.
 *
 * A hash rather than a path, and no router library. The studio is one page
 * served by a Go binary that knows nothing about client routes, so a real path
 * would need the server to serve index.html for every unknown URL and would
 * break the day somebody puts the studio behind a subdirectory. A hash never
 * reaches the server at all.
 *
 * The shape is `#/admin/firmware`: a section and a page inside it, and nothing
 * deeper, because nothing here is deeper than that.
 */

export interface Route {
  /** '' for the studio itself, otherwise the section, e.g. 'admin'. */
  section: string;
  /** The page within the section, or '' for its first one. */
  page: string;
}

/** Read a hash. Tolerant of the several ways one can be written. */
export function parseRoute(hash: string): Route {
  const parts = String(hash || '')
    .replace(/^#/, '')
    .split('/')
    .map((p) => p.trim())
    .filter(Boolean);
  return { section: parts[0] ?? '', page: parts[1] ?? '' };
}

/** The hash for a route, ready to assign to `location.hash`. */
export function routeHash(section: string, page = ''): string {
  if (!section) return '#/';
  return page ? `#/${section}/${page}` : `#/${section}`;
}

/** Whether a nav item should read as current. */
export function isCurrent(route: Route, section: string, page = ''): boolean {
  if (route.section !== section) return false;
  return page ? route.page === page : true;
}
