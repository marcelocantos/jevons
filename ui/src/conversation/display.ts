// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { shouldPaintMainUserText } from './asideWire';
import { turnOriginOf, type TurnOrigin } from './paint';
import { isSealedAssistant } from './stream';
import { isGenericToolName, summariseInput } from './toolSummary';
import { isNonBoundaryUserText, normalizeOwnerEchoText } from './userText';

export { isNonBoundaryUserText, isProtocolControlFrameText, normalizeOwnerEchoText } from './userText';

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
  items?: StepItem[];
  when?: number;
  /** Assistant only: incremental smd until a terminal stop_reason. */
  sealed?: boolean;
  /** User only: wire provenance (🎯T381). Unmarked is owner. */
  origin?: TurnOrigin;
  /** Diagnostic only: consecutive identical nacks (🎯T545.3). */
  count?: number;
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

const SILENT_PREFIX = '[silent]';

/** Owner-filterable ops reply (🎯T238 / T240 / T245). Port of web ChatEvents.isSilentAssistantText. */
export function isSilentAssistantText(text: string): boolean {
  const t = String(text ?? '').trim();
  if (!t) return false;
  const lower = t.toLowerCase();
  const pref = SILENT_PREFIX.toLowerCase();
  if (lower.startsWith(pref)) return true;
  const head = t.length > 80 ? t.slice(0, 80) : t;
  for (const line of head.split('\n')) {
    if (line.trim().toLowerCase().startsWith(pref)) return true;
  }
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

export function displayRows(frames: unknown[]): DisplayRow[] {
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
    if (rec.type === 'tool_result' || rec.type === 'result' || rec.type === 'progress' || rec.type === 'system') {
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
      if (!shouldPaintMainUserText(raw)) continue;
      if (isNonBoundaryUserText(raw)) continue;
      const text = normalizeOwnerEchoText(raw);
      if (!text) continue;
      const last = out[out.length - 1];
      if (last && last.kind === 'user' && normalizeOwnerEchoText(last.text) === text) continue;
      flush();
      out.push({ kind: 'user', text, when, origin: turnOriginOf(f) });
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
