// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** RHS Coach tab model (vanilla rsi_dispositions.js 🎯T354). */

export const API_PATH = '/api/rsi/dispositions';
export const POLL_MS = 20000;

export const PENDING = 'pending';
export const FILE = 'file';
export const PARK = 'park';
export const IGNORE = 'ignore_with_reason';
export const ACT_OTHER = 'act_other';

const DISPOSITION_LABELS: Record<string, string> = {
  pending: 'pending',
  file: 'filed',
  park: 'parked',
  ignore_with_reason: 'ignored',
  act_other: 'acted',
};

const SEVERITY_RANK: Record<string, number> = { high: 3, medium: 2, low: 1 };

export type CoachRow = {
  fingerprint: string;
  title: string;
  observation: string;
  severity: string;
  evidence: string;
  retro: boolean;
  disposition: string;
  dispositionLabel: string;
  pending: boolean;
  reason: string;
  targetID: string;
  outcome: string;
  deliveredAt: number;
  dispositionAt: number;
};

export type CoachModel = {
  rows: CoachRow[];
  total: number;
  pending: number;
  empty: boolean;
  error: string;
  path: string;
};

function str(v: unknown): string {
  return v == null ? '' : String(v);
}

export function dispositionOf(entry: unknown): string {
  const d = str((entry as { disposition?: unknown } | null)?.disposition).trim().toLowerCase();
  if (d === FILE || d === PARK || d === IGNORE || d === ACT_OTHER) return d;
  return PENDING;
}

export function dispositionLabel(id: unknown): string {
  return DISPOSITION_LABELS[str(id).trim().toLowerCase()] || PENDING;
}

export function parseTime(v: unknown): number {
  const s = str(v).trim();
  if (!s || s.indexOf('0001-01-01') === 0) return 0;
  const ms = Date.parse(s);
  return isNaN(ms) ? 0 : ms;
}

export function row(entry: unknown): CoachRow {
  const e = (entry || {}) as Record<string, unknown>;
  const disposition = dispositionOf(e);
  const name = str(e.name).trim();
  const observation = str(e.observation).trim();
  return {
    fingerprint: str(e.fingerprint).trim(),
    title: name || observation || '(unnamed judgment)',
    observation,
    severity: str(e.severity).trim().toLowerCase(),
    evidence: str(e.evidence).trim(),
    retro: str(e.mode).trim().toLowerCase() === 'retro',
    disposition,
    dispositionLabel: dispositionLabel(disposition),
    pending: disposition === PENDING,
    reason: str(e.reason).trim(),
    targetID: str(e.target_id).trim(),
    outcome: str(e.outcome).trim(),
    deliveredAt: parseTime(e.delivered_at),
    dispositionAt: parseTime(e.disposition_at),
  };
}

export function sortRows(rows: CoachRow[]): CoachRow[] {
  return rows.slice().sort((a, b) => {
    if (a.deliveredAt !== b.deliveredAt) return b.deliveredAt - a.deliveredAt;
    const sa = SEVERITY_RANK[a.severity] || 0;
    const sb = SEVERITY_RANK[b.severity] || 0;
    if (sa !== sb) return sb - sa;
    return a.fingerprint < b.fingerprint ? -1 : a.fingerprint > b.fingerprint ? 1 : 0;
  });
}

export function normalizePayload(payload: unknown, err?: unknown): CoachModel {
  if (err) {
    const message = err instanceof Error ? err.message : str(err) || 'request failed';
    return { rows: [], total: 0, pending: 0, empty: false, error: message, path: '' };
  }
  const p = (payload || {}) as Record<string, unknown>;
  const list = Array.isArray(p.judgments) ? p.judgments : [];
  const rows = sortRows(list.map(row));
  const pending = typeof p.pending === 'number' ? p.pending : rows.filter((r) => r.pending).length;
  const total = typeof p.total === 'number' ? p.total : rows.length;
  return {
    rows,
    total,
    pending,
    empty: rows.length === 0,
    error: str(p.error).trim(),
    path: str(p.path).trim(),
  };
}

export function emptyText(): string {
  return 'No coach judgments yet.';
}

export function countsText(model: CoachModel | null | undefined): string {
  const total = model?.total || 0;
  if (!total) return '';
  let s = total + (total === 1 ? ' judgment' : ' judgments');
  if (model?.pending) s += ' · ' + model.pending + ' pending';
  return s;
}

export function detailText(r: CoachRow | null | undefined): string {
  if (!r || r.pending) return '';
  const parts: string[] = [];
  if (r.targetID) parts.push('🎯' + r.targetID.replace(/^🎯/, ''));
  if (r.reason) parts.push(r.reason);
  if (r.outcome) parts.push(r.outcome);
  return parts.join(' — ');
}
