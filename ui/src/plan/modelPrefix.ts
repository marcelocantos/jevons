// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Fleet badge: company mark + condensed model. Same rules as web/scripts/model_prefix.js (🎯T287). */

const ANTHROPIC = 'anthropic';
const XAI = 'xai';
const OPENAI = 'openai';
const CURSOR = 'cursor';

const PROVIDER_COMPANY: Record<string, string> = {
  claude: ANTHROPIC,
  anthropic: ANTHROPIC,
  bedrock: ANTHROPIC,
  grok: XAI,
  xai: XAI,
  codex: OPENAI,
  openai: OPENAI,
  cursor: CURSOR,
};

const FAMILIES = ['opus', 'sonnet', 'haiku', 'fable', 'grok', 'gpt'];
const FAMILY_INITIAL: Record<string, string> = { opus: 'O', sonnet: 'S', haiku: 'H', fable: 'F' };
const COMPANY_LABEL: Record<string, string> = {
  anthropic: 'Anthropic',
  xai: 'xAI',
  openai: 'OpenAI',
  cursor: 'Cursor',
};
const DATE_DIGITS = 6;

function norm(s: unknown): string {
  return String(s ?? '').trim().toLowerCase();
}

export function companyFromModel(model: string): string {
  const m = norm(model);
  if (!m) return '';
  if (/claude|opus|sonnet|haiku|fable/.test(m)) return ANTHROPIC;
  if (/grok/.test(m)) return XAI;
  if (/gpt|codex|^o\d/.test(m)) return OPENAI;
  return '';
}

export function companyFor(provider: string, model: string): string {
  const p = norm(provider);
  if (p && PROVIDER_COMPANY[p]) return PROVIDER_COMPANY[p];
  return companyFromModel(model);
}

function claudeFamilySniff(m: string): string {
  const idx = m.indexOf('claude');
  if (idx < 0) return '';
  const rest = m.slice(idx + 'claude'.length).replace(/^[^a-z0-9]+/, '');
  const tokens = rest.split(/[^a-z0-9]+/);
  for (const t of tokens) {
    if (!t || /^\d/.test(t) || /^v\d+$/.test(t)) continue;
    return t;
  }
  return '';
}

function familyOf(model: string): string {
  const m = norm(model);
  for (const f of FAMILIES) {
    if (m.includes(f)) return f;
  }
  return claudeFamilySniff(m);
}

function segment(digits: string): string {
  const n = parseInt(digits, 10);
  return Number.isNaN(n) ? '' : String(n);
}

function segmentsFrom(s: string): string {
  let tail = s;
  const parts: string[] = [];
  while (tail) {
    const hit = /^(\d+)/.exec(tail);
    if (!hit || hit[1].length >= DATE_DIGITS) break;
    parts.push(segment(hit[1]));
    tail = tail.slice(hit[1].length);
    if (!/^[.\-_]/.test(tail)) break;
    tail = tail.slice(1);
  }
  return parts.join('.');
}

function versionNear(model: string, family: string): string {
  const m = norm(model);
  if (!family) return '';
  const idx = m.indexOf(family);
  if (idx < 0) return '';
  const after = segmentsFrom(m.slice(idx + family.length).replace(/^[^0-9a-z]+/, ''));
  if (after) return after;
  const before = /(\d+(?:[.\-_]\d+)*)[^0-9a-z]*$/.exec(m.slice(0, idx));
  return before ? segmentsFrom(before[1]) : '';
}

export function versionOf(model: string): string {
  const m = norm(model);
  if (!m) return '';
  const family = familyOf(m);
  if (family) return versionNear(m, family);
  return versionNear(m, 'claude');
}

export function familyInitial(model: string): string {
  const family = familyOf(model);
  if (!family) return '';
  if (Object.prototype.hasOwnProperty.call(FAMILY_INITIAL, family)) return FAMILY_INITIAL[family];
  if (FAMILIES.includes(family)) return '';
  return family.charAt(0).toUpperCase();
}

export function modelMatchesCompany(company: string, model: string): boolean {
  if (!model || !company) return true;
  const fromModel = companyFromModel(model);
  return !fromModel || fromModel === company;
}

export type ModelPrefix = {
  company: string;
  initial: string;
  version: string;
  label: string;
  title: string;
};

export function modelPrefix(agent: { provider?: string; model?: string } | null | undefined): ModelPrefix {
  const a = agent && typeof agent === 'object' ? agent : {};
  const provider = String(a.provider || '');
  const model = String(a.model || '');
  const company = companyFor(provider, model);
  if (!company) return { company: '', initial: '', version: '', label: '', title: '' };
  const matched = modelMatchesCompany(company, model);
  const initial = matched ? familyInitial(model) : '';
  const version = matched ? versionOf(model) : '';
  const shown = matched && model ? model : provider;
  const title = (COMPANY_LABEL[company] || company) + (shown ? ' · ' + shown : '');
  return { company, initial, version, label: initial + version, title };
}

/** Keep last provider/model when a poll omits them (omitempty). */
export function mergeAgentChrome<T extends { name: string; provider?: string; model?: string }>(
  prev: T[],
  next: T[],
): T[] {
  const by = new Map(prev.map((a) => [a.name, a]));
  return next.map((a) => {
    const old = by.get(a.name);
    if (!old) return a;
    return {
      ...a,
      provider: a.provider || old.provider,
      model: a.model || old.model,
    };
  });
}
