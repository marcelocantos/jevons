// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AgentTree, agentRowsFromAPI, filterPanelAgents, type AgentRow } from './AgentTree';

describe('AgentTree panel filter (🎯T1121)', () => {
  const fixture: AgentRow[] = [
    { name: 'run-a', running: true, visible: true, class: 'live' },
    { name: 'run-b', running: true, visible: true, class: 'live' },
    { name: 'run-c', running: true, visible: true, class: 'live' },
    {
      name: 'park-owner',
      visible: true,
      class: 'park',
      panel_line: 'waiting on owner merge approval',
    },
    {
      name: 'po-dead',
      visible: true,
      class: 'fault',
      panel_line: 'should be running; seat is down',
    },
    ...Array.from({ length: 20 }, (_, i) => ({
      name: `reaped-${i}`,
      visible: false,
      class: 'history',
      stop_reason: 'kill',
    })),
  ];

  it('renders five actionable rows by default', () => {
    const filtered = filterPanelAgents(fixture, false);
    expect(filtered).toHaveLength(5);
    const view = render(
      createElement(AgentTree, {
        agents: fixture,
        selected: '',
        onSelect: () => {},
        showHistory: false,
      }),
    );
    expect(view.container.querySelectorAll('.agent-node')).toHaveLength(5);
  });

  it('shows all rows when history is on', () => {
    const view = render(
      createElement(AgentTree, {
        agents: fixture,
        selected: '',
        onSelect: () => {},
        showHistory: true,
      }),
    );
    expect(view.container.querySelectorAll('.agent-node')).toHaveLength(25);
  });

  // fc352eab rewrote App's /api/agents mapper from a stale read and dropped
  // class/visible/panel_line, so 136 reaped rows showed on 2026-10-22.
  it('keeps the daemon panel fields when mapping /api/agents', () => {
    const rows = agentRowsFromAPI([
      { name: 'r', class: 'history', visible: false, panel_line: '', stop_reason: 'kill' },
      { name: 'p', class: 'park', visible: true, panel_line: 'waiting on owner' },
      { name: '' },
      null,
    ]);
    expect(rows.map((r) => r.name)).toEqual(['r', 'p']);
    expect(rows[0]).toMatchObject({ class: 'history', visible: false });
    expect(rows[1]).toMatchObject({ class: 'park', panel_line: 'waiting on owner' });
    expect(filterPanelAgents(rows, false).map((r) => r.name)).toEqual(['p']);
  });

  it('hides history and toggles it without caller wiring', () => {
    window.localStorage?.removeItem('fleet-panel-history');
    const view = render(createElement(AgentTree, { agents: fixture, selected: '', onSelect: () => {} }));
    expect(view.container.querySelectorAll('.agent-node')).toHaveLength(5);
    const toggle = view.container.querySelector('.fleet-history-toggle') as HTMLElement;
    expect(toggle.textContent).toBe('History');
    fireEvent.click(toggle);
    expect(view.container.querySelectorAll('.agent-node')).toHaveLength(25);
    if (window.localStorage) expect(window.localStorage.getItem('fleet-panel-history')).toBe('1');
    expect(toggle.textContent).toBe('Hide history');
    fireEvent.click(toggle);
    expect(view.container.querySelectorAll('.agent-node')).toHaveLength(5);
  });
});
