// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Frontier play / stop / submitted chrome — port of vanilla
 * `web/scripts/frontier_table.js` (🎯T182 / T198 / T222 / T255 / T278).
 * Pure: rows + agents in, request specs out. The component does the fetch. */

import { normalizeTargetID, statusTitle, type FrontierRow } from './table';

export const PLAY_GLYPH = '▶'; // ▶
export const STOP_GLYPH = '■'; // ■
export const DEFAULT_PLAY_PO = 'jevons-po';
export const ENGAGEMENT_STOP_PATH = '/api/agents/engagement/stop';

export type PlayMode = 'play' | 'submitted' | 'stop';

export type PlayAgent = {
  name: string;
  purpose?: string;
  role?: string;
  parent?: string;
  target_id?: string;
  ledger?: string;
};

export type PlayRow = FrontierRow & { engaged?: boolean; engaged_agents?: string[]; kickoff_submitted?: boolean };

export type PlayOpts = { po?: string; selectedAgent?: string | null; agents?: PlayAgent[]; force?: boolean };

export type PlayChromeSpec = {
  mode: PlayMode;
  className: string;
  glyph: string;
  ariaLabel: string;
  title: string;
  disabled: boolean;
  spinning: boolean;
  po?: string;
};

export type KickoffSubmittedSet = Record<string, true>;

export function isProductOwnerName(name?: string): boolean {
  const n = String(name || '').trim().toLowerCase();
  return n.length >= 4 && n.endsWith('-po');
}

function findAgentByName(agents: PlayAgent[], name: string): PlayAgent | null {
  const want = name.trim();
  if (!want) return null;
  return agents.find((a) => a && String(a.name || '').trim() === want) || null;
}

function purposeOf(a: PlayAgent): string {
  return String(a.purpose || a.role || '').trim().toLowerCase();
}

/** 🎯T255: kickoff recipient is the selected agent's PO, never a worker; overseer → default. */
export function resolvePlayPO(opts?: PlayOpts): string {
  const o = opts || {};
  if (o.po && o.po.trim()) return o.po.trim();
  const agents = Array.isArray(o.agents) ? o.agents : [];
  const selected = o.selectedAgent ? String(o.selectedAgent).trim() : '';
  if (!selected) return DEFAULT_PLAY_PO;
  const row = findAgentByName(agents, selected);
  if (!row) return isProductOwnerName(selected) ? selected : DEFAULT_PLAY_PO;
  if (purposeOf(row) === 'overseer') return DEFAULT_PLAY_PO;
  if (isProductOwnerName(row.name)) return row.name.trim();
  const seen = new Set<string>();
  let cur: PlayAgent | null = row;
  for (let hops = 0; cur && hops < 16; hops++) {
    const parentName = String(cur.parent || '').trim();
    if (!parentName || seen.has(parentName)) break;
    seen.add(parentName);
    if (isProductOwnerName(parentName)) return parentName;
    const parentRow = findAgentByName(agents, parentName);
    if (!parentRow || purposeOf(parentRow) === 'overseer') break;
    cur = parentRow;
  }
  return DEFAULT_PLAY_PO;
}

export function playKickoffTitle(po?: string): string {
  return 'Start work via ' + (String(po || '').trim() || DEFAULT_PLAY_PO);
}

export function agentSendPath(name?: string): string {
  return '/api/agents/' + encodeURIComponent(String(name || '').trim() || DEFAULT_PLAY_PO) + '/send';
}

/** 🎯T198: target_id → sorted engaged worker names (overseer excluded, ledger-scoped). */
export function engagementIndex(agents: PlayAgent[], ledgerKey?: string): Record<string, string[]> {
  const want = ledgerKey ? ledgerKey.trim() : '';
  const by: Record<string, string[]> = {};
  for (const a of Array.isArray(agents) ? agents : []) {
    if (!a) continue;
    const tid = normalizeTargetID(a.target_id || '');
    if (!tid) continue;
    const mine = a.ledger ? a.ledger.trim() : '';
    if (want && mine && mine !== want) continue;
    if (String(a.purpose || 'work').trim().toLowerCase() === 'overseer') continue;
    const name = String(a.name || '').trim();
    if (!name) continue;
    if (!by[tid]) by[tid] = [];
    if (!by[tid].includes(name)) by[tid].push(name);
  }
  for (const k of Object.keys(by)) by[k].sort();
  return by;
}

/** 🎯T198: engaged rows carry their agents and sink to the bottom; free rows keep order. */
export function applyEngagement(rows: FrontierRow[], agents: PlayAgent[], ledgerKey?: string): PlayRow[] {
  const index = engagementIndex(agents, ledgerKey);
  const free: PlayRow[] = [];
  const engaged: PlayRow[] = [];
  for (const row of Array.isArray(rows) ? rows : []) {
    if (!row || !row.id) continue;
    const hit = index[normalizeTargetID(row.id)];
    if (hit && hit.length) engaged.push({ ...row, engaged: true, engaged_agents: hit.slice() });
    else free.push({ ...row, engaged: false, engaged_agents: [] });
  }
  return free.concat(engaged);
}

export type KickoffGate = { ok: boolean; reason: string; message?: string; agents?: string[] };

/** 🎯T222: no second kickoff when engaged / set_aside / achieved. */
export function canPlayKickoff(row: PlayRow | null | undefined, opts?: PlayOpts): KickoffGate {
  if (opts && opts.force) return { ok: true, reason: 'ok' };
  if (!row || !row.id) return { ok: false, reason: 'no_id', message: 'missing target id' };
  if (row.engaged) {
    const agents = Array.isArray(row.engaged_agents) ? row.engaged_agents.slice() : [];
    return {
      ok: false,
      reason: 'already_engaged',
      agents,
      message:
        'target already has engaged implementer(s)' +
        (agents.length ? ': ' + agents.join(', ') : '') +
        ' — focus existing engagement or stop first',
    };
  }
  const st = String(row.status || '').trim().toLowerCase().replace(/-/g, '_');
  if (st === 'set_aside' || st === 'achieved') {
    return { ok: false, reason: st, message: 'target is ' + st + ' — not available for kickoff' };
  }
  return { ok: true, reason: 'ok' };
}

export function buildPlayKickoffText(row: PlayRow, opts?: PlayOpts): string {
  if (!row || !row.id || !canPlayKickoff(row, opts).ok) return '';
  const id = String(row.id).trim();
  const name = row.name ? String(row.name).trim() : '';
  const po = resolvePlayPO(opts);
  const lines = [
    'Start work on frontier target 🎯' + id + (name ? ' — ' + name : '') + '.',
    '',
    'Kick off now: spawn/brief a fleet worker with parent=' +
      po +
      ' and target_id=' +
      id +
      ' (jevons_agent_start target_id arg — required for Frontier engagement overlay 🎯T198; do not encode the T-id only in the worker name) ' +
      'and a full brief to execute this target end-to-end ' +
      '(local commits + oracle evidence; no Ship/PR unless the owner asks). ' +
      'Do not only toast or acknowledge — actually start the worker.',
    // 🎯T197: hierarchical target ids keep literal dots in worker names.
    'Worker name: encode hierarchical target ids with literal dots ' +
      '(e.g. jv-t27.2-config not jv-t272-config); flat ids stay flat (jv-t159-seal).',
    // 🎯T222: if an implementer is already engaged, do not spawn a second.
    'If target_id=' + id + ' already has an engaged work agent, do not spawn a second — focus the existing engagement (🎯T222).',
  ];
  const st = statusTitle(row.status) || String(row.status || '').trim();
  if (st) lines.push('', 'Status: ' + st);
  const acc = (Array.isArray(row.acceptance) ? row.acceptance : []).map((s) => String(s).trim()).filter(Boolean);
  if (acc.length) lines.push('', 'Acceptance:', ...acc.map((a) => '- ' + a));
  return lines.join('\n');
}

export type KickoffRequest =
  | { blocked: true; reason: string; message: string; agents: string[]; po: string }
  | { blocked: false; url: string; method: 'POST'; body: { text: string }; po: string };

export function playKickoffRequest(row: PlayRow, opts?: PlayOpts): KickoffRequest {
  const gate = canPlayKickoff(row, opts);
  const po = resolvePlayPO(opts);
  if (!gate.ok) return { blocked: true, reason: gate.reason, message: gate.message || gate.reason, agents: gate.agents || [], po };
  return { blocked: false, url: agentSendPath(po), method: 'POST', body: { text: buildPlayKickoffText(row, opts) }, po };
}

/** 🎯T389: stop within the ledger the table is bound to. */
export function stopEngagementRequest(targetId: string, cwd?: string) {
  const tid = normalizeTargetID(targetId);
  const dir = cwd ? cwd.trim() : '';
  const body: { target_id: string; cwd?: string } = { target_id: tid };
  if (dir) body.cwd = dir;
  return { url: ENGAGEMENT_STOP_PATH, method: 'POST' as const, body, target_id: tid, cwd: dir };
}

// ── 🎯T278: optimistic kickoff-submitted chrome (before PO reply / engage) ──

export function addKickoffSubmitted(set: KickoffSubmittedSet, targetId: string): KickoffSubmittedSet {
  const tid = normalizeTargetID(targetId);
  return tid ? { ...set, [tid]: true } : set;
}

export function removeKickoffSubmitted(set: KickoffSubmittedSet, targetId: string): KickoffSubmittedSet {
  const tid = normalizeTargetID(targetId);
  if (!tid || !set[tid]) return set;
  const out = { ...set };
  delete out[tid];
  return out;
}

export function isKickoffSubmitted(set: KickoffSubmittedSet, targetId: string): boolean {
  const tid = normalizeTargetID(targetId);
  return !!(tid && set[tid]);
}

/** Drop submitted flags once engagement lands (stop chrome owns the cell). */
export function pruneKickoffSubmitted(set: KickoffSubmittedSet, rows: PlayRow[]): KickoffSubmittedSet {
  const engaged = new Set(rows.filter((r) => r && r.engaged).map((r) => normalizeTargetID(r.id)));
  let changed = false;
  const out: KickoffSubmittedSet = {};
  for (const k of Object.keys(set)) {
    if (engaged.has(k)) changed = true;
    else out[k] = true;
  }
  return changed ? out : set;
}

/** Overlay kickoff_submitted on free rows present in the set. */
export function applyKickoffSubmitted(rows: PlayRow[], set: KickoffSubmittedSet): PlayRow[] {
  return rows.map((row) => ({ ...row, kickoff_submitted: !row.engaged && isKickoffSubmitted(set, row.id) }));
}

export function playChromeMode(row?: PlayRow | null): PlayMode {
  if (row && row.engaged) return 'stop';
  if (row && row.kickoff_submitted) return 'submitted';
  return 'play';
}

/** 🎯T182/T198/T278: free → play; submitted → spinner (disabled); engaged → stop. */
export function playChromeSpec(row: PlayRow | null | undefined, opts?: PlayOpts): PlayChromeSpec {
  const id = row && row.id != null ? normalizeTargetID(row.id) : '';
  const mode = playChromeMode(row);
  if (mode === 'stop') {
    return {
      mode,
      className: 'ft-play-btn ft-stop-btn',
      glyph: STOP_GLYPH,
      ariaLabel: 'Stop work on 🎯' + id,
      title: 'Stop engaged worker(s) for this target',
      disabled: false,
      spinning: false,
    };
  }
  if (mode === 'submitted') {
    return {
      mode,
      className: 'ft-play-btn ft-submitted-btn',
      glyph: '',
      ariaLabel: 'Kickoff submitted for 🎯' + id,
      title: 'Kickoff submitted to PO — waiting for engagement',
      disabled: true,
      spinning: true,
    };
  }
  const po = resolvePlayPO(opts);
  return {
    mode,
    className: 'ft-play-btn',
    glyph: PLAY_GLYPH,
    ariaLabel: 'Start work on 🎯' + id,
    title: playKickoffTitle(po),
    disabled: false,
    spinning: false,
    po,
  };
}
