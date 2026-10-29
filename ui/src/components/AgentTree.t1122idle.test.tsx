// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement } from 'react';
import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { AgentTree, type AgentRow } from './AgentTree';

afterEach(cleanup);

// 🎯T1122: idle leaf workers fold into one line under their parent.
describe('AgentTree idle fold', () => {
  const rows: AgentRow[] = [
    { name: 'jevons-po', running: true, phase: 'working', class: 'live' },
    { name: 'w-busy', parent: 'jevons-po', running: true, phase: 'working', class: 'live' },
    { name: 'w-parked', parent: 'jevons-po', running: false, class: 'park', panel_line: 'waiting on owner' },
    ...Array.from({ length: 5 }, (_, i) => ({ name: `w-idle-${i}`, parent: 'jevons-po', running: true, phase: 'idle', class: 'live' })),
  ];
  const names = (c: HTMLElement) => [...c.querySelectorAll('.agent-node')].map((n) => n.textContent || '');

  it('shows working and faulted workers and folds the idle and parked ones', () => {
    const view = render(createElement(AgentTree, { agents: [...rows, { name: 'w-down', parent: 'jevons-po', running: false, class: 'live', panel_line: 'should be running; seat is down' }], selected: '', onSelect: () => {}, showHistory: false }));
    const shown = names(view.container).join(' | ');
    expect(shown).toContain('w-busy');
    expect(shown).toContain('w-down');
    expect(shown).not.toContain('w-parked');
    expect(shown).not.toContain('w-idle-0');
    const fold = view.container.querySelector('.agent-idle-fold') as HTMLElement;
    expect(fold.textContent).toBe('5 idle, 1 parked — show');
    fireEvent.click(fold);
    expect(names(view.container).join(' | ')).toContain('w-idle-4');
    expect(names(view.container).join(' | ')).toContain('w-parked');
    expect(fold.textContent).toBe('Hide 5 idle, 1 parked');
  });

  it('keeps the selected seat visible even when idle', () => {
    const view = render(createElement(AgentTree, { agents: rows, selected: 'w-idle-2', onSelect: () => {}, showHistory: false }));
    const shown = names(view.container).join(' | ');
    expect(shown).toContain('w-idle-2');
    expect((view.container.querySelector('.agent-idle-fold') as HTMLElement).textContent).toBe('4 idle, 1 parked — show');
  });
});
