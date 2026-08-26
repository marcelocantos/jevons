// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T266 / 🎯T267 React port of web/scripts/target_context_chrome.js and the
// planTargetAskFocus half of web/scripts/frontier_table.js. Pure: no DOM.
//
// T266: speaker/context chrome tab on a Jevons bubble that asks the owner
// about a 🎯 target — [〈author〉][repo]. T306: never on an owner bubble.
// T314: speaker is the proven author only; repo/PO are never promoted.
// T267: a live target-ask selects the owning PO and highlights the row.

import { DEFAULT_PLAY_PO, isProductOwnerName, resolvePlayPO, type PlayAgent } from './play';
import { normalizeTargetID as normalizeRowID } from './table';

/** Vanilla parity: strips 🎯 and upper-cases the leading `t` (`t540.3` → `T540.3`). */
function normalizeTargetID(raw?: string): string {
  const s = normalizeRowID(raw);
  return /^t\d/.test(s) ? 'T' + s.slice(1) : s;
}

export const SPEAKER_LT = '〈';
export const SPEAKER_GT = '〉';

// The 🎯 is a surrogate pair: the optional prefix must be a group.
const TARGET_ID_RE = /(?:🎯)?\s*(T\d+(?:\.\d+)*)\b/gi;
const ASK_CUE_RE =
  /needs[-\s]?owner|owner\s+(?:decision|accept|ack|gate|ratify)|decision\s+packet|parked[-\s]?for[-\s]?design|design[-\s]?gated|design[-\s]?discussion|blocked[-\s]?on[-\s]?(?:human|owner)|please\s+(?:confirm|accept|decide|choose|pick)|do\s+you\s+(?:want|accept|approve|prefer)|which\s+(?:repo|ledger|product|po|portfolio)|owner[-\s]?facing|ratif(?:y|ication)|\baccept\b|\bconfirm\b|\bdecide\b/i;

export type ContextAgent = PlayAgent & { workdir?: string; purpose?: string; role?: string };

export type TargetContextOpts = {
  text?: string;
  role?: string;
  agents?: ContextAgent[];
  targetId?: string;
  repo?: string;
  po?: string;
  product?: string;
  workdir?: string;
  ledger?: string;
  cwd?: string;
  force?: boolean;
  author?: string;
  authorName?: string;
  agent?: string;
  speaker?: string;
  from?: string;
};

export type TargetContext = {
  show: boolean;
  targetId: string;
  targetIds: string[];
  repo: string;
  po: string;
  product: string;
  speaker: string;
  label: string;
  title: string;
};

export type ChromeModel = TargetContext & {
  /** Painted segments: speaker (bold 〈author〉) and context (dim repo label). */
  speakerText: string;
  contextText: string;
};

function collapse(s: unknown): string {
  return String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
}

/** 🎯T273 speaker-omit: pure overseer / product name "Jevons" only. */
export function isOmittedSpeakerIdentity(name?: string): boolean {
  const n = collapse(name).toLowerCase();
  return !n || n === 'jevons' || n === 'overseer';
}

/** 🎯T314: an org/repo label is context, never a speaker. */
export function looksLikeRepoLabel(name?: string): boolean {
  return String(name == null ? '' : name).indexOf('/') >= 0;
}

/** 🎯T306 role gate: owner-authored bubbles never carry provenance chrome. */
export function isOwnerRole(role?: string): boolean {
  const r = collapse(role).toLowerCase();
  return r === 'user' || r === 'owner' || r === 'me';
}

/** 🎯T314 speaker = proven message author only. */
export function messageAuthor(opts?: TargetContextOpts): string {
  const o = opts || {};
  const raw = o.author ?? o.authorName ?? o.agent ?? o.speaker ?? o.from;
  const name = collapse(raw);
  if (!name || looksLikeRepoLabel(name) || isOmittedSpeakerIdentity(name)) return '';
  return name;
}

export function formatSpeakerLabel(name?: string): string {
  const id = collapse(name);
  if (!id || looksLikeRepoLabel(id) || isOmittedSpeakerIdentity(id)) return '';
  return SPEAKER_LT + id + SPEAKER_GT;
}

/** Context head: full org/repo preferred; a bare product leaf reads as a name. */
export function contextHead(ctx: { repo?: string; product?: string }): string {
  const repo = collapse(ctx.repo);
  if (repo) return repo;
  const product = collapse(ctx.product);
  if (product && isOmittedSpeakerIdentity(product)) return '';
  return product;
}

export function extractTargetIDs(text?: string): string[] {
  const s = String(text == null ? '' : text);
  if (!s) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  TARGET_ID_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = TARGET_ID_RE.exec(s)) !== null) {
    const id = normalizeTargetID(m[1]);
    if (!id || seen.has(id)) continue;
    seen.add(id);
    out.push(id);
  }
  return out;
}

export function looksLikeTargetAsk(text?: string): boolean {
  const s = String(text == null ? '' : text);
  if (!s || !extractTargetIDs(s).length) return false;
  if (ASK_CUE_RE.test(s)) return true;
  if (/\?/.test(s)) return true;
  return /\bowner\b/i.test(s) && /\b(target|frontier|ledger|bullseye)\b/i.test(s);
}

export function repoLabelFromPath(path?: string): string {
  let s = String(path == null ? '' : path).replace(/\\/g, '/').replace(/\/+$/, '');
  if (!s) return '';
  s = s.replace(/\/bullseye\.ya?ml$/i, '');
  const gh = /(?:^|\/)(?:Users\/[^/]+\/)?work\/github\.com\/(.+)$/.exec(s) || /github\.com\/(.+)$/.exec(s);
  if (gh) {
    const parts = gh[1].replace(/\/+$/, '').split('/').filter(Boolean);
    return parts.length >= 2 ? parts[0] + '/' + parts[1] : parts.join('/');
  }
  const work = /(?:^|\/)work\/(?:bitbucket\.org|gitlab\.com)\/(.+)$/.exec(s);
  if (work) {
    const wparts = work[1].split('/').filter(Boolean);
    return wparts.length >= 2 ? wparts[0] + '/' + wparts[1] : work[1];
  }
  const leaf = s.split('/').filter(Boolean).pop() || '';
  if (leaf === 'bullseye.yaml' || leaf === '.jevons') return '';
  return leaf;
}

export function productFromRepoLabel(repo?: string): string {
  const r = collapse(repo);
  const i = r.lastIndexOf('/');
  return i >= 0 ? r.slice(i + 1) : r;
}

function agentTargetID(a: ContextAgent): string {
  const raw = (a as { target_id?: string }).target_id ?? (a as { targetId?: string }).targetId ?? '';
  return normalizeTargetID(String(raw));
}

export function findAgentsByTargetID(agents: ContextAgent[] | undefined, targetId?: string): ContextAgent[] {
  const tid = normalizeTargetID(targetId);
  if (!tid) return [];
  return (agents || []).filter((a) => a && agentTargetID(a) === tid);
}

export function resolvePOForAgent(agents: ContextAgent[] | undefined, agent?: ContextAgent | null): string {
  if (!agent) return '';
  if (isProductOwnerName(agent.name)) return collapse(agent.name);
  const list = agents || [];
  const seen = new Set<string>();
  let cur: ContextAgent | undefined = agent;
  let hops = 0;
  while (cur && hops < 16) {
    hops++;
    const parentName = collapse(cur.parent);
    if (!parentName || seen.has(parentName)) break;
    seen.add(parentName);
    if (isProductOwnerName(parentName)) return parentName;
    const parentRow = list.find((a) => a && collapse(a.name) === parentName);
    if (!parentRow) break;
    if (collapse(parentRow.purpose || parentRow.role).toLowerCase() === 'overseer') break;
    cur = parentRow;
  }
  return '';
}

export function extractRepoHintsFromText(text?: string): string[] {
  const s = String(text == null ? '' : text);
  const out: string[] = [];
  const re = /github\.com\/([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(s)) !== null) {
    const r = collapse(m[1]);
    if (r && !out.includes(r)) out.push(r);
  }
  return out;
}

/** Hover/aria title keeps full meta; the owning PO is labelled "ledger PO" (🎯T314). */
export function formatChromeTitle(ctx: { targetId?: string; repo?: string; product?: string; po?: string; author?: string }): string {
  const parts: string[] = [];
  const tid = normalizeTargetID(ctx.targetId);
  if (tid) parts.push('🎯' + tid);
  const repo = collapse(ctx.repo);
  if (repo) parts.push('repo ' + repo);
  const product = collapse(ctx.product);
  if (product && product !== repo && repo.indexOf('/' + product) < 0) parts.push('product ' + product);
  const po = collapse(ctx.po);
  if (po) parts.push('ledger PO ' + po);
  const speaker = messageAuthor({ author: ctx.author });
  if (speaker) parts.push('speaker ' + SPEAKER_LT + speaker + SPEAKER_GT);
  return parts.length ? parts.join(' · ') : 'Target context';
}

function normPath(p?: string): string {
  return String(p == null ? '' : p).replace(/\\/g, '/').replace(/\/+$/, '');
}

export function resolveTargetContext(opts?: TargetContextOpts): TargetContext {
  const o = opts || {};
  const text = o.text != null ? String(o.text) : '';
  const agents = o.agents || [];
  let ids = extractTargetIDs(text);
  const primary = normalizeTargetID(o.targetId) || ids[0] || '';
  if (primary && !ids.includes(primary)) ids = [primary, ...ids];

  const ownerAuthored = isOwnerRole(o.role);
  let ask = !!o.force || looksLikeTargetAsk(text);
  if (!ask && (o.repo || o.workdir || o.ledger || o.cwd) && primary) ask = true;

  let repo = collapse(o.repo);
  let po = collapse(o.po);
  let product = collapse(o.product);
  let workdir = o.workdir != null ? String(o.workdir) : '';

  if (primary) {
    const engaged = findAgentsByTargetID(agents, primary);
    const pick = engaged.find((a) => collapse(a.purpose || a.role).toLowerCase() !== 'overseer') || engaged[0] || null;
    if (pick) {
      if (!workdir && pick.workdir) workdir = String(pick.workdir);
      if (!po) po = resolvePOForAgent(agents, pick);
    }
  }

  if (!repo) repo = repoLabelFromPath(workdir);
  if (!repo) repo = repoLabelFromPath(o.ledger);
  if (!repo) repo = repoLabelFromPath(o.cwd);
  if (!repo) repo = extractRepoHintsFromText(text)[0] || '';

  if (!po && workdir) {
    const wd = normPath(workdir);
    for (const a of agents) {
      if (!a || !isProductOwnerName(a.name)) continue;
      const aw = normPath(a.workdir);
      if (aw && (aw === wd || wd.indexOf(aw) === 0 || aw.indexOf(wd) === 0)) {
        po = collapse(a.name);
        break;
      }
    }
  }
  if (!po && repo) {
    for (const a of agents) {
      if (!a || !isProductOwnerName(a.name)) continue;
      if (repoLabelFromPath(a.workdir) === repo) {
        po = collapse(a.name);
        break;
      }
    }
  }

  if (!product) product = productFromRepoLabel(repo);
  const speaker = messageAuthor(o);
  const label = contextHead({ repo, product });
  const show = !!(ask && (repo || speaker)) && !ownerAuthored;
  const title = formatChromeTitle({ repo, po, product, targetId: primary, author: speaker });
  return { show, targetId: primary, targetIds: ids, repo, po, product, speaker, label, title };
}

/** 🎯T314 painted tab model: [optional speaker][optional context], space-joined (never "·"). */
export function chromeModel(opts?: TargetContextOpts): ChromeModel {
  const ctx = resolveTargetContext(opts);
  if (!ctx.show) return { ...ctx, label: '', speakerText: '', contextText: '' };
  const speakerText = formatSpeakerLabel(ctx.speaker);
  const contextText = contextHead({ repo: ctx.repo, product: ctx.product });
  const show = !!(speakerText || contextText);
  const label = speakerText + (speakerText && contextText ? ' ' : '') + contextText;
  return { ...ctx, show, label: show ? label : '', speakerText, contextText };
}

// ---------------------------------------------------------------------------
// 🎯T267: target-ask focus plan (PO select + Frontier row highlight).

export type TargetAskPlan = {
  targetId: string;
  highlightId: string;
  po: string;
  tab: 'frontier';
  selectPO: true;
};

export type TargetAskFocusOpts = {
  text?: string;
  targetId?: string;
  po?: string;
  preferredPO?: string;
  agents?: ContextAgent[];
  selectedAgent?: string | null;
};

/** Explicit `__TARGET_ASK__:Tn[|po]` marker, or 🎯-prefixed id plus a needs-owner cue. */
export function detectTargetAsk(text?: string): { targetId: string; po: string } | null {
  const s = String(text == null ? '' : text);
  if (!s) return null;
  const m = /__TARGET_ASK__\s*:\s*(T[0-9]+(?:\.[0-9]+)*)(?:\s*[|@]\s*([A-Za-z0-9_.\-]+))?/i.exec(s);
  if (m) return { targetId: normalizeTargetID(m[1]), po: m[2] ? m[2].trim() : '' };
  const ids: string[] = [];
  const re = /🎯\s*(T[0-9]+(?:\.[0-9]+)*)/g;
  let mm: RegExpExecArray | null;
  while ((mm = re.exec(s)) !== null) {
    const id = normalizeTargetID(mm[1]);
    if (id && !ids.includes(id)) ids.push(id);
  }
  if (!ids.length) return null;
  const askish =
    /needs[- ]owner|decision\s*packet|owner\s+decision|please\s+(decide|choose|confirm|accept)|needs\s+your\s+(decision|input|call)|awaiting\s+owner|owner\s+call|owner\s+ask/i.test(s);
  return askish ? { targetId: ids[0], po: '' } : null;
}

export function resolveOwningPOForTarget(opts: { targetId?: string; agents?: ContextAgent[]; preferredPO?: string }): string {
  const preferred = collapse(opts.preferredPO);
  if (preferred && (isProductOwnerName(preferred) || preferred.indexOf('-po') > 0)) return preferred;
  const agents = opts.agents || [];
  const tid = normalizeTargetID(opts.targetId);
  if (tid) {
    for (const a of agents) {
      if (!a || agentTargetID(a) !== tid) continue;
      if (collapse(a.purpose || a.role).toLowerCase() === 'overseer') continue;
      if (isProductOwnerName(a.name)) return collapse(a.name);
      const via = resolvePlayPO({ selectedAgent: a.name, agents });
      if (via) return via;
    }
  }
  return DEFAULT_PLAY_PO;
}

export function rowMatchesHighlight(rowId?: string, highlightId?: string): boolean {
  const rid = normalizeTargetID(rowId);
  const hid = normalizeTargetID(highlightId);
  return !!(rid && hid && rid === hid);
}

export function planTargetAskFocus(opts?: TargetAskFocusOpts): TargetAskPlan | null {
  const o = opts || {};
  const detected = collapse(o.targetId)
    ? { targetId: normalizeTargetID(o.targetId), po: collapse(o.po ?? o.preferredPO) }
    : detectTargetAsk(o.text);
  if (!detected || !detected.targetId) return null;
  const po = resolveOwningPOForTarget({
    targetId: detected.targetId,
    agents: o.agents,
    preferredPO: detected.po || o.po || o.preferredPO,
  });
  return { targetId: detected.targetId, highlightId: detected.targetId, po, tab: 'frontier', selectPO: true };
}
