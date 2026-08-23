// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Activity-strip tool-arg summaries (🎯T116). Port of web/scripts/tool_summary.js. */

export const TOOL_SUMMARY_MAX_LEN = 60;

const PREFERRED_KEYS = [
  'query',
  'path',
  'command',
  'title',
  'name',
  'text',
  'url',
  'id',
  'tool_name',
] as const;

function collapse(s: string): string {
  return String(s).replace(/\s+/g, ' ').trim();
}

export function truncateToolSummary(s: string, n = TOOL_SUMMARY_MAX_LEN): string {
  s = collapse(s);
  if (!s) return '';
  return s.length > n ? s.slice(0, n - 3) + '...' : s;
}

function asUsefulString(v: unknown): string | null {
  if (typeof v === 'string') {
    const t = collapse(v);
    return t || null;
  }
  if (typeof v === 'number' && Number.isFinite(v)) return String(v);
  if (typeof v === 'boolean') return String(v);
  return null;
}

function pickFromObject(obj: Record<string, unknown>, depth: number): string | null {
  const toolInput = obj.tool_input;
  const hasToolInput = toolInput != null && typeof toolInput === 'object' && !Array.isArray(toolInput);

  for (const k of PREFERRED_KEYS) {
    if (!Object.prototype.hasOwnProperty.call(obj, k)) continue;
    if (k === 'tool_name' && hasToolInput) continue;
    const pref = asUsefulString(obj[k]);
    if (pref) return truncateToolSummary(pref);
  }

  if (depth < 1 && hasToolInput) {
    const nested = pickFromObject(toolInput as Record<string, unknown>, depth + 1);
    if (nested) {
      const tn = asUsefulString(obj.tool_name);
      if (tn) return truncateToolSummary(tn + ': ' + nested);
      return nested;
    }
  }

  const keys = Object.keys(obj);
  for (const k of keys) {
    const s = asUsefulString(obj[k]);
    if (s) return truncateToolSummary(s);
  }

  if (depth < 1) {
    for (const k of keys) {
      const v = obj[k];
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        const deeper = pickFromObject(v as Record<string, unknown>, depth + 1);
        if (deeper) return deeper;
      }
    }
  }
  return null;
}

/** Short single-line value gist (never a bare key list when a value exists). */
export function summariseInput(input: unknown): string {
  if (input == null) return '';
  if (typeof input === 'string') return truncateToolSummary(input);
  if (typeof input !== 'object') {
    const scalar = asUsefulString(input);
    return scalar ? truncateToolSummary(scalar) : '';
  }
  if (Array.isArray(input)) {
    for (const item of input) {
      const s = summariseInput(item);
      if (s) return s;
    }
    return '';
  }
  return pickFromObject(input as Record<string, unknown>, 0) || '';
}

export function isGenericToolName(name: string): boolean {
  const t = name.replace(/\s+/g, ' ').trim().toLowerCase();
  return t === '' || t === 'tool' || t === 'tool_use' || t === 'mcp: tool' || t === 'mcp:tool';
}
