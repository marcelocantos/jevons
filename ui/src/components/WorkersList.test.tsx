// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement } from 'react';
import { WorkersList, workersLiveLabel } from './WorkersList';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('workers list', () => {
  it('says none yet only when the API list is empty', () => {
    expect(workersLiveLabel([])).toBe('NONE YET');
    expect(workersLiveLabel([{ id: 'a', status: 'completed' }])).toBe('none live');
    expect(workersLiveLabel([{ id: 'a', status: 'running' }])).toBe('1 live');
  });

  it('shows finished workers instead of a permanent none-yet', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{
        id: '65733b05',
        status: 'completed',
        task: 'run bin/gate show\nand nothing else',
        started_at: '2026-09-12T14:18:38Z',
        outcome: 'GATE green',
      }],
    }));
    render(createElement(WorkersList));
    expect(await screen.findByText('65733b05')).toBeTruthy();
    expect(screen.getByText('none live')).toBeTruthy();
    expect(screen.getByText('run bin/gate show')).toBeTruthy();
    expect(screen.queryByText('NONE YET')).toBeNull();
  });
});
