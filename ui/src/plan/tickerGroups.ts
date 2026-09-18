// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Same bar-membership rules as web/scripts/plan_usage.js (🎯T390.1). */

export type PlanHistoryPoint = {
  at: string;
  remaining_percent: number;
  /** 🎯T667: the daemon's band for the window as of `at`. */
  band?: string;
};

export type PlanWindow = {
  provider?: string;
  name?: string;
  remaining_percent?: number | null;
  used_percent?: number | null;
  resets_at?: string | null;
  limit_window_seconds?: number | null;
  status?: string;
  pace?: string;
  /** Stored vendor remaining samples for this period (🎯T634). */
  history?: PlanHistoryPoint[] | null;
};

export type PlanBackend = {
  provider?: string;
  status?: string;
  reason?: string;
  fleet_agents?: number;
  stale?: boolean;
  plan_type?: string;
  windows?: PlanWindow[];
};

export type PlanSnapshot = {
  pending?: boolean;
  error?: string;
  backends?: PlanBackend[];
  windows?: PlanWindow[];
};

export type TickerGroup = {
  provider: string;
  available: boolean;
  stale?: boolean;
  reason?: string;
  windows: PlanWindow[];
};

const PROVIDER_RANK: Record<string, number> = {
  claude: 0,
  codex: 1,
  grok: 2,
  bedrock: 3,
  cursor: 4,
};

export function isExhaustedReason(reason: string): boolean {
  const s = String(reason || '').toLowerCase();
  if (!s) return false;
  return (
    s.includes('429') ||
    s.includes('rate_limit') ||
    s.includes('rate-limit') ||
    s.includes('rate limited')
  );
}

export function showOnBar(row: { provider: string; available: boolean; running: boolean }): boolean {
  if (row.provider === 'bedrock' && !row.available && !row.running) return false;
  return true;
}

function backendsOf(snap: PlanSnapshot | undefined): PlanBackend[] {
  if (!snap) return [];
  if (Array.isArray(snap.backends) && snap.backends.length) return snap.backends;
  const wins = Array.isArray(snap.windows) ? snap.windows : [];
  if (!wins.length) return [];
  const m = new Map<string, PlanWindow[]>();
  for (const w of wins) {
    const p = String(w.provider || '');
    const list = m.get(p) || [];
    list.push(w);
    m.set(p, list);
  }
  return [...m.entries()].map(([provider, windows]) => ({
    provider,
    status: 'available',
    windows,
  }));
}

function numericWindows(wins: PlanWindow[] | undefined): PlanWindow[] {
  return (wins || []).filter(
    (w) => w.status !== 'unavailable' && typeof w.remaining_percent === 'number',
  );
}

function exhaustedZeroWindows(): PlanWindow[] {
  return [
    { name: 'session', remaining_percent: 0, used_percent: 100 },
    { name: 'weekly', remaining_percent: 0, used_percent: 100 },
  ];
}

function orderWindows(wins: PlanWindow[]): PlanWindow[] {
  return wins.slice().sort((a, b) => {
    const rank = (n: string) =>
      n === 'session' ? 0 : n === 'weekly' ? 1 : n === 'monthly' ? 2 : 3;
    return rank(String(a.name || '')) - rank(String(b.name || ''));
  });
}

export function tickerGroups(snap: PlanSnapshot | undefined): TickerGroup[] {
  const out: TickerGroup[] = [];
  for (const b of backendsOf(snap)) {
    const provider = String(b.provider || '').toLowerCase();
    if (!provider) continue;
    let windows = numericWindows(b.windows);
    let available = b.status === 'available' && windows.length > 0;
    if (!available && isExhaustedReason(b.reason || '') && windows.length === 0) {
      available = true;
      windows = exhaustedZeroWindows();
    }
    const running = (b.fleet_agents || 0) > 0;
    if (!showOnBar({ provider, available, running })) continue;
    if (!available) {
      out.push({
        provider,
        available: false,
        stale: b.stale,
        reason: b.reason,
        windows: [],
      });
      continue;
    }
    out.push({
      provider,
      available: true,
      stale: b.stale,
      reason: b.reason,
      windows: orderWindows(windows),
    });
  }
  out.sort((a, b) => (PROVIDER_RANK[a.provider] ?? 50) - (PROVIDER_RANK[b.provider] ?? 50));
  return out;
}

/**
 * formatInstantParts formats and forces three-letter month names.
 *
 * en-GB renders September as 'Sept' and every other month as three
 * letters, so a column of dates comes out ragged (🎯T611). Trimming
 * the month token is safe where the year is absent and the date is days
 * away: 'Sep' cannot be read as any other month.
 */
export function formatInstantParts(at: Date, opts: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat('en-GB', opts)
    .formatToParts(at)
    .map((part) => (part.type === 'month' ? part.value.slice(0, 3) : part.value))
    .join('')
    .replace(',', '');
}

/**
 * formatRolloverLocal renders a rollover instant in the viewer's own
 * timezone, to the minute (🎯T588).
 *
 * The tooltip used to interpolate the server's ISO string raw —
 * "rollover 2026-08-31T00:52:28Z" — so every glance cost the owner a
 * mental timezone conversion, and it carried seconds on a value that
 * moves once a day or once a week.
 *
 * An unreadable instant yields '', and the caller drops the clause
 * entirely. Never the raw string as a fallback (that is the bug being
 * fixed) and never a substituted time: a wrong rollover is worse than no
 * rollover, because the owner would plan around it.
 *
 * timeZone is injectable so the oracle can pin a zone; production passes
 * nothing and gets the runtime's. Locale is fixed at en-GB rather than
 * the runtime's so the shape stays '31 Aug 10:52' wherever it renders —
 * the zone must follow the viewer, the wording need not.
 */
export function formatRolloverLocal(
  iso: string | null | undefined,
  timeZone?: string,
): string {
  if (!iso) return '';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '';
  try {
    return formatInstantParts(at, {
      timeZone,
      day: '2-digit',
      month: 'short',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  } catch {
    // An invalid timeZone must not take the whole tooltip down with it.
    return '';
  }
}

/** InstantTip body (🎯T175 / T390): remaining + rollover, not a native title=. */
export function tickerTipBody(groups: TickerGroup[]): string {
  const lines = ['Plan remaining'];
  for (const g of groups) {
    if (!g.available) {
      lines.push(`${g.provider}: unavailable — ${g.reason || 'no plan-remaining published'}`);
      continue;
    }
    for (const w of g.windows) {
      const rem =
        typeof w.remaining_percent === 'number'
          ? `${Math.round(w.remaining_percent)}% remaining`
          : 'remaining unknown';
      const at = formatRolloverLocal(w.resets_at);
      const roll = at ? ` · rollover ${at}` : '';
      lines.push(`${g.provider} ${w.name || 'window'}: ${rem}${roll}`);
    }
  }
  return lines.join('\n');
}

/** @deprecated use tickerTipBody — kept so existing imports compile. */
export function tickerTitle(groups: TickerGroup[]): string {
  return tickerTipBody(groups);
}

/**
 * Usage 0–100 for a window (🎯T670). The published used_percent when there is
 * one, else the complement of remaining. Null when the window publishes
 * neither: an unknown is not zero usage.
 */
export function usedPercentOf(w: {
  used_percent?: number | null;
  remaining_percent?: number | null;
}): number | null {
  const used = w.used_percent;
  if (typeof used === 'number' && Number.isFinite(used)) return Math.min(100, Math.max(0, used));
  const rem = w.remaining_percent;
  if (typeof rem === 'number' && Number.isFinite(rem)) return Math.min(100, Math.max(0, 100 - rem));
  return null;
}

/**
 * What the gauge fills with (🎯T670). Usage, except when the window is spent:
 * a run-dry window keeps the empty red-bordered bar it has always had rather
 * than reading as a solid red block, because "nothing left" and "full" must
 * not paint the same however the bar is oriented.
 */
export function gaugeFillPercent(w: {
  used_percent?: number | null;
  remaining_percent?: number | null;
}): number {
  const rem = w.remaining_percent;
  if (typeof rem === 'number' && Number.isFinite(rem) && rem <= 0) return 0;
  return usedPercentOf(w) ?? 0;
}
