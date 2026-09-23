// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement, useState } from 'react';
import { AsideHistoryPanel } from './AsideHistoryPanel';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function Harness() {
  const [open, setOpen] = useState(false);
  return createElement(
    'div',
    null,
    createElement('button', {
      type: 'button',
      id: 'aside-history-btn',
      'aria-expanded': open,
      onClick: () => setOpen((v) => !v),
    }, 'Closed'),
    createElement(AsideHistoryPanel, { open, onClose: () => setOpen(false) }),
  );
}

describe('closed asides', () => {
  it('lists the archive when Closed is pressed', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        asides: [{
          id: 'att-1',
          title: 'Selection is not sticky',
          kind: 'target',
          kind_label: 'Target filing',
          closed_at: '2026-08-09T11:36:56Z',
        }],
        count: 1,
      }),
    }));
    render(createElement(Harness));
    expect(document.getElementById('aside-history-panel')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Closed' }));
    expect(await screen.findByText('Selection is not sticky')).toBeTruthy();
    expect(screen.getByText('Target filing')).toBeTruthy();
    expect(document.getElementById('aside-history-panel')?.classList.contains('open')).toBe(true);
  });
});
