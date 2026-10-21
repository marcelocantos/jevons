// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { AgentTree, filterPanelAgents, type AgentRow } from './AgentTree';

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
});
