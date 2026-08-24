// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Pure frontier table helpers (🎯T173 / T179 / T181 / T485). DOM-free. */

export const FRONTIER_API_PATH = '/api/frontier';
export const FRONTIER_GRAPH_API_PATH = '/api/frontier/graph';
export const FANOUT_MARK = '\u169B';

export type FrontierDependent = { id: string; name?: string };

export type FrontierRow = {
  id: string;
  name: string;
  status?: string;
  fanout?: number;
  acceptance?: string[];
  context?: string;
  tags?: string[];
  dependents?: Array<FrontierDependent | string>;
};

export type FanoutCell = { text: string; title: string; visible: boolean };

export type HoverCardCache = Record<string, { fingerprint: string; markdown: string }>;

const STATUS_ABBR: Record<string, { abbr: string; title: string }> = {
  identified: { abbr: 'Id', title: 'Identified' },
  converging: { abbr: 'Cv', title: 'Converging' },
  achieved: { abbr: 'Ac', title: 'Achieved' },
  setaside: { abbr: 'Sa', title: 'Set aside' },
  set_aside: { abbr: 'Sa', title: 'Set aside' },
  postponed: { abbr: 'Pp', title: 'Postponed' },
  assigned: { abbr: 'As', title: 'Assigned' },
};

function statusKey(status?: string): string {
  return String(status || '')
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, '_');
}

export function normalizeTargetID(raw?: string): string {
  return String(raw || '')
    .trim()
    .replace(/^🎯/, '');
}

export function formatStatus(status?: string): string {
  const s = String(status || '').trim();
  if (!s) return '—';
  const key = statusKey(s);
  const hit = STATUS_ABBR[key] || STATUS_ABBR[key.replace(/_/g, '')];
  if (hit) return hit.abbr;
  const parts = s.replace(/_/g, ' ').split(/(?=[A-Z])|[\s-]+/).filter(Boolean);
  if (parts.length >= 2) {
    return parts.map((p) => p.charAt(0).toUpperCase()).join('').slice(0, 3);
  }
  return s.slice(0, 2).charAt(0).toUpperCase() + s.slice(1, 2).toLowerCase();
}

export function statusTitle(status?: string): string {
  const s = String(status || '').trim();
  if (!s) return '';
  const key = statusKey(s);
  const hit = STATUS_ABBR[key] || STATUS_ABBR[key.replace(/_/g, '')];
  if (hit) return hit.title;
  return s.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

function normalizeDependents(raw: FrontierRow['dependents']): FrontierDependent[] {
  if (!Array.isArray(raw)) return [];
  const out: FrontierDependent[] = [];
  for (const d of raw) {
    if (d == null) continue;
    if (typeof d === 'string') {
      const id = d.trim();
      if (id) out.push({ id, name: '' });
      continue;
    }
    const id = String(d.id || '').trim();
    if (!id) continue;
    out.push({ id, name: String(d.name || '').trim() });
  }
  return out;
}

function normalizeStringList(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((x) => String(x ?? '').trim()).filter(Boolean);
}

export function formatFanout(
  n?: number,
  id?: string,
  dependents?: FrontierRow['dependents'],
): FanoutCell {
  const deps = normalizeDependents(dependents);
  const count = deps.length > 0 ? deps.length : typeof n === 'number' ? n : parseInt(String(n), 10) || 0;
  if (count <= 0) return { text: '', title: '', visible: false };
  const tid = String(id || '').trim() || '?';
  const text = String(count) + FANOUT_MARK;
  const lead = count === 1 ? '1 target depends on ' + tid : String(count) + ' targets depend on ' + tid;
  if (deps.length === 0) return { text, title: lead, visible: true };
  const lines = [lead];
  for (const d of deps) {
    lines.push(d.name ? '• ' + d.id + ' ' + d.name : '• ' + d.id);
  }
  return { text, title: lines.join('\n'), visible: true };
}

export function shortName(name: string, maxLen = 48): string {
  const n = String(name || '');
  if (n.length <= maxLen) return n;
  return n.slice(0, maxLen - 1) + '…';
}

export function formatTargetCardMarkdown(row?: Partial<FrontierRow> | null): string {
  if (!row || !row.id) return '';
  const id = String(row.id).trim();
  const name = String(row.name || '').trim();
  const lines = ['**🎯' + id + '**' + (name ? ' — ' + name : '')];
  const st = statusTitle(row.status) || String(row.status || '').trim();
  if (st) {
    lines.push('', '**Status:** ' + st);
  }
  const tags = normalizeStringList(row.tags);
  if (tags.length > 0) {
    lines.push('', '**Tags:** ' + tags.join(', '));
  }
  const deps = normalizeDependents(row.dependents);
  if (deps.length > 0) {
    lines.push('', '**Dependents** (' + deps.length + ')');
    for (const d of deps) {
      lines.push(d.name ? '- ' + d.id + ' — ' + d.name : '- ' + d.id);
    }
  }
  const acc = normalizeStringList(row.acceptance);
  if (acc.length > 0) {
    lines.push('', '**Acceptance**');
    for (const a of acc) lines.push('- ' + a);
  }
  const ctx = row.context != null ? String(row.context).trim() : '';
  if (ctx) {
    lines.push('', '**Context**', ctx.split(/\n\s*\n/)[0].trim());
  }
  return lines.join('\n');
}

function stableSerialize(v: unknown): string {
  if (v == null) return 'null';
  const t = typeof v;
  if (t === 'number' || t === 'boolean' || t === 'string') return JSON.stringify(v);
  if (Array.isArray(v)) return '[' + v.map(stableSerialize).join(',') + ']';
  if (t === 'object') {
    const rec = v as Record<string, unknown>;
    const keys = Object.keys(rec).sort();
    return '{' + keys.map((k) => JSON.stringify(k) + ':' + stableSerialize(rec[k])).join(',') + '}';
  }
  return JSON.stringify(String(v));
}

export function cardSourceFingerprint(row?: Partial<FrontierRow> | null): string {
  if (!row || !row.id) return '';
  return stableSerialize({
    id: normalizeTargetID(row.id),
    name: row.name != null ? String(row.name) : '',
    status: row.status != null ? String(row.status) : '',
    tags: normalizeStringList(row.tags),
    dependents: normalizeDependents(row.dependents),
    acceptance: normalizeStringList(row.acceptance),
    context: row.context != null ? String(row.context) : '',
  });
}

/** Built on first hover; reuse until the row source fingerprint changes (🎯T485). */
export function shouldReuseHoverCard(
  cached: { fingerprint: string } | null | undefined,
  row?: Partial<FrontierRow> | null,
): boolean {
  if (!cached || !row?.id) return false;
  const fp = cardSourceFingerprint(row);
  return !!fp && cached.fingerprint === fp;
}

export function expireCardCache(cache: HoverCardCache, rows: Array<Partial<FrontierRow>>): {
  expired: number;
  kept: number;
} {
  if (!cache || typeof cache !== 'object') return { expired: 0, kept: 0 };
  const byId: Record<string, Partial<FrontierRow>> = Object.create(null);
  for (const r of Array.isArray(rows) ? rows : []) {
    if (!r || !r.id) continue;
    byId[normalizeTargetID(r.id)] = r;
  }
  let expired = 0;
  let kept = 0;
  for (const id of Object.keys(cache)) {
    const row = byId[id];
    const hit = cache[id];
    const fp = row ? cardSourceFingerprint(row) : '';
    if (!row || !hit || hit.fingerprint !== fp) {
      delete cache[id];
      expired++;
    } else {
      kept++;
    }
  }
  return { expired, kept };
}

export function hoverCardMarkdown(cache: HoverCardCache, row: FrontierRow): string {
  const id = normalizeTargetID(row.id);
  const hit = cache[id];
  if (shouldReuseHoverCard(hit, row)) return hit.markdown;
  const markdown = formatTargetCardMarkdown(row);
  cache[id] = { fingerprint: cardSourceFingerprint(row), markdown };
  return markdown;
}

/** CSS shape gates for T179 / T340 — pad-sum theatre is not geometry. */
export function cssBlock(css: string, selector: string): string {
  const re = new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{[^}]*\\}');
  const m = css.match(re);
  return m ? m[0] : '';
}

export function tableLayoutIsAuto(rule: string): boolean {
  return /table-layout:\s*auto/.test(rule);
}

export function chromeColumnIsContentMin(rule: string): boolean {
  return /width:\s*1%/.test(rule) || /width:\s*(max-content|fit-content|min-content|0|1px)/.test(rule);
}

export function idColumnClipsHierarchical(rule: string): boolean {
  return /text-overflow:\s*ellipsis/.test(rule) || /overflow:\s*hidden/.test(rule);
}

export function idUses7chClip(rule: string): boolean {
  return /width:\s*7ch/.test(rule) || /max-width:\s*9ch/.test(rule);
}

export function idRemChasm(rule: string): boolean {
  const m = rule.match(/width:\s*([\d.]+)rem/);
  return !!(m && parseFloat(m[1]) >= 5.25);
}

export function statusUsesSmallCaps(rule: string): boolean {
  return /font-variant\s*:\s*small-caps/.test(rule);
}

export function tdHasSharedHorizontalPad(rule: string): boolean {
  return /padding:\s*[\d.]+px\s+[\d.]+px/.test(rule) || /padding-left:\s*[\d.]+px/.test(rule);
}

export function nameFillsRemainder(rule: string): boolean {
  return /width:\s*100%/.test(rule) && /text-overflow:\s*ellipsis/.test(rule);
}

export function statusFanFacingCollapse(statusRule: string, fanRule: string): boolean {
  return /text-align:\s*right/.test(statusRule) && /text-align:\s*left/.test(fanRule);
}
