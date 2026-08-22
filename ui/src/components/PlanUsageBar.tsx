// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useQuery } from '@tanstack/react-query';

type Window = {
  provider?: string;
  name?: string;
  remaining_percent?: number | null;
  status?: string;
};

type Snapshot = {
  backends?: Array<{
    provider?: string;
    status?: string;
    windows?: Window[];
  }>;
  windows?: Window[];
};

function windowsOf(snap: Snapshot | undefined): Window[] {
  if (!snap) return [];
  if (Array.isArray(snap.windows) && snap.windows.length) return snap.windows;
  const out: Window[] = [];
  for (const b of snap.backends || []) {
    for (const w of b.windows || []) {
      out.push({ ...w, provider: w.provider || b.provider });
    }
  }
  return out;
}

export function PlanUsageBar() {
  const q = useQuery({
    queryKey: ['plan-usage'],
    queryFn: async () => {
      const r = await fetch('/api/plan-usage');
      if (!r.ok) throw new Error(String(r.status));
      return (await r.json()) as Snapshot;
    },
    refetchInterval: (query) => {
      const wins = windowsOf(query.state.data);
      const has = wins.some((w) => typeof w.remaining_percent === 'number');
      return has ? 60_000 : 5_000;
    },
  });
  const wins = windowsOf(q.data).filter(
    (w) => w.status !== 'unavailable' && typeof w.remaining_percent === 'number',
  );
  if (!wins.length) {
    return <span className="plan-usage pending">plan: waiting</span>;
  }
  return (
    <span className="plan-usage">
      {wins.map((w) => (
        <span key={`${w.provider}-${w.name}`} className="plan-chip" title={`${w.provider} ${w.name}`}>
          <span className="plan-label">
            {(w.provider || '').slice(0, 2)}/{(w.name || '').slice(0, 1)}
          </span>
          <span className="plan-pct">{Math.round(w.remaining_percent as number)}%</span>
        </span>
      ))}
    </span>
  );
}
