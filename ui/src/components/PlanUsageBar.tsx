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
import {
  gaugeFillPercent,
  holdGroupReadings,
  tickerGroups,
  type LastReading,
  type PlanSnapshot,
} from '../plan/tickerGroups';
import { PlanTipTable } from '../plan/tipTable';

/** HTTP fallback only when mux is not connected (tests / non-cockpit). */
export const PLAN_POLL_MS = 60_000;
export const PLAN_POLL_PENDING_MS = 5_000;
const PLAN_DECISIONS_POLL_MS = 15_000;

type PlanDecision = {
  Name: string;
  From: string;
  To: string;
  Execution: string;
  Failure: string;
  ReauthAvailable: boolean;
};

function hasNumericRemaining(snap: PlanSnapshot | undefined): boolean {
  return tickerGroups(snap).some((g) => g.windows.some((w) => typeof w.remaining_percent === 'number'));
}

function migrationFailureSummary(failure: string): string {
  if (/invalid_grant/i.test(failure)) return 'Destination refresh token was rejected (invalid_grant).';
  return failure.split('\n').find((line) => line.trim())?.trim() || 'Authentication failed.';
}

export function PlanUsageBar(props: { mux?: MuxClient } = {}) {
  const [reauthBusy, setReauthBusy] = useState('');
  const [reauthMessage, setReauthMessage] = useState('');
  const decisions = useQuery({
    queryKey: ['plan-usage-decisions'],
    queryFn: async ({ signal }) => {
      const r = await fetch('/api/plan-usage/decisions', { signal });
      if (!r.ok) throw new Error(String(r.status));
      const body: unknown = await r.json();
      return Array.isArray(body) ? body as PlanDecision[] : [];
    },
    refetchInterval: PLAN_DECISIONS_POLL_MS,
  });
  const failedMigrations = (decisions.data || []).filter((d) => d.ReauthAvailable);
  const recoveryProviders = [...new Set(failedMigrations.map((d) => d.To))];
  const recoverDestination = async (provider: string) => {
    setReauthBusy(provider);
    setReauthMessage('');
    try {
      const r = await fetch('/api/plan-usage/auth/recover/' + encodeURIComponent(provider), { method: 'POST' });
      const body = await r.json() as { error?: string };
      if (!r.ok) throw new Error(body.error || 'Claudia could not recover the login');
      setReauthMessage('Claudia recovered ' + provider + '; migration will retry automatically.');
      await decisions.refetch();
    } catch (err) {
      setReauthMessage(err instanceof Error ? err.message : 'Authentication recovery failed');
    } finally {
      setReauthBusy('');
    }
  };
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
  // 🎯T681: carry each provider's last real reading forward, so a
  // provider that has gone unreadable can say what it last knew and when.
  const heldReadings = useRef<Map<string, LastReading>>(new Map());
  const held = holdGroupReadings(heldReadings.current, tickerGroups(snap), now());
  heldReadings.current = held.last;
  const groups = held.groups;
  // 🎯T588.1: a grid, so comparing two providers is a glance along a row.
  const tip = <>
    <PlanTipTable groups={groups} nowMs={now()} />
    {failedMigrations.length ? (
      <div className="plan-migration-failures">
        <strong>Claudia could not switch these running agents:</strong>
        {failedMigrations.map((d) => (
          <div key={d.Name}>
            {d.Name}: {d.From} → {d.To} — {migrationFailureSummary(d.Failure)}
          </div>
        ))}
        {reauthMessage ? <div role="status">{reauthMessage}</div> : null}
      </div>
    ) : null}
  </>;
  const inner = <>
    {recoveryProviders.map((provider) => (
      <button
        key={provider}
        type="button"
        className="plan-reauth"
        disabled={!!reauthBusy}
        aria-label={'Reauth ' + provider + ' for failed migration'}
        onClick={(e) => {
          e.stopPropagation();
          void recoverDestination(provider);
        }}
      >
        {reauthBusy === provider ? 'Signing in…' : 'Reauth ' + provider}
      </button>
    ))}
    {!groups.length ? (
      <span className="plan-chip">{q.data?.pending ? 'plan usage: waiting for the first reading' : ''}</span>
    ) : groups.map((g) => (
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
        {/* The vendor mark sits inside the provider's box, so the box
            reads as one unit; a provider with no windows has no box and
            keeps the mark on its own. */}
        {g.available && !g.windows.length ? (
          <span className="plan-icon">
            <CompanyMark provider={g.provider} />
          </span>
        ) : null}
        {!g.available ? (
          // 🎯T681: an unreadable provider keeps its place in the row and
          // says so. Painting nothing here was indistinguishable from a
          // provider that is simply idle, and painting an empty bar was
          // indistinguishable from one with no usage at all.
          <span className="plan-box">
            <span className="plan-icon">
              <CompanyMark provider={g.provider} />
            </span>
            <span className="plan-win plan-nodata" data-window="unreadable">
              <span className="plan-track">
                <span className="plan-bar" aria-hidden="true" />
              </span>
              <span className="plan-win-label">?</span>
            </span>
          </span>
        ) : g.windows.length ? (
          <span className="plan-box">
            <span className="plan-icon">
              <CompanyMark provider={g.provider} />
            </span>
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
                  data-model={w.model || undefined}
                >
                  <span className="plan-track">
                    <span className="plan-bar" aria-hidden="true">
                      {/* DO NOT set an inline background here. The server's band
                          is the only colour, via the pace class below and the
                          rules in cockpit.css. fillColorForWindow will happily
                          turn a served "ahead" into green or red. That is the
                          drift: a green bar beside an amber graph. */}
                      <span className="plan-bar-fill" style={{ width: used + '%' }} />
                    </span>
                    {spentTime != null ? (
                      // 🎯T673: position is time spent; the chevron itself is
                      // neutral. It is a ruler mark, not a reading.
                      <span className="plan-tri" aria-hidden="true" style={{ left: spentTime + '%' }} />
                    ) : null}
                  </span>
                  <span className="plan-win-label">{windowAbbrev(w.name || '', w.model || '')}</span>
                </span>
              );
            })}
          </span>
        ) : null}
      </span>
    ))}
  </>;
  return (
    <InstantTip
      id="plan-ticker"
      cardClassName="plan-tip-card"
      placement="below-host"
      yieldSelectors={['#agents']}
      content={tip}
    >
      {inner}
    </InstantTip>
  );
}
