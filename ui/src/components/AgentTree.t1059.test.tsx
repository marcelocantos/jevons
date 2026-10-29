// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentTree } from './AgentTree';

const parked = {
  name: 'jv-t1059-parked', parent: 'jevons-po', running: false,
  stop_reason: 'jevons_agent_stop by marcelo: parked pending owner decision',
  stop_actor: 'marcelo',
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
  afterEach(() => { cleanup(); vi.restoreAllMocks(); });
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
    expect(screen.getByRole('dialog').textContent).toContain('Reason: jevons_agent_stop by marcelo: parked pending owner decision');
    expect(screen.getByRole('dialog').textContent).toContain('Actor: marcelo');
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

  it('measures a long popup in a narrow viewport instead of using a guessed height', () => {
    vi.stubGlobal('innerWidth', 320);
    vi.stubGlobal('innerHeight', 568);
    const nativeRect = Element.prototype.getBoundingClientRect;
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      if (this.classList.contains('agent-stop-icon')) return { top: 520, bottom: 544, left: 285, right: 309, width: 24, height: 24 } as DOMRect;
      if (this.classList.contains('agent-stop-popup')) return { top: 0, bottom: 420, left: 0, right: 304, width: 304, height: 420 } as DOMRect;
      return nativeRect.call(this);
    });
    try {
      setup();
      fireEvent.click(screen.getByRole('button', { name: 'Stop details for ' + parked.name }));
      const popup = screen.getByRole('dialog');
      expect(Number.parseFloat(popup.style.top)).toBe(96);
      expect(Number.parseFloat(popup.style.left)).toBe(8);
      expect(Number.parseFloat(popup.style.top) + 420).toBeLessThanOrEqual(560);
      expect(popup.textContent).toContain('Reason: jevons_agent_stop by marcelo: parked pending owner decision');
      expect(popup.querySelector('.agent-stop-popup-head button')?.textContent).toBe('Close');
      expect(popup.querySelector('.agent-stop-popup-body')).toBeTruthy();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('keeps a non-parked stopped reason accessible in the same disclosure', () => {
    render(<AgentTree agents={[{
      name: 'jv-failed', running: false, stop_reason: 'start failed: launch timed out',
      stopped_at: '2026-10-01T08:00:00Z',
    }]} selected="" onSelect={() => {}} />);
    expect(screen.queryByText('start failed: launch timed out')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Stop details for jv-failed' }));
    expect(screen.getByRole('dialog').textContent).toContain('Reason: start failed: launch timed out');
    expect(screen.getByRole('dialog').textContent).toContain('Since: 2026-10-01T08:00:00Z');
  });


  it('shows the trusted plan-policy actor even when the reason has no by-X syntax', () => {
    render(<AgentTree agents={[{
      name: 'jv-plan', running: false, stop_reason: 'plan policy parked: no eligible destination',
      stop_actor: 'product:plan_policy', stopped_at: '2026-10-01T09:00:00Z',
    }]} selected="" onSelect={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: 'Stop details for jv-plan' }));
    const text = screen.getByRole('dialog').textContent || '';
    expect(text).toContain('Reason: plan policy parked: no eligible destination');
    expect(text).toContain('Actor: product:plan_policy');
    expect(text).toContain('Since: 2026-10-01T09:00:00Z');
  });

});
