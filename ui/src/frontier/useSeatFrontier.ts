// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useQuery } from '@tanstack/react-query';
import { readyFrontierRows, toFrontierRows } from './table';

/** Seat workdir scopes the ledger when set (🎯T996). Empty cwd uses the
 * daemon's configured frontier workdir — same as owner `fetch('/api/frontier')`. */
export function useSeatFrontier(cwd: string) {
  return useQuery({
    queryKey: ['frontier', cwd],
    queryFn: async ({ signal }) => {
      const qs = cwd ? '?cwd=' + encodeURIComponent(cwd) : '';
      const r = await fetch('/api/frontier' + qs, { signal });
      if (!r.ok) throw new Error('frontier: HTTP ' + r.status);
      const body: unknown = await r.json();
      const ledgerKey = body && typeof body === 'object' ? String((body as { ledger_key?: unknown }).ledger_key ?? '') : '';
      return { rows: readyFrontierRows(toFrontierRows(body)), ledgerKey };
    },
    refetchInterval: 8000,
  });
}
