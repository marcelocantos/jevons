// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useQuery } from '@tanstack/react-query';
import { now } from '../clock';
import { CompanyMark, companyOfProvider, windowAbbrev } from '../plan/companyMark';
import { remainingTimePercent } from '../plan/windowGeom';
import { pixelFixtureActive, pixelFixturePlanUsage } from '../visual/oldCockpitFixture';

type Window = {
  provider?: string;
  name?: string;
  remaining_percent?: number | null;
  resets_at?: string | null;
  limit_window_seconds?: number | null;
  status?: string;
  pace?: string;
};

type Snapshot = {
  pending?: boolean;
  backends?: Array<{
    provider?: string;
    status?: string;
    windows?: Window[];
  }>;
  windows?: Window[];
};

function windowsOf(snap: Snapshot | undefined): Array<Window & { provider?: string }> {
  if (!snap) return [];
  if (Array.isArray(snap.windows) && snap.windows.length) return snap.windows;
  const out: Array<Window & { provider?: string }> = [];
  for (const b of snap.backends || []) {
    for (const w of b.windows || []) {
      out.push({ ...w, provider: w.provider || b.provider });
    }
  }
  return out;
}

const PROVIDER_RANK: Record<string, number> = { claude: 0, codex: 1, grok: 2, bedrock: 3 };

function groups(wins: Window[]): Array<[string, Window[]]> {
  const m = new Map<string, Window[]>();
  for (const w of wins) {
    const p = w.provider || '';
    const list = m.get(p) || [];
    list.push(w);
    m.set(p, list);
  }
  const ordered: Array<[string, Window[]]> = [];
  for (const [p, list] of m) {
    const winsOrdered = list.slice().sort((a, b) => {
      const rank = (n: string) => (n === 'session' ? 0 : n === 'weekly' ? 1 : 2);
      return rank(String(a.name || '')) - rank(String(b.name || ''));
    });
    ordered.push([p, winsOrdered]);
  }
  ordered.sort((a, b) => (PROVIDER_RANK[a[0]] ?? 50) - (PROVIDER_RANK[b[0]] ?? 50));
  return ordered;
}

function paceClass(w: Window): string {
  const p = String(w.pace || '');
  if (p) return 'plan-' + p;
  const r = w.remaining_percent;
  if (typeof r === 'number' && r <= 5) return 'plan-crit';
  if (typeof r === 'number' && r <= 15) return 'plan-low';
  return '';
}

export function PlanUsageBar() {
  const fixture = pixelFixtureActive();
  const q = useQuery({
    queryKey: ['plan-usage', now()],
    enabled: !fixture,
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
  const snap = fixture ? pixelFixturePlanUsage() : q.data;
  const wins = windowsOf(snap).filter(
    (w) => w.status !== 'unavailable' && typeof w.remaining_percent === 'number',
  );
  if (!wins.length) {
    return (
      <div id="plan-ticker" title="Plan remaining — hover for rollover and detail">
        <span className="plan-chip">{!fixture && q.data?.pending ? 'plan usage: waiting for the first reading' : ''}</span>
      </div>
    );
  }
  const g = groups(wins);
  return (
    <div id="plan-ticker" title="Plan remaining — hover for rollover and detail">
      {g.map(([provider, list]) => (
        <span
          key={provider}
          className="plan-group"
          data-provider={provider}
          data-company={companyOfProvider(provider)}
        >
          <span className="plan-icon">
            <CompanyMark provider={provider} />
          </span>
          <span className="plan-box">
            {list.map((w) => {
              const rem = Number(w.remaining_percent) || 0;
              const t = remainingTimePercent(w, now());
              const cls = paceClass(w);
              return (
                <span
                  key={`${provider}-${w.name}`}
                  className={'plan-win' + (cls ? ' ' + cls : '')}
                  data-pace={w.pace || undefined}
                  data-window={w.name}
                >
                  <span className="plan-track">
                    <span className="plan-bar" aria-hidden="true">
                      <span className="plan-bar-fill" style={{ width: rem + '%' }} />
                    </span>
                    {t != null ? (
                      <span className="plan-tri" aria-hidden="true" style={{ left: t + '%' }} />
                    ) : null}
                  </span>
                  <span className="plan-win-label">{windowAbbrev(w.name || '')}</span>
                </span>
              );
            })}
          </span>
        </span>
      ))}
    </div>
  );
}
