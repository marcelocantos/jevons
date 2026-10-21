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
