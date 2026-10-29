// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { FrontierTable } from '../components/FrontierTable';
import { readyFrontierRows, toFrontierRows } from './table';

/** 🎯T1119: pane shows ready leaves only; badge matches ready count. */
const mixedPayload = {
  targets: [
    { id: 'T1', name: 'Ready A', status: 'identified', ready: true, blocked: false },
    { id: 'T2', name: 'Ready B', status: 'identified', ready: true, blocked: false },
    { id: 'T3', name: 'Ready C', status: 'converging', ready: true, blocked: false },
    { id: 'T4', name: 'Blocked X', status: 'identified', ready: false, blocked: true, depends_on: ['T1'] },
    { id: 'T5', name: 'Blocked Y', status: 'identified', ready: false, blocked: true, depends_on: ['T2'] },
  ],
};

describe('T1119 frontier ready tab', () => {
  it('App paints query rows when no seat is selected (default /api/frontier)', () => {
    const app = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../App.tsx'), 'utf8');
    expect(app).toMatch(/const frontierRows = frontierQ\.data\?\.rows \|\| \[\]/);
    expect(app).not.toMatch(/frontierCwd \? frontierQ\.data\?\.rows/);
  });

  it('filters to ready rows and paints a 3 ready badge', () => {
    const rows = readyFrontierRows(toFrontierRows(mixedPayload));
    expect(rows).toHaveLength(3);
    expect(rows.map((r) => r.id)).toEqual(['T1', 'T2', 'T3']);

    const { container } = render(
      createElement(
        'div',
        null,
        createElement('span', { id: 'rhs-tab-meta', className: 'rhs-tab-meta' }, rows.length + ' ready'),
        createElement(FrontierTable, {
          rows,
          agents: [],
          selectedAgent: '',
          ledgerKey: 'fixture',
          frontierLedger: 'fixture',
          highlightId: '',
        }),
      ),
    );

    expect(container.querySelector('#rhs-tab-meta')?.textContent).toBe('3 ready');
    const bodyRows = container.querySelectorAll('#frontier-table tbody tr');
    expect(bodyRows.length).toBe(3);
  });
});
