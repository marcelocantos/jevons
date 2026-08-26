// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useRef } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { now } from '../clock';
import { CompanyMark, companyOfProvider, windowAbbrev } from '../plan/companyMark';
import { holdLastPlanSnapshot } from '../plan/holdSnapshot';
import { applyThresholds, formatWindow } from '../plan/pace';
import { tickerGroups, tickerTitle, type PlanSnapshot } from '../plan/tickerGroups';
import { pixelFixtureActive, pixelFixturePlanUsage } from '../visual/oldCockpitFixture';

/** Vanilla: 60s once a reading exists; 5s only after a pending long-poll times out. */
export const PLAN_POLL_MS = 60_000;
export const PLAN_POLL_PENDING_MS = 5_000;

function hasNumericRemaining(snap: PlanSnapshot | undefined): boolean {
  return tickerGroups(snap).some((g) => g.windows.some((w) => typeof w.remaining_percent === 'number'));
}

export function PlanUsageBar() {
  const fixture = pixelFixtureActive();
  useQuery({
    queryKey: ['plan-usage-thresholds'],
    enabled: !fixture,
    queryFn: async () => {
      const r = await fetch('/api/plan-usage/thresholds');
      if (!r.ok) throw new Error(String(r.status));
      const t = await r.json();
      applyThresholds(t);
      return t;
    },
    staleTime: Infinity,
  });
  const q = useQuery({
    queryKey: ['plan-usage'],
    enabled: !fixture,
    queryFn: async ({ signal }) => {
      const r = await fetch('/api/plan-usage', { signal });
      if (!r.ok) throw new Error(String(r.status));
      return (await r.json()) as PlanSnapshot;
    },
    placeholderData: keepPreviousData,
    staleTime: 30_000,
    refetchInterval: (query) => {
      if (query.state.fetchStatus === 'fetching') return false;
      return hasNumericRemaining(query.state.data) ? PLAN_POLL_MS : PLAN_POLL_PENDING_MS;
    },
  });
  const last = useRef<PlanSnapshot | undefined>(undefined);
  const incoming = fixture ? pixelFixturePlanUsage() : q.data;
  const snap = holdLastPlanSnapshot(last.current, incoming);
  last.current = snap;
  const groups = tickerGroups(snap);
  if (!groups.length) {
    return (
      <div id="plan-ticker" title="Plan remaining — hover for rollover and detail">
        <span className="plan-chip">{!fixture && q.data?.pending ? 'plan usage: waiting for the first reading' : ''}</span>
      </div>
    );
  }
  return (
    <div id="plan-ticker" title={tickerTitle(groups)}>
      {groups.map((g) => (
        <span
          key={g.provider}
          className={
            'plan-group' +
            (g.available ? '' : ' plan-unavail') +
            (g.stale ? ' plan-stale' : '')
          }
          data-provider={g.provider}
          data-company={companyOfProvider(g.provider)}
        >
          <span className="plan-icon">
            <CompanyMark provider={g.provider} />
          </span>
          {g.windows.length ? (
            <span className="plan-box">
              {g.windows.map((w) => {
                const painted = formatWindow(w, now());
                const rem = Number(w.remaining_percent) || 0;
                const t = painted.remainingTimePercent;
                const cls = painted.className;
                return (
                  <span
                    key={`${g.provider}-${w.name}`}
                    className={'plan-win' + (cls ? ' ' + cls : '')}
                    data-pace={painted.pace || undefined}
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
          ) : null}
        </span>
      ))}
    </div>
  );
}
