// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Lifted display fold: notes, ⋯ n steps, prose. Not a second hydrate. */

export type DisplayKind = 'user' | 'assistant' | 'steps';

export type StepItem = { cls: string; text: string };

export type DisplayRow = {
  kind: DisplayKind;
  text: string;
  steps?: number;
  items?: StepItem[];
  when?: number;
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

function gist(input: unknown): string {
  if (input == null) return '';
  if (typeof input === 'string') return input.replace(/\s+/g, ' ').trim().slice(0, 60);
  if (typeof input !== 'object') return String(input);
  const o = input as Record<string, unknown>;
  for (const k of ['query', 'path', 'command', 'title', 'name', 'text', 'url']) {
    const v = o[k];
    if (typeof v === 'string' && v.trim()) return v.replace(/\s+/g, ' ').trim().slice(0, 60);
  }
  return '';
}

export function summariseToolUse(c: Record<string, unknown>): string {
  const name = typeof c.name === 'string' && c.name.trim() ? c.name : 'tool';
  const extra = gist(c.input);
  return extra ? name + ': ' + extra : name;
}

export function stepItem(frame: unknown): StepItem | null {
  if (isAgentNote(frame)) {
    const t = String(asRec(frame).text || '').trim();
    return t ? { cls: 'agent-note', text: t } : null;
  }
  const f = asRec(frame);
  if (f.type === 'tool_use') return { cls: 'tool-use', text: summariseToolUse(f) };
  for (const b of blocks(frame)) {
    if (b.type === 'tool_use') return { cls: 'tool-use', text: summariseToolUse(b) };
  }
  return null;
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
    const when = frameWhen(f);
    // Old foldDisplayEvent: agent_note is a turn-slot item, not a painted note.
    if (isAgentNote(f) || isToolOnly(f)) {
      const it = stepItem(f);
      if (it) runItems.push(it);
      run += 1;
      if (when != null) runWhen = when;
      continue;
    }
    if (isUserFrame(f)) {
      const raw = proseText(f);
      if (isNonBoundaryUserText(raw)) continue;
      const text = normalizeOwnerEchoText(raw);
      if (!text) continue;
      const last = out[out.length - 1];
      if (last && last.kind === 'user' && normalizeOwnerEchoText(last.text) === text) continue;
      flush();
      out.push({ kind: 'user', text, when });
      continue;
    }
    const t = proseText(f).trim();
    if (!t) continue;
    flush();
    out.push({ kind: 'assistant', text: t, when });
  }
  flush();
  return out;
}
