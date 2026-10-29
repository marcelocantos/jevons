// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SidebarPanel } from './SidebarPanel';

function withQueryClient(node: ReturnType<typeof createElement>) {
  const client = new QueryClient();
  return createElement(QueryClientProvider, { client }, node);
}

afterEach(cleanup);

describe('frontier refresh', () => {
  it('asks again when Refresh is pressed', () => {
    const onRefresh = vi.fn();
    render(withQueryClient(createElement(SidebarPanel, {
      tab: 'frontier',
      onTab: () => {},
      onRefresh,
      children: 'rows',
    })));
    fireEvent.click(document.getElementById('frontier-refresh')!);
    expect(onRefresh).toHaveBeenCalledOnce();
  });
});

describe('ready count', () => {
  // "176 ready" beside the Questions tab read as 176 pending questions. The
  // count now lives inside the Frontier tab, whichever tab is selected.
  it('renders inside the Frontier tab, never beside another tab', () => {
    const panel = (tab: 'frontier' | 'review') =>
      withQueryClient(createElement(SidebarPanel, { tab, onTab: () => {}, readyCount: 176, children: 'rows' }));
    const { rerender } = render(panel('frontier'));
    const count = () => document.getElementById('rhs-tab-meta');
    expect(count()!.textContent).toBe('176');
    expect(count()!.closest('#rhs-tab-frontier')).not.toBeNull();
    rerender(panel('review'));
    expect(count()!.closest('#rhs-tab-frontier')).not.toBeNull();
    expect(document.getElementById('rhs-tab-review')!.textContent).toBe('Questions');
  });
});
