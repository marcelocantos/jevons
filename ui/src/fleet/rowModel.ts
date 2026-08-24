// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Port of web/scripts/fleet_row.js glanceable chrome (🎯T118 / T211 / T545.4). */

export type FleetAgent = {
  name?: string;
  purpose?: string;
  parent?: string;
  status?: string;
  running?: boolean;
  phase?: string;
  step?: string;
  last_tool?: string;
  lastTool?: string;
  progress?: string;
  summary?: string;
  workdir?: string;
};

export type FleetSecondaryCtx = {
  parentWorkdir?: string;
  hasChildren?: boolean;
};

export type FleetSecondary = {
  kind: 'path' | 'progress' | 'status' | '';
  text: string;
};

const PROCESS_LABELS = new Set(['running', 'stopped', 'idle', 'parked']);
const PROGRESS_MAX = 48;

export function collapse(s: unknown): string {
  return String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
}

function truncate(s: string, n = PROGRESS_MAX): string {
  const t = collapse(s);
  if (!t) return '';
  return t.length > n ? t.slice(0, n - 1) + '…' : t;
}

function isProcessOnlyLabel(text: string): boolean {
  return PROCESS_LABELS.has(collapse(text).toLowerCase());
}

export function isBusyAgent(agent: FleetAgent): boolean {
  const phase = collapse(agent.phase).toLowerCase();
  if (phase === 'working') return true;
  if (phase === 'idle' || phase === 'parked' || phase === 'blocked') return false;
  const step = collapse(agent.step || agent.last_tool || agent.lastTool);
  if (step) return true;
  const explicit = collapse(agent.progress || agent.summary);
  if (!explicit) return false;
  if (isProcessOnlyLabel(explicit)) return false;
  const low = explicit.toLowerCase();
  if (low === 'working' || low.indexOf('working') === 0) return true;
  if (low.indexOf(' · ') !== -1 || low.indexOf('·') !== -1) return true;
  return false;
}

function isActionProgressText(text: string): boolean {
  const t = collapse(text);
  if (!t || isProcessOnlyLabel(t)) return false;
  const low = t.toLowerCase();
  if (low === 'working' || low.indexOf('working') === 0) return true;
  if (low === 'blocked' || low.indexOf('blocked') === 0) return true;
  if (low.indexOf(' · ') !== -1 || low.indexOf('·') !== -1) return true;
  return false;
}

function normalizeWorkdir(d: unknown): string {
  let s = String(d == null ? '' : d).replace(/\\/g, '/').replace(/\/+$/, '');
  if (!s) return '';
  return s.replace(/^\/Users\/[^/]+/, '~');
}

export function githubPath(workdir?: string): string {
  const s = String(workdir || '');
  const gh = /github\.com\/(.+)$/.exec(s);
  return gh ? gh[1] : '';
}

export function isStateDirOverseerHome(workdir: unknown, name: unknown): boolean {
  const n = collapse(name);
  if (!n) return false;
  const s = String(workdir == null ? '' : workdir).replace(/\\/g, '/').replace(/\/+$/, '');
  if (!s) return false;
  return new RegExp('(?:^|/)\\.jevons/' + n.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$').test(s);
}

export function shouldShowPathSecondary(agent: FleetAgent, ctx: FleetSecondaryCtx = {}): boolean {
  if (isStateDirOverseerHome(agent.workdir, agent.name)) return false;
  const purpose = collapse(agent.purpose).toLowerCase();
  if (purpose === 'aside' || purpose === 'side' || purpose === 'side-chat' || purpose === 'file-target') {
    return false;
  }
  const parent = collapse(agent.parent);
  if (!parent) return true;
  if (ctx.hasChildren) return true;
  const parentWd = normalizeWorkdir(ctx.parentWorkdir || '');
  const selfWd = normalizeWorkdir(agent.workdir || '');
  if (parentWd && selfWd && parentWd !== selfWd) return true;
  if (!parentWd && selfWd) return true;
  return false;
}

export function formatFleetProgress(agent: FleetAgent, maxLen = PROGRESS_MAX): string {
  const phaseRaw = collapse(agent.phase);
  const phase = phaseRaw.toLowerCase();
  const step = collapse(agent.step || agent.last_tool || agent.lastTool);
  let explicit = collapse(agent.progress || agent.summary);
  if (explicit && isProcessOnlyLabel(explicit)) explicit = '';

  if (phase === 'working') {
    if (explicit) return truncate(explicit, maxLen);
    if (step) return truncate('working · ' + step, maxLen);
    return 'working';
  }
  if (phase === 'blocked') {
    if (explicit) return truncate(explicit, maxLen);
    if (step) return truncate('blocked · ' + step, maxLen);
    return 'blocked';
  }
  if (explicit) return truncate(explicit, maxLen);
  if (phase === 'idle' || phase === 'parked' || phase === '') {
    const st = collapse(agent.status).toLowerCase();
    if (st === 'stopped') return 'stopped';
    if (st === 'running' || phase === 'idle' || phase === 'parked') {
      return phase === 'parked' ? 'parked' : 'idle';
    }
    if (step) return truncate(step, maxLen);
    return phase === 'parked' ? 'parked' : phase === 'idle' ? 'idle' : '';
  }
  if (phase && step) return truncate(phaseRaw + ' · ' + step, maxLen);
  if (step) return truncate(step, maxLen);
  if (phaseRaw) return truncate(phaseRaw, maxLen);
  const st = collapse(agent.status).toLowerCase();
  if (st === 'running') return 'idle';
  if (st === 'stopped') return 'stopped';
  if (st === 'blocked' || st === 'idle' || st === 'working' || st === 'parked') return st;
  return '';
}

export function fleetSecondary(agent: FleetAgent, ctx: FleetSecondaryCtx = {}): FleetSecondary {
  const purpose = collapse(agent.purpose).toLowerCase();
  if (purpose === 'aside' || purpose === 'side' || purpose === 'side-chat' || purpose === 'file-target') {
    return { kind: '', text: '' };
  }
  const progressText = formatFleetProgress(agent);
  const busy = isBusyAgent(agent);
  const hasAction =
    busy ||
    isActionProgressText(progressText) ||
    collapse(agent.phase).toLowerCase() === 'working';
  if (shouldShowPathSecondary(agent, ctx)) {
    const path = githubPath(agent.workdir) || collapse(agent.workdir);
    return path ? { kind: 'path', text: path } : { kind: '', text: '' };
  }
  if (isStateDirOverseerHome(agent.workdir, agent.name)) {
    if (hasAction && progressText && isActionProgressText(progressText)) {
      return { kind: 'progress', text: progressText };
    }
    const st = collapse(progressText).toLowerCase();
    if (st === 'stopped' || st === 'blocked') {
      return { kind: 'status', text: progressText };
    }
    return { kind: '', text: '' };
  }
  if (progressText) {
    const kind = hasAction && isActionProgressText(progressText) ? 'progress' : 'status';
    return { kind, text: progressText };
  }
  return { kind: '', text: '' };
}

/** Process liveness for the tree dot — Alive(), not registry presence. */
export function agentDotState(agent: FleetAgent): 'running' | 'stopped' {
  if (agent.running === false) return 'stopped';
  if (agent.running === true) return 'running';
  const st = collapse(agent.status).toLowerCase();
  if (st === 'stopped' || st === 'dead' || st === 'dead_unmaterialized') return 'stopped';
  if (st === 'running') return 'running';
  return 'stopped';
}
