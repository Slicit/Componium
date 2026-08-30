// @vitest-environment jsdom

/* The library table.
 *
 * The alignment fault these guard against was structural rather than visual:
 * every row laid its buttons out left to right, so a film with no Prepare
 * button put Delete where its neighbour put Rebuild. The column shifted from
 * row to row, and the button under the pointer changed as the list refreshed
 * underneath it. Fixed slots are the fix, and "every row has the same number
 * of slots" is the thing worth asserting — the widths are CSS, which jsdom
 * does not compute, but the structure is not.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { Library } from './Library';

function film(name: string, over: Record<string, unknown> = {}) {
  return { film: name, size: 1024 * 1024, hasScore: false, preview: false, ...over };
}

const many = Array.from({ length: 34 }, (_, i) => film(`film-${String(i).padStart(2, '0')}.mp4`));

function libraryOf(entries: unknown[]) {
  return {
    scores: '/scores', free: 1024 * 1024 * 100,
    canBuild: true, canUpload: true, canPrepare: true,
    current: '', entries,
  };
}

let body: unknown;

beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: true, json: async () => body,
  } as Response)));
  vi.stubGlobal('confirm', vi.fn(() => true));
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); });

async function show(entries: unknown[]) {
  body = libraryOf(entries);
  render(<Library onOpen={() => {}} />);
  await waitFor(() => {
    if (!document.querySelector('.lib-row')) throw new Error('no rows yet');
  });
}

const rows = () => Array.from(document.querySelectorAll('.lib-row'));
const names = () => rows().map((r) => r.querySelector('.lib-name')?.textContent?.split(' ')[0]);

describe('the action column', () => {
  it('gives every row the same slots, whatever buttons it has', async () => {
    // A film with a score and finished chunks has every button; a fresh
    // upload has almost none. Both must occupy the same shape.
    await show([
      film('bare.mp4'),
      film('rich.mp4', {
        hasScore: true, preview: true,
        job: { kind: 'analyse', state: 'failed', progress: 0.5, label: '',
               chunks: [{ index: 0, from: 0, to: 10, state: 'done' }] },
      }),
    ]);
    const counts = rows().map((r) => r.querySelectorAll('.lib-actions .slot').length);
    expect(counts[0]).toBe(counts[1]);
    expect(counts[0]).toBeGreaterThan(1);
  });

  it('keeps an empty slot rather than closing the gap', async () => {
    await show([film('bare.mp4')]);
    const slots = rows()[0].querySelectorAll('.lib-actions .slot');
    const empty = Array.from(slots).filter((s) => s.children.length === 0);
    expect(empty.length).toBeGreaterThan(0);
  });

  it('puts delete in the last slot on every row', async () => {
    await show([film('a.mp4'), film('b.mp4', { hasScore: true, preview: true })]);
    for (const r of rows()) {
      const slots = r.querySelectorAll('.lib-actions .slot');
      const last = slots[slots.length - 1];
      expect(last.querySelector('button')?.getAttribute('aria-label')).toMatch(/^Delete /);
    }
  });
});

describe('icons and words', () => {
  it('uses an icon for delete, not the word', async () => {
    await show([film('a.mp4')]);
    const del = screen.getByLabelText('Delete a.mp4');
    expect(del.querySelector('svg')).not.toBeNull();
    expect(del.textContent).toBe('');
  });

  it('still names the actions that have no settled icon', async () => {
    // Rebuild, Prepare and Reset have no glyph anyone reads the same way
    // twice, so they keep their words. A row of unlabelled symbols would have
    // to be hovered to be understood.
    await show([film('a.mp4', { hasScore: true })]);
    expect(screen.getByText('Rebuild')).toBeTruthy();
    expect(screen.getByText('Prepare')).toBeTruthy();
  });

  it('gives the icon button a label a screen reader can use', async () => {
    await show([film('sintel.mp4')]);
    expect(screen.getByLabelText('Delete sintel.mp4')).toBeTruthy();
  });
});

describe('filtering', () => {
  it('narrows to what matches', async () => {
    await show([film('sintel.mp4'), film('big-buck-bunny.mp4'), film('crab.mp4')]);
    fireEvent.change(screen.getByLabelText('Filter films'), { target: { value: 'bunny' } });
    expect(names()).toEqual(['big-buck-bunny.mp4']);
  });

  it('says so when nothing matches, rather than showing an empty table', async () => {
    await show([film('sintel.mp4')]);
    fireEvent.change(screen.getByLabelText('Filter films'), { target: { value: 'zzz' } });
    expect(rows()).toHaveLength(0);
    expect(screen.getByText(/Nothing matches/)).toBeTruthy();
  });
});

describe('paging', () => {
  it('shows ten at a time by default', async () => {
    await show(many);
    expect(rows()).toHaveLength(10);
    expect(screen.getByText('page 1 of 4')).toBeTruthy();
  });

  it('moves between pages', async () => {
    await show(many);
    expect(names()[0]).toBe('film-00.mp4');
    fireEvent.click(screen.getByLabelText('Next page'));
    expect(names()[0]).toBe('film-10.mp4');
    fireEvent.click(screen.getByLabelText('Previous page'));
    expect(names()[0]).toBe('film-00.mp4');
  });

  it('cannot step off either end', async () => {
    await show(many);
    expect((screen.getByLabelText('Previous page') as HTMLButtonElement).disabled).toBe(true);
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByLabelText('Next page'));
    expect((screen.getByLabelText('Next page') as HTMLButtonElement).disabled).toBe(true);
    expect(rows()).toHaveLength(4);
  });

  it('goes back to the first page when the filter changes', async () => {
    // Staying on page three of a search that now matches two things shows
    // nothing at all, with no clue that going back would help.
    await show(many);
    fireEvent.click(screen.getByLabelText('Next page'));
    fireEvent.change(screen.getByLabelText('Filter films'), { target: { value: 'film-3' } });
    expect(names()[0]).toBe('film-30.mp4');
  });

  it('hides the controls when everything fits', async () => {
    await show([film('a.mp4')]);
    expect(screen.queryByLabelText('Next page')).toBeNull();
  });

  it('remembers the page size but not the filter', async () => {
    await show(many);
    fireEvent.change(screen.getByLabelText('Films per page'), { target: { value: '25' } });
    fireEvent.change(screen.getByLabelText('Filter films'), { target: { value: 'film-1' } });
    expect(rows()).toHaveLength(10);  // film-10 through film-19

    cleanup();
    await show(many);
    expect(rows()).toHaveLength(25);
    expect((screen.getByLabelText('Filter films') as HTMLInputElement).value).toBe('');
  });

  it('can show everything at once', async () => {
    await show(many);
    fireEvent.change(screen.getByLabelText('Films per page'), { target: { value: '0' } });
    expect(rows()).toHaveLength(34);
  });
});
