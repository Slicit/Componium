/* What is mounted: the navbar, and whichever section the hash names.
 *
 * The studio stays mounted while the admin is open, hidden rather than
 * unmounted. It holds a score, an undo history, a WebGL context and a playing
 * video, and throwing those away to look at a settings page would mean losing
 * the history and rebuilding the room every time somebody checked which port a
 * node is on. Hidden with `display: none`, so the room stops being asked for
 * frames while nobody is looking at it.
 */

import { App } from '../App';
import { Nav } from './Nav';
import { Admin } from './admin/Admin';
import { useRoute } from './useRoute';

export function Shell() {
  const route = useRoute();
  const admin = route.section === 'admin';

  return (
    <div className="shell">
      <Nav route={route} />
      <div className="shell-body">
        <div className={admin ? 'shell-away' : 'shell-here'} aria-hidden={admin || undefined}>
          <App />
        </div>
        {admin && <Admin route={route} />}
      </div>
    </div>
  );
}
