// @vitest-environment jsdom

/* Choosing a board, and the ordering that makes it awkward.
 *
 * The picker mounts before the rig has loaded, so its address arrives after it
 * is already on screen. An address that turns out to match no board has to
 * appear then, or the entry shows a dropdown pointing at the wrong board while
 * the value the rig actually holds is invisible and uneditable.
 *
 * Mounted directly because that ordering cannot be produced through the page,
 * where every load is awaited before anything is asserted. Mutation testing is
 * what found the gap: hiding the field for a late address broke nothing.
 */

import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { BoardPicker } from './Devices';

const boards = [
  { name: 'cinema', addr: '192.168.1.99:5570' },
  { name: 'bench', addr: '192.168.1.145:5570' },
];

afterEach(cleanup);

describe('the board picker', () => {
  it('asks for a typed address when there are no boards', () => {
    // Every installation, until somebody attaches the first one.
    render(
      <BoardPicker boards={[]} value="" disabled={false} label="Instrument 1"
        onChange={() => {}} />,
    );
    expect(screen.getByLabelText('Instrument 1 address')).toBeTruthy();
    expect(screen.queryByLabelText('Instrument 1 board')).toBeNull();
  });

  it('shows an address that arrives after it is on screen', () => {
    const { rerender } = render(
      <BoardPicker boards={boards} value="" disabled={false} label="Instrument 1"
        onChange={() => {}} />,
    );
    expect(screen.queryByLabelText('Instrument 1 address')).toBeNull();

    rerender(
      <BoardPicker boards={boards} value="10.0.0.5:5570" disabled={false}
        label="Instrument 1" onChange={() => {}} />,
    );
    const field = screen.getByLabelText('Instrument 1 address') as HTMLInputElement;
    expect(field.value).toBe('10.0.0.5:5570');
  });

  it('selects the board an address belongs to', () => {
    render(
      <BoardPicker boards={boards} value="192.168.1.145:5570" disabled={false}
        label="Instrument 1" onChange={() => {}} />,
    );
    expect((screen.getByLabelText('Instrument 1 board') as HTMLSelectElement).value)
      .toBe('192.168.1.145:5570');
    // And no free field, because the address is a board and saying it twice
    // invites the two to disagree.
    expect(screen.queryByLabelText('Instrument 1 address')).toBeNull();
  });

  it('hands up the address of the board that was chosen', () => {
    const chosen: string[] = [];
    render(
      <BoardPicker boards={boards} value="" disabled={false} label="Instrument 1"
        onChange={(a) => chosen.push(a)} />,
    );
    fireEvent.change(screen.getByLabelText('Instrument 1 board'),
      { target: { value: '192.168.1.99:5570' } });
    expect(chosen).toEqual(['192.168.1.99:5570']);
  });

  it('opens a field when somebody wants an address that is on no list', () => {
    render(
      <BoardPicker boards={boards} value="192.168.1.99:5570" disabled={false}
        label="Instrument 1" onChange={() => {}} />,
    );
    fireEvent.change(screen.getByLabelText('Instrument 1 board'), { target: { value: '' } });
    expect(screen.getByLabelText('Instrument 1 address')).toBeTruthy();
  });

  it('lets two entries be pointed at one board', () => {
    /* One ESP32 carrying a fan and a strip, which is what ADR 0007 is for. The
     * picker offers every board to every entry and knows nothing about which
     * are already spoken for, which is the correct amount to know. */
    const first: string[] = [];
    const second: string[] = [];
    render(
      <>
        <BoardPicker boards={boards} value="" disabled={false} label="Instrument 1"
          onChange={(a) => first.push(a)} />
        <BoardPicker boards={boards} value="" disabled={false} label="Instrument 2"
          onChange={(a) => second.push(a)} />
      </>,
    );
    fireEvent.change(screen.getByLabelText('Instrument 1 board'),
      { target: { value: '192.168.1.99:5570' } });
    fireEvent.change(screen.getByLabelText('Instrument 2 board'),
      { target: { value: '192.168.1.99:5570' } });
    expect(first).toEqual(['192.168.1.99:5570']);
    expect(second).toEqual(['192.168.1.99:5570']);
  });
});
