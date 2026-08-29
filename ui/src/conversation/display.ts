// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { classifyInjectUserText } from './inject';
import { isSealedAssistant } from './stream';
import { turnOriginOf, type TurnOrigin } from './paint';
import { isGenericToolName, summariseInput } from './toolSummary';

/** Lifted display fold: notes, ⋯ n steps, prose. Not a second hydrate. */

export type DisplayKind = 'user' | 'assistant' | 'steps' | 'diagnostic';

/** Client-only mux send nack (🎯T545.3). Not a journaled event. */
export const SEND_ERROR_TYPE = 'send_error';

export function isSendErrorFrame(frame: unknown): boolean {
  return asRec(frame).type === SEND_ERROR_TYPE;
}

export function diagnosticLabel(text: string, count: number): string {
  const t = String(text ?? '').trim();
  if (!t) return '';
  return count > 1 ? t + ' · ×' + count : t;
}

function diagnosticBase(row: DisplayRow): string {
  const n = row.count || 1;
  if (n > 1) {
    const suffix = ' · ×' + n;
    if (row.text.endsWith(suffix)) return row.text.slice(0, -suffix.length);
  }
  return row.text;
}

export type StepItem = { cls: string; text: string };

export type DisplayRow = {
  kind: DisplayKind;
  text: string;
  steps?: number;
  /** 🎯T233: harness inject nugget kind (row is a steps-family turn-marker). */
  inject?: string;
  items?: StepItem[];
  when?: number;
  /** Assistant only: incremental smd until a terminal stop_reason. */
  sealed?: boolean;
  /** Diagnostic only: consecutive identical nacks (🎯T545.3). */
  count?: number;
  /** User only: provenance for body paint (🎯T221 / T381). */
  origin?: TurnOrigin;
};

function asRec(frame: unknown): Record<string, unknown> {
  return frame && typeof frame === 'object' ? (frame as Record<string, unknown>) : {};
}

function message(frame: unknown): Record<string, unknown> | undefined {
  const m = asRec(frame).message;
  return m && typeof m === 'object' ? (m as Record<string, unknown>) : undefined;
}

function blocks(frame: unknown): Record<string, unknown>[] {
  const content = message(frame)?.content ?? asRec(frame).content;
  if (Array.isArray(content)) {
    return content.filter((b) => b && typeof b === 'object') as Record<string, unknown>[];
  }
  return [];
}

export function proseText(frame: unknown): string {
  const f = asRec(frame);
  if (typeof f.text === 'string' && f.text && f.type === 'agent_note') return f.text;
  const msg = message(frame);
  const content = msg?.content ?? f.content ?? f.text;
  if (typeof content === 'string') return content;
  return blocks(frame)
    .filter((b) => b.type === 'text' || b.type === 'output_text')
    .map((b) => String(b.text || ''))
    .join('');
}

/** Strip journal echo markers so owner text matches the live bubble (🎯T537.1.2). */
export function normalizeOwnerEchoText(text: string): string {
  let t = String(text ?? '').trim();
  if (!t) return '';
  if (t.startsWith('[user]\n')) t = t.slice('[user]\n'.length).trim();
  else if (/^\[user\]\s+/.test(t)) t = t.replace(/^\[user\]\s+/, '').trim();
  for (let i = 0; i < 3; i++) {
    const m = t.match(/^\s*<user_query(?:\s[^>]*)?>\s*([\s\S]*?)\s*<\/user_query>\s*$/i);
    if (!m) break;
    t = String(m[1] || '').trim();
  }
  return t;
}

/** True when a user echo matching `pending` arrived at or after `afterIndex`. */
export function shouldAckPendingSend(
  pending: string | null | undefined,
  frames: unknown[],
  afterIndex = 0,
): boolean {
  const want = normalizeOwnerEchoText(String(pending ?? ''));
  if (!want) return false;
  const start = Number.isFinite(afterIndex) && afterIndex > 0 ? afterIndex : 0;
  for (let i = frames.length - 1; i >= start; i--) {
    const f = frames[i];
    if (isSendErrorFrame(f)) continue;
    if (isUserFrame(f)) return normalizeOwnerEchoText(proseText(f)) === want;
  }
  return false;
}

/** Owner-filterable overseer ops replies (🎯T238 / T240 / T245). */
export function isSilentAssistantText(text: string): boolean {
  const t = String(text ?? '').trim();
  if (!t) return false;
  const pref = '[silent]';
  const lower = t.toLowerCase();
  if (lower.startsWith(pref)) return true;
  const head = t.length > 80 ? t.slice(0, 80) : t;
  return head.split('\n').some((line) => line.trim().toLowerCase().startsWith(pref));
}

function isProtocolControlFrameText(text: string): boolean {
  const t = text.trim();
  if (t.length < 2 || t[0] !== '{' || t[t.length - 1] !== '}') return false;
  try {
    const obj = JSON.parse(t) as { type?: unknown };
    return !!obj && typeof obj === 'object' && !Array.isArray(obj) && typeof obj.type === 'string' && obj.type.trim() !== '';
  } catch {
    return false;
  }
}

function isNonBoundaryUserText(text: string): boolean {
  const raw = String(text ?? '');
  if (!raw.trim()) return false;
  if (isProtocolControlFrameText(raw)) return true;
  const display = normalizeOwnerEchoText(raw);
  const trimmed = display.replace(/^\s+/, '');
  if (/<system-reminder[\s>]/i.test(raw) || /<\/system-reminder>/i.test(raw)) return true;
  if (trimmed.indexOf('[Jevons fleet standing brief') === 0 || /Jevons fleet standing brief/.test(display)) return true;
  if (/^\[event:\s*[^\]]+\]/i.test(trimmed)) return true;
  if (trimmed.indexOf('[Daemon restart') === 0) return true;
  if (/^Background task\b/i.test(trimmed)) return true;
  return false;
}

export function isAgentNote(frame: unknown): boolean {
  return asRec(frame).type === 'agent_note';
}

export function isUserFrame(frame: unknown): boolean {
  const t = asRec(frame).type;
  const role = message(frame)?.role;
  return t === 'user' || role === 'user';
}

export function isToolOnly(frame: unknown): boolean {
  const f = asRec(frame);
  if (f.type === 'tool_result' || f.type === 'result') return false;
  const bs = blocks(frame);
  if (!bs.length) {
    return f.type === 'tool_use';
  }
  const tools = bs.filter((b) => b.type === 'tool_use');
  const text = bs.filter((b) => (b.type === 'text' || b.type === 'output_text') && String(b.text || '').trim());
  return tools.length > 0 && text.length === 0;
}

export function stepsLabel(n: number): string {
  if (n <= 0) return '';
  return '⋯ ' + n + (n === 1 ? ' step' : ' steps');
}

export function summariseToolUse(c: Record<string, unknown>): string {
  const name = typeof c.name === 'string' && c.name.trim() ? c.name : 'tool';
  const extra = summariseInput(c.input);
  if (isGenericToolName(name)) return extra || name;
  return extra ? name + ': ' + extra : name;
}

export function stepItems(frame: unknown): StepItem[] {
  if (isAgentNote(frame)) {
    const t = String(asRec(frame).text || '').trim();
    return t ? [{ cls: 'agent-note', text: t }] : [];
  }
  const f = asRec(frame);
  if (f.type === 'tool_result' || f.type === 'result') return [];
  if (f.type === 'tool_use') return [{ cls: 'tool-use', text: summariseToolUse(f) }];
  const out: StepItem[] = [];
  for (const b of blocks(frame)) {
    if (b.type === 'tool_use') out.push({ cls: 'tool-use', text: summariseToolUse(b) });
  }
  return out;
}

export function stepItem(frame: unknown): StepItem | null {
  return stepItems(frame)[0] ?? null;
}

function frameWhen(frame: unknown): number | undefined {
  const f = asRec(frame);
  if (typeof f.when === 'number' && Number.isFinite(f.when)) return f.when;
  if (typeof f.timestamp === 'number' && Number.isFinite(f.timestamp)) {
    return f.timestamp < 1e11 ? Math.round(f.timestamp * 1000) : f.timestamp;
  }
  return undefined;
}

/**
 * 🎯T221: a fleet inject arrives as role=user wrapped in <user_query>…</user_query>
 * (event push / overseer inject); the wrapper is provenance, so the bubble paints
 * markdown like any agent-origin turn. An unwrapped owner turn stays literal (T381).
 */
export function isWrappedInjectUserText(raw: string): boolean {
  return /^\s*<user_query(?:\s[^>]*)?>[\s\S]*<\/user_query>\s*$/i.test(String(raw ?? ''));
}

export function userTurnOrigin(frame: unknown, raw: string, inspect = false): TurnOrigin {
  if (turnOriginOf(frame) === 'agent') return 'agent';
  // Main chat: the owner's own echo is also user_query-wrapped (🎯T537.1.2), so the
  // wrapper is provenance only on the RHS inspect surface (vanilla T221 was inspect-only).
  return inspect && isWrappedInjectUserText(raw) ? 'agent' : 'owner';
}

export type DisplayRowsOpts = { inspect?: boolean };

export function displayRows(frames: unknown[], opts?: DisplayRowsOpts): DisplayRow[] {
  const out: DisplayRow[] = [];
  let run = 0;
  let runItems: StepItem[] = [];
  let runWhen: number | undefined;
  const flush = () => {
    if (!run) return;
    out.push({
      kind: 'steps',
      text: stepsLabel(run),
      steps: run,
      items: runItems,
      when: runWhen,
    });
    run = 0;
    runItems = [];
    runWhen = undefined;
  };
  for (const f of frames) {
    const rec = asRec(f);
    if (rec.recorded === 'lossless') continue;
    if (
      rec.type === 'tool_result' ||
      rec.type === 'result' ||
      rec.type === 'progress' ||
      rec.type === 'system' ||
      rec.type === 'ux_state' ||
      rec.type === 'status' // 🎯T555.5: recovery chrome, never unsealed assistant
    ) {
      continue;
    }
    const when = frameWhen(f);
    if (isSendErrorFrame(f)) {
      const text = String(rec.text || '').trim();
      if (!text) continue;
      flush();
      const last = out[out.length - 1];
      if (last && last.kind === 'diagnostic' && diagnosticBase(last) === text) {
        const n = (last.count || 1) + 1;
        last.count = n;
        last.text = diagnosticLabel(text, n);
        continue;
      }
      out.push({ kind: 'diagnostic', text, count: 1, when });
      continue;
    }
    const addStep = (it: StepItem) => {
      runItems.push(it);
      run += 1;
      if (when != null) runWhen = when;
    };
    // Old foldDisplayEvent: agent_note is a turn-slot item, not a painted note.
    if (isAgentNote(f)) {
      for (const it of stepItems(f)) addStep(it);
      continue;
    }
    if (isUserFrame(f)) {
      const raw = proseText(f);
      // 🎯T233: harness injects fold to a compact ⋯ nugget with hover detail.
      const nug = classifyInjectUserText(raw);
      if (nug) {
        flush();
        out.push({ kind: 'steps', text: nug.label, steps: 0, inject: nug.injectKind, items: [{ cls: 'inject-detail', text: nug.detail }], when });
        continue;
      }
      if (isNonBoundaryUserText(raw)) continue;
      const text = normalizeOwnerEchoText(raw);
      if (!text) continue;
      const last = out[out.length - 1];
      if (last && last.kind === 'user' && normalizeOwnerEchoText(last.text) === text) continue;
      flush();
      out.push({ kind: 'user', text, when, origin: userTurnOrigin(f, raw, !!opts?.inspect) });
      continue;
    }
    // Walk content in order so a mixed text+tool_use frame reports every
    // tool (🎯T119.10). tool_result is not a second step.
    const bs = blocks(f);
    if (bs.length) {
      for (const b of bs) {
        if (b.type === 'tool_use') {
          addStep({ cls: 'tool-use', text: summariseToolUse(b) });
          continue;
        }
        if (b.type !== 'text' && b.type !== 'output_text') continue;
        const t = String(b.text || '').trim();
        if (!t || isSilentAssistantText(t)) continue;
        flush();
        out.push({ kind: 'assistant', text: t, when, sealed: isSealedAssistant(f) });
      }
      continue;
    }
    if (rec.type === 'tool_use') {
      addStep({ cls: 'tool-use', text: summariseToolUse(rec) });
      continue;
    }
    const t = proseText(f).trim();
    if (!t || isSilentAssistantText(t)) continue;
    flush();
    out.push({ kind: 'assistant', text: t, when, sealed: isSealedAssistant(f) });
  }
  flush();
  return out;
}
