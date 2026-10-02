// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useQuery } from '@tanstack/react-query';
import { toFrontierRows } from './table';

/** T996: no selection means no request, and repo changes never retain the
 * previous repo's rows. The cwd also isolates late responses in the cache. */
export function useSeatFrontier(cwd: string) {
  return useQuery({
    queryKey: ['frontier', cwd],
    enabled: Boolean(cwd),
    queryFn: async ({ signal }) => {
      const r = await fetch('/api/frontier?cwd=' + encodeURIComponent(cwd), { signal });
      if (!r.ok) throw new Error('frontier: HTTP ' + r.status);
      const body: unknown = await r.json();
      const ledgerKey = body && typeof body === 'object' ? String((body as { ledger_key?: unknown }).ledger_key ?? '') : '';
      return { rows: toFrontierRows(body), ledgerKey };
    },
    refetchInterval: 8000,
  });
}
