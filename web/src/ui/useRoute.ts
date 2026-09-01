/* The address bar, as state.
 *
 * `hashchange` rather than polling, and one listener for the whole app: the
 * route is read in the shell and passed down, so nothing else has to know that
 * navigation is a hash at all.
 */

import { useEffect, useState } from 'react';
import { parseRoute, type Route } from '../core/route';

export function useRoute(): Route {
  const [hash, setHash] = useState(() =>
    typeof location === 'undefined' ? '' : location.hash);

  useEffect(() => {
    const onChange = () => setHash(location.hash);
    globalThis.addEventListener('hashchange', onChange);
    return () => globalThis.removeEventListener('hashchange', onChange);
  }, []);

  return parseRoute(hash);
}
