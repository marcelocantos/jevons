// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Lifted display fold: notes, ⋯ n steps, prose. Not a second hydrate. */

export type DisplayKind = 'user' | 'assistant' | 'note' | 'steps';

export type DisplayRow = {
  kind: DisplayKind;
  text: string;
  steps?: number;
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

export function isAgentNote(frame: unknown): boolean {
  return asRec(frame).type === 'agent_note';
}

export function isUserFrame(frame: unknown): boolean {
  const t = asRec(frame).type;
  const role = message(frame)?.role;
  return t === 'user' || role === 'user';
}

export function isToolOnly(frame: unknown): boolean {
  const bs = blocks(frame);
  if (!bs.length) {
    const t = asRec(frame).type;
    return t === 'tool_use' || t === 'tool_result';
  }
  const tools = bs.filter((b) => b.type === 'tool_use' || b.type === 'tool_result');
  const text = bs.filter((b) => (b.type === 'text' || b.type === 'output_text') && String(b.text || '').trim());
  return tools.length > 0 && text.length === 0;
}

export function stepsLabel(n: number): string {
  if (n <= 0) return '';
  return '⋯ ' + n + (n === 1 ? ' step' : ' steps');
}

export function displayRows(frames: unknown[]): DisplayRow[] {
  const out: DisplayRow[] = [];
  let run = 0;
  const flush = () => {
    if (!run) return;
    out.push({ kind: 'steps', text: stepsLabel(run), steps: run });
    run = 0;
  };
  for (const f of frames) {
    if (isAgentNote(f)) {
      flush();
      out.push({ kind: 'note', text: proseText(f) });
      continue;
    }
    if (isToolOnly(f)) {
      run += 1;
      continue;
    }
    if (isUserFrame(f)) {
      flush();
      out.push({ kind: 'user', text: proseText(f) });
      continue;
    }
    const t = proseText(f).trim();
    if (!t) continue;
    flush();
    out.push({ kind: 'assistant', text: t });
  }
  flush();
  return out;
}
