// @vitest-environment jsdom

/* Reading what the model said, and deciding whether to pay for it again.
 *
 * The decision is the point. The description is the one part of an analysis
 * that costs a GPU and cannot be recovered once replaced, and until now it was
 * invisible — so the choice between reusing it and running it again was never
 * offered, because the thing the choice is about could not be looked at.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { Vision } from './Vision';

const trace = {
  film: 'crab-rave.mp4',
  made: '2026-08-30 17:04',
  note: 'vision qwen2.5, 96 frames',
  observations: [
    { t: 1, labels: ['water', 'scene-calm'], seen: 'A serene island.' },
    { t: 39.5, labels: ['dust', 'splash', 'scene-active'], seen: 'Crabs kicking up sand.' },
    { t: 60, labels: ['scene-calm'], seen: 'Red crabs on the shore.' },
  ],
};

let body: unknown;
const saved: string[] = [];
let ok = true;

beforeEach(() => {
  body = trace;
  ok = true;
  saved.length = 0;
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (String(url).startsWith('/api/context')) {
      saved.push(String(init?.body ?? ''));
      return { ok: true, json: async () => ({ context: String(init?.body ?? '') }) } as Response;
    }
    return { ok, json: async () => body } as Response;
  }));
  vi.stubGlobal('confirm', vi.fn(() => true));
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function show(over: Partial<Parameters<typeof Vision>[0]> = {}) {
  const onClose = vi.fn();
  const onLookAgain = vi.fn();
  render(
    <Vision film="crab-rave.mp4" fps={25}
      onClose={over.onClose ?? onClose}
      onLookAgain={over.onLookAgain ?? onLookAgain} />,
  );
  return { onClose, onLookAgain };
}

const said = () => Array.from(document.querySelectorAll('.vis-said')).map((n) => n.textContent);

describe('the reading room', () => {
  it('shows every frame the model was given', async () => {
    show();
    await waitFor(() => expect(said().length).toBe(3));
    expect(said()).toContain('Crabs kicking up sand.');
  });

  it('counts what it found, so a thin description is visible at a glance', async () => {
    // One dust in a film full of it is the shape of a film looked at too
    // thinly, and that reads off a tally where it does not off a list.
    show();
    await waitFor(() => expect(document.querySelectorAll('.vis-chip').length)
      .toBeGreaterThan(0));
    const chips = Array.from(document.querySelectorAll('.vis-tally .vis-chip'))
      .map((c) => c.textContent);
    expect(chips.some((c) => c?.startsWith('dust'))).toBe(true);
    expect(chips.some((c) => c?.startsWith('water'))).toBe(true);
  });

  it('says which model answered and when', async () => {
    show();
    await waitFor(() => expect(document.body.textContent).toContain('qwen2.5'));
    expect(document.body.textContent).toContain('3 frames');
  });

  it('says how much of the film it reaches', async () => {
    // A count of frames reads as a description of the film. Against the length
    // it is standing in for, it reads as what it is.
    body = { ...trace, duration: 600 };
    show();
    await waitFor(() => expect(document.body.textContent).toContain('covers'));
    // Last observation is at 60s of a 600s film.
    expect(document.body.textContent).toContain('covers 10%');
  });

  it('warns when it describes only the opening of the film', async () => {
    // The situation this was built to make visible: a trial run of the first
    // few minutes quietly becomes the description of a whole feature, because
    // a rebuild reuses whatever is there.
    body = { ...trace, duration: 600 };
    show();
    await waitFor(() => expect(document.body.textContent).toContain('only the first'));
    expect(document.body.textContent).toContain('never looked at');
  });

  it('does not warn about a description that covers its film', async () => {
    body = { ...trace, duration: 62 };
    show();
    await waitFor(() => expect(document.body.textContent).toContain('covers'));
    expect(document.body.textContent).not.toContain('only the first');
  });

  it('says nothing about coverage when the length is not known', async () => {
    // An older build with no duration recorded. Better to say nothing than to
    // divide by a zero and report that it covers none of the film.
    show();
    await waitFor(() => expect(document.body.textContent).toContain('3 frames'));
    expect(document.body.textContent).not.toContain('covers');
    expect(document.body.textContent).not.toContain('only the first');
  });

  it('finds a word the model used that no label caught', async () => {
    // The case worth having a search for: the model saw sand and the
    // vocabulary did not catch it, which is a mapping problem rather than a
    // model one and is invisible if you only read labels.
    show();
    await waitFor(() => expect(said().length).toBe(3));
    fireEvent.change(document.querySelector('.vis-find')!, { target: { value: 'sand' } });
    await waitFor(() => expect(said()).toEqual(['Crabs kicking up sand.']));
  });

  it('says so when there is nothing to read', async () => {
    body = { film: 'x.mp4', observations: [] };
    show();
    await waitFor(() => expect(document.body.textContent).toContain('Nothing kept'));
  });

  it('says so when it cannot be read at all', async () => {
    ok = false;
    show();
    await waitFor(() => expect(document.body.textContent).toContain('Could not read'));
  });
});

describe('asking the model to look again', () => {
  it('asks first, and says what is being thrown away', async () => {
    const { onLookAgain } = show();
    await waitFor(() => expect(said().length).toBe(3));
    fireEvent.click(Array.from(document.querySelectorAll('button'))
      .find((b) => b.textContent === 'Look again')!);
    expect(window.confirm).toHaveBeenCalled();
    const asked = (window.confirm as unknown as { mock: { calls: string[][] } }).mock.calls[0][0];
    expect(asked).toContain('3 observations');
    expect(asked).toContain('cannot be got back');
    expect(onLookAgain).toHaveBeenCalled();
  });

  it('does nothing at all if the answer is no', async () => {
    vi.stubGlobal('confirm', vi.fn(() => false));
    const { onLookAgain, onClose } = show();
    await waitFor(() => expect(said().length).toBe(3));
    fireEvent.click(Array.from(document.querySelectorAll('button'))
      .find((b) => b.textContent === 'Look again')!);
    expect(onLookAgain).not.toHaveBeenCalled();
    /* And the panel stays, so a mis-click does not also lose the reading. */
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe('getting out again', () => {
  it('closes on escape', async () => {
    const { onClose } = show();
    await waitFor(() => expect(said().length).toBe(3));
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('closes when the backdrop is pressed, but not the panel', async () => {
    const { onClose } = show();
    await waitFor(() => expect(said().length).toBe(3));
    fireEvent.pointerDown(document.querySelector('.modal')!);
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.pointerDown(document.querySelector('.modal-back')!);
    expect(onClose).toHaveBeenCalled();
  });
});

describe('telling the model what the film is', () => {
  const box = () => document.querySelector('#vis-about') as HTMLTextAreaElement;
  const saveButton = () => Array.from(document.querySelectorAll('button'))
    .find((b) => b.textContent === 'Save') as HTMLButtonElement;

  it('shows what has been said already', async () => {
    body = { ...trace, context: 'Space opera.' };
    show();
    await waitFor(() => expect(box()).not.toBeNull());
    expect(box().value).toBe('Space opera.');
  });

  it('says when nothing has been said', async () => {
    show();
    await waitFor(() => expect(box()).not.toBeNull());
    expect(document.body.textContent).toContain('nothing said about this film yet');
  });

  it('cannot be saved until it is changed', async () => {
    body = { ...trace, context: 'Space opera.' };
    show();
    await waitFor(() => expect(box()).not.toBeNull());
    expect(saveButton().disabled).toBe(true);
  });

  it('says it is unsaved, and what saving it will do', async () => {
    // Saving changes nothing on its own. It is read by the next run that
    // looks, and a box that appeared to take effect immediately would be a
    // lie about an expensive pass.
    show();
    await waitFor(() => expect(box()).not.toBeNull());
    fireEvent.change(box(), { target: { value: 'Space opera.' } });
    expect(document.body.textContent).toContain('next time the model looks');
    expect(saveButton().disabled).toBe(false);
  });

  it('saves what was typed', async () => {
    show();
    await waitFor(() => expect(box()).not.toBeNull());
    fireEvent.change(box(), { target: { value: 'A heist film. Guns, trains, a loom.' } });
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toContain('A heist film. Guns, trains, a loom.'));
    await waitFor(() => expect(document.body.textContent).toContain('saved'));
  });

  it('can be cleared', async () => {
    body = { ...trace, context: 'Space opera.' };
    show();
    await waitFor(() => expect(box().value).toBe('Space opera.'));
    fireEvent.change(box(), { target: { value: '' } });
    fireEvent.click(saveButton());
    await waitFor(() => expect(saved).toContain(''));
  });
});
