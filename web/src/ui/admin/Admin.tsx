/* The admin section: a side menu, and one page at a time.
 *
 * Separate from the studio rather than more buttons on its toolbar. The
 * toolbar is a working surface, used with a film open and a hand on the
 * playhead; this is where you go when something is not set up yet, which is a
 * different activity with a different pace.
 */

import { Devices } from './Devices';
import { Nodes } from './Nodes';
import { Firmware } from './Firmware';
import { RoomDefaults } from './RoomDefaults';
import { isCurrent, routeHash, type Route } from '../../core/route';

/** The menu, in the order a rig gets built: what it is, then how it looks. */
export const PAGES = [
  { id: 'devices', label: 'Devices', hint: 'What the loaded rig says is out there' },
  { id: 'boards', label: 'Boards', hint: 'What is physically wired to an ESP32' },
  { id: 'firmware', label: 'Firmware', hint: 'Put the node firmware on an ESP32' },
  { id: 'room', label: 'Room preview', hint: 'How the 3D preview opens' },
] as const;

export type PageId = (typeof PAGES)[number]['id'];

export function Admin({ route }: { route: Route }) {
  /* An unknown page is the first one rather than an error. A hash is a thing
   * people edit and share, and half of one should land somewhere useful. */
  const known = PAGES.some((p) => p.id === route.page);
  const page: PageId = known ? (route.page as PageId) : PAGES[0].id;

  return (
    <div className="adm">
      <nav className="adm-menu" aria-label="Admin sections">
        <ul>
          {PAGES.map((p) => (
            <li key={p.id}>
              <a
                href={routeHash('admin', p.id)}
                className={isCurrent(route, 'admin', p.id) || (!known && p.id === page)
                  ? 'is-current' : ''}
                aria-current={p.id === page ? 'page' : undefined}
                title={p.hint}
              >
                {p.label}
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <div className="adm-body">
        {page === 'devices' && <Devices />}
        {page === 'boards' && <Nodes />}
        {page === 'firmware' && <Firmware />}
        {page === 'room' && <RoomDefaults />}
      </div>
    </div>
  );
}
