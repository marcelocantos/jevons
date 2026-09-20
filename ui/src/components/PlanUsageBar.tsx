// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { now } from '../clock';
import type { MuxClient } from '../mux/client';
import { PLAN_USAGE_CHANNEL } from '../mux/protocol';
import { CompanyMark, companyOfProvider, windowAbbrev } from '../plan/companyMark';
import { holdLastPlanSnapshot } from '../plan/holdSnapshot';
import { applyThresholds, formatWindow } from '../plan/pace';
import { InstantTip } from './InstantTip';
import { gaugeFillPercent, tickerGroups, type PlanSnapshot } from '../plan/tickerGroups';
import { PlanTipTable } from '../plan/tipTable';

/** HTTP fallback only when mux is not connected (tests / non-cockpit). */
export const PLAN_POLL_MS = 60_000;
export const PLAN_POLL_PENDING_MS = 5_000;

function hasNumericRemaining(snap: PlanSnapshot | undefined): boolean {
  return tickerGroups(snap).some((g) => g.windows.some((w) => typeof w.remaining_percent === 'number'));
}

export function PlanUsageBar(props: { mux?: MuxClient } = {}) {
  useQuery({
    queryKey: ['plan-usage-thresholds'],
    queryFn: async () => {
      const r = await fetch('/api/plan-usage/thresholds');
      if (!r.ok) throw new Error(String(r.status));
      const t = await r.json();
      applyThresholds(t);
      return t;
    },
    staleTime: Infinity,
  });
  const [muxSnap, setMuxSnap] = useState<PlanSnapshot | undefined>(undefined);
  useEffect(() => {
    const mux = props.mux;
    if (!mux) return;
    const unsub = mux.subscribe(PLAN_USAGE_CHANNEL, (env) => {
      if (env.t !== 'frame' || env.body == null || typeof env.body !== 'object') return;
      setMuxSnap(env.body as PlanSnapshot);
    });
    mux.openChannel(PLAN_USAGE_CHANNEL);
    return () => {
      unsub();
      mux.closeChannel(PLAN_USAGE_CHANNEL);
    };
  }, [props.mux]);
  useEffect(() => {
    const ac = new AbortController();
    fetch('/api/plan-usage?refresh=1', { signal: ac.signal }).catch(() => {});
    return () => ac.abort();
  }, []);
  const q = useQuery({
    queryKey: ['plan-usage'],
    queryFn: async ({ signal }) => {
      const r = await fetch('/api/plan-usage', { signal });
      if (!r.ok) throw new Error(String(r.status));
      return (await r.json()) as PlanSnapshot;
    },
    enabled: !props.mux,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
    refetchInterval: (query) => {
      if (props.mux) return false;
      if (query.state.fetchStatus === 'fetching') return false;
      return hasNumericRemaining(query.state.data) ? PLAN_POLL_MS : PLAN_POLL_PENDING_MS;
    },
  });
  const last = useRef<PlanSnapshot | undefined>(undefined);
  const incoming = muxSnap ?? q.data;
  const snap = holdLastPlanSnapshot(last.current, incoming);
  last.current = snap;
  const groups = tickerGroups(snap);
  // 🎯T588.1: a grid, so comparing two providers is a glance along a row.
  const tip = <PlanTipTable groups={groups} nowMs={now()} />;
  const inner = !groups.length ? (
    <span className="plan-chip">{q.data?.pending ? 'plan usage: waiting for the first reading' : ''}</span>
  ) : (
    groups.map((g) => (
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
              // 🎯T670: the gauge fills with what has been spent, like every
              // harness reports it, so bar and chevron both travel rightward.
              // A spent window stays the empty red-bordered bar.
              const used = gaugeFillPercent(w);
              const remainingTime = painted.remainingTimePercent;
              const spentTime = remainingTime == null ? null : 100 - remainingTime;
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
                      <span className="plan-bar-fill" style={{ width: used + '%' }} />
                    </span>
                    {spentTime != null ? (
                      // 🎯T673: position is time spent; the chevron itself is
                      // neutral. It is a ruler mark, not a reading.
                      <span className="plan-tri" aria-hidden="true" style={{ left: spentTime + '%' }} />
                    ) : null}
                  </span>
                  <span className="plan-win-label">{windowAbbrev(w.name || '')}</span>
                </span>
              );
            })}
          </span>
        ) : null}
      </span>
    ))
  );
  return (
    <InstantTip
      id="plan-ticker"
      cardClassName="plan-tip-card"
      placement="below-host"
      content={tip}
    >
      {inner}
    </InstantTip>
  );
}
