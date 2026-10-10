// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentTree } from './AgentTree';

const parked = {
  name: 'jv-t1059-parked', parent: 'jevons-po', running: false,
  stop_reason: 'jevons_agent_stop by marcelo: parked pending owner decision',
  stopped_at: '2026-09-30T12:10:00Z',
};

function setup() {
  const onSelect = vi.fn();
  const view = render(<AgentTree agents={[{ name: 'jevons-po', running: true }, parked]} selected="" onSelect={onSelect} />);
  const row = [...view.container.querySelectorAll('.agent-node')].find((n) =>
    n.querySelector('.agent-name')?.textContent === parked.name,
  )!;
  return { ...view, row, onSelect };
}

describe('parked seat disclosure (🎯T1059)', () => {
  afterEach(cleanup);
  it('has one icon immediately after the seat name, with no inline duplicate', () => {
    const { row } = setup();
    expect(row.querySelector('.agent-name')?.nextElementSibling?.classList.contains('agent-stop-wrap')).toBe(true);
    expect(row.querySelectorAll('.agent-stop-icon')).toHaveLength(1);
    expect(row.textContent).not.toContain('parked pending owner decision');
    expect(row.querySelector('.agent-stop-icon')?.textContent).toBe('⛔');
  });

  it('tap opens full reason, actor and since without selecting seat; escape and outside dismiss', () => {
    const { row, onSelect } = setup();
    const icon = screen.getByRole('button', { name: 'Stop details for ' + parked.name });
    fireEvent.click(icon);
    expect(onSelect).not.toHaveBeenCalled();
    expect(icon.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('dialog').textContent).toContain('Reason: parked pending owner decision');
    expect(screen.getByRole('dialog').textContent).toContain('Actor: marcelo (jevons_agent_stop)');
    expect(screen.getByRole('dialog').textContent).toContain('Since: 2026-09-30T12:10:00Z');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(icon);
    fireEvent.click(icon);
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole('dialog')).toBeNull();
    fireEvent.click(row);
    expect(onSelect).toHaveBeenCalledWith(parked.name);
  });
});
