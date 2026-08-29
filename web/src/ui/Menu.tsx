/* The right-click menu.
 *
 * Context-sensitive, because a menu that offers every action everywhere is a
 * list rather than a menu: what you can do to a span is not what you can do to
 * a curve point, and showing both halves greyed out teaches nobody anything.
 *
 * Items that cannot apply right now are omitted rather than disabled, with one
 * exception — Split, which is disabled with a reason when the playhead is not
 * inside the span, because "why is Split missing" is a worse question than
 * "why is Split grey".
 */

import { useEffect, useRef } from 'react';

export interface MenuItem {
  label: string;
  /** Shown right-aligned: the keystroke that does the same thing. */
  key?: string;
  run?: () => void;
  /** A reason it cannot run, shown as a tooltip. Implies disabled. */
  why?: string;
  danger?: boolean;
  separator?: false;
}

export type MenuEntry = MenuItem | { separator: true };

export function Menu(props: {
  x: number;
  y: number;
  items: MenuEntry[];
  onClose: () => void;
}) {
  const { x, y, items, onClose } = props;
  const box = useRef<HTMLDivElement>(null);

  /* Close on anything that is not choosing something: a click elsewhere, the
   * escape key, a scroll, or the window losing focus. A menu that outlives its
   * context and then acts on stale state is worse than no menu. */
  useEffect(() => {
    const away = (e: Event) => {
      if (box.current && e.target instanceof Node && box.current.contains(e.target)) return;
      onClose();
    };
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('pointerdown', away, true);
    window.addEventListener('wheel', onClose, { passive: true });
    window.addEventListener('blur', onClose);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('pointerdown', away, true);
      window.removeEventListener('wheel', onClose);
      window.removeEventListener('blur', onClose);
      window.removeEventListener('keydown', key);
    };
  }, [onClose]);

  /* Keep it on screen. Opening near the right edge otherwise puts half the
   * menu past it, and near the bottom puts the destructive items out of
   * reach — which is exactly where a mis-click lands. */
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const dx = Math.min(0, window.innerWidth - (r.right + 8));
    const dy = Math.min(0, window.innerHeight - (r.bottom + 8));
    if (dx || dy) {
      el.style.left = `${x + dx}px`;
      el.style.top = `${y + dy}px`;
    }
  }, [x, y, items]);

  return (
    <div className="menu" ref={box} style={{ left: x, top: y }} role="menu">
      {items.map((item, i) => (
        'separator' in item && item.separator
          ? <div key={'s' + i} className="menu-sep" />
          : (
            <button
              key={item.label + i}
              className={'menu-item' + (item.danger ? ' danger' : '')}
              role="menuitem"
              disabled={!!item.why || !item.run}
              title={item.why}
              onClick={() => { item.run?.(); onClose(); }}
            >
              <span>{item.label}</span>
              {item.key && <kbd>{item.key}</kbd>}
            </button>
          )
      ))}
    </div>
  );
}
