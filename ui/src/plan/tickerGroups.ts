// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Same bar-membership rules as web/scripts/plan_usage.js (🎯T390.1). */

export type PlanWindow = {
  provider?: string;
  name?: string;
  remaining_percent?: number | null;
  used_percent?: number | null;
  resets_at?: string | null;
  limit_window_seconds?: number | null;
  status?: string;
  pace?: string;
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
      const roll = w.resets_at ? ` · rollover ${w.resets_at}` : '';
      lines.push(`${g.provider} ${w.name || 'window'}: ${rem}${roll}`);
    }
  }
  return lines.join('\n');
}

/** @deprecated use tickerTipBody — kept so existing imports compile. */
export function tickerTitle(groups: TickerGroup[]): string {
  return tickerTipBody(groups);
}
