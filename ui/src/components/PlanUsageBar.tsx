// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef, useState } from 'react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
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
import { OverrideBlockIcon } from '../plan/OverrideBlockIcon';
import { overrideMark, overrideTipHeading } from '../plan/overrideMark';

/** HTTP fallback only when mux is not connected (tests / non-cockpit). */
export const PLAN_POLL_MS = 60_000;
export const PLAN_POLL_PENDING_MS = 5_000;
const PLAN_DECISIONS_POLL_MS = 15_000;
const PLAN_AUTH_POLL_MS = 30_000;

type PlanAuth = { provider: string; state: string; detail?: string };

/** 🎯T924: the broker names plans by subscription id; the bar by plan. */
const PLAN_OF_SUBSCRIPTION: Record<string, string> = {
  anthropic: 'claude',
  'openai-codex': 'codex',
  'xai-oauth': 'grok',
  cursor: 'cursor',
};

const LOGIN_STATE: Record<string, string> = {
  missing: 'no saved login',
  expired: 'login expired',
  rejected: 'login rejected',
};

/** The plan a seat's provider signs in to; plan ids map to themselves. */
function planOfProvider(provider: string): string {
  return PLAN_OF_SUBSCRIPTION[provider] || provider;
}

/** A seat whose provider refused its plan login (🎯T905). */
export type RefusedSeat = { name: string; provider: string };

/**
 * One backend the owner can sign in to again. The menu lists only backends
 * already known to be signed out: a refused login, a failed migration, or a
 * plan with no saved login. Nothing here probes a provider to find out.
 */
type SignInNeed = { plan: string; why: string; url: string };

function loginStateText(p: PlanAuth): string {
  const said = LOGIN_STATE[p.state] || 'login ' + p.state;
  return p.detail ? said + ' (' + p.detail + ')' : said;
}

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

export function PlanUsageBar(props: { mux?: MuxClient; refusedSeats?: readonly RefusedSeat[] } = {}) {
  const queryClient = useQueryClient();
  const [reauthBusy, setReauthBusy] = useState('');
  const [reauthMessage, setReauthMessage] = useState('');
  const [overrideTip, setOverrideTip] = useState('');
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
  // 🎯T924: any plan whose login needs the owner, whether or not a seat or
  // a migration is waiting on it. Reading this never starts a sign-in.
  const planAuth = useQuery({
    queryKey: ['plan-usage-auth'],
    queryFn: async ({ signal }) => {
      const r = await fetch('/api/plan-usage/auth', { signal });
      // An unreadable status is not a verdict: offer nothing rather than guess.
      if (!r.ok) return [] as PlanAuth[];
      const body = await r.json() as { plans?: PlanAuth[] };
      return Array.isArray(body.plans) ? body.plans : [];
    },
    refetchInterval: PLAN_AUTH_POLL_MS,
  });
  const failedMigrations = (decisions.data || []).filter((d) => d.ReauthAvailable);
  const migrationProviders = [...new Set(failedMigrations.map((d) => d.To))];
  const unhealthyLogins = (planAuth.data || [])
    .filter((p) => p.state !== 'ok')
    .map((p) => ({ ...p, plan: PLAN_OF_SUBSCRIPTION[p.provider] || p.provider }))
    .filter((p) => !migrationProviders.includes(p.plan));
  // One entry per backend, however many seats or migrations wait on it.
  const needs: SignInNeed[] = [];
  const planURL = (plan: string) => '/api/plan-usage/auth/recover/' + encodeURIComponent(plan);
  for (const plan of migrationProviders) {
    const names = failedMigrations.filter((d) => d.To === plan).map((d) => d.Name);
    needs.push({ plan, why: 'migration failed for ' + names.join(', '), url: planURL(plan) });
  }
  for (const p of unhealthyLogins) needs.push({ plan: p.plan, why: loginStateText(p), url: planURL(p.plan) });
  // A seat refused on a login the broker still calls healthy is recovered
  // through that seat, which the broker accepts as the evidence.
  for (const seat of props.refusedSeats || []) {
    const plan = planOfProvider(seat.provider);
    if (!plan || needs.some((n) => n.plan === plan)) continue;
    const names = (props.refusedSeats || []).filter((s) => planOfProvider(s.provider) === plan).map((s) => s.name);
    needs.push({
      plan,
      why: 'login refused for ' + names.join(', '),
      url: '/api/agents/' + encodeURIComponent(seat.name) + '/auth/recover',
    });
  }
  const signIn = async (need: SignInNeed) => {
    setReauthBusy(need.plan);
    setReauthMessage('');
    try {
      const r = await fetch(need.url, { method: 'POST' });
      const body = await r.json().catch(() => ({})) as { error?: string };
      if (!r.ok) throw new Error(body.error || 'Claudia could not recover the login');
      setReauthMessage(migrationProviders.includes(need.plan)
        ? 'Claudia recovered ' + need.plan + '; migration will retry automatically.'
        : 'Claudia recovered the ' + need.plan + ' login.');
    } catch (err) {
      setReauthMessage(err instanceof Error ? err.message : 'Authentication recovery failed');
    } finally {
      setReauthBusy('');
      await Promise.all([
        decisions.refetch(),
        planAuth.refetch(),
        queryClient.invalidateQueries({ queryKey: ['agents'] }),
      ]);
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
  const overridden = groups.find((g) => g.provider === overrideTip && g.override)?.override;
  const tip = overridden ? (
    <div className="plan-override-card">
      <strong>{overrideTipHeading(overrideTip, overridden)}</strong>
      <div className="plan-override-reason">{overridden.reason}</div>
      <div className="plan-override-note">The bars still show real usage.</div>
    </div>
  ) : <>
    <PlanTipTable groups={groups} nowMs={now()} />
    {failedMigrations.length ? (
      <div className="plan-migration-failures">
        <strong>Claudia could not switch these running agents:</strong>
        {failedMigrations.map((d) => (
          <div key={d.Name}>
            {d.Name}: {d.From} → {d.To} — {migrationFailureSummary(d.Failure)}
          </div>
        ))}
      </div>
    ) : null}
    {unhealthyLogins.length ? (
      <div className="plan-login-health">
        <strong>These plan logins need you to sign in:</strong>
        {unhealthyLogins.map((p) => (
          <div key={p.provider}>{p.plan}: {loginStateText(p)}</div>
        ))}
      </div>
    ) : null}
  </>;
  const inner = <>
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
            {g.override ? (
              // 🎯T948: the band is the owner's, not the readings'. The
              // ticker's one tip card says why while the pointer is on the mark.
              // 🎯T979: a forced band paints a ? in that band's colour
              // (data-band, cockpit.css). 🎯T982: an outright block is a
              // prohibition sign (ring with a diagonal, the ⊘ family) in the
              // neutral text colour, never a colour that could read as "fine".
              <span
                className={'plan-override plan-override-' + overrideMark(g.override).kind}
                data-band={overrideMark(g.override).band || undefined}
                aria-label={'Override: ' + g.override.reason}
                onPointerEnter={() => setOverrideTip(g.provider)}
                onPointerLeave={() => setOverrideTip('')}
              >{overrideMark(g.override).kind === 'block' ? <OverrideBlockIcon /> : '?'}</span>
            ) : null}
          </span>
        ) : null}
      </span>
    ))}
  </>;
  // 🎯T945: visible only while some backend is known to be signed out; it opens on
  // hover (or keyboard focus) and lists each one.
  const menu = needs.length ? (
    <span id="plan-reauth-menu" className="plan-reauth-menu">
      <button type="button" className="plan-reauth-trigger" aria-haspopup="true">
        {'Sign in (' + needs.length + ')'}
      </button>
      <span className="plan-reauth-list" role="menu">
        {needs.map((n) => (
          <button
            key={n.plan}
            type="button"
            role="menuitem"
            className="plan-reauth-item"
            disabled={!!reauthBusy}
            aria-label={'Reauth ' + n.plan + ': ' + n.why}
            onClick={() => void signIn(n)}
          >
            <span className="plan-reauth-plan">{reauthBusy === n.plan ? 'Signing in to ' + n.plan + '…' : n.plan}</span>
            <span className="plan-reauth-why">{n.why}</span>
          </button>
        ))}
        {reauthMessage ? <span className="plan-reauth-message" role="status">{reauthMessage}</span> : null}
      </span>
    </span>
  ) : null;
  return (
    <>
    {menu}
    <InstantTip
      id="plan-ticker"
      cardClassName="plan-tip-card"
      placement="below-host"
      yieldSelectors={['#agents']}
      content={tip}
    >
      {inner}
    </InstantTip>
    </>
  );
}
