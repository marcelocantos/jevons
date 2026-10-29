// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { classifyInjectUserText } from './inject';
import { TUI_PREVIEW_FAULT, type ProvisionalMap } from './provisional';
import { coalesceAssistantText, isSealedAssistant, joinAssistantTexts, streamIdOf } from './stream';
import { isAgentForgedOwnerEvent } from './agentForgedOwnerTurns';
import { turnOriginOf, type TurnOrigin } from './paint';
import { isGenericToolName, summariseInput } from './toolSummary';
import { now } from '../clock';
import { isAsideWireUserText } from './asideWire';

/** Lifted display fold: notes, ⋯ n steps, prose. Not a second hydrate. */

export type DisplayKind = 'user' | 'assistant' | 'steps' | 'diagnostic';

/** Client-only mux send nack (🎯T545.3). Not a journaled event. */
export const SEND_ERROR_TYPE = 'send_error';

export function isSendErrorFrame(frame: unknown): boolean {
  return asRec(frame).type === SEND_ERROR_TYPE;
}

export const PROVIDER_TRANSPORT_TYPE = 'provider_transport';

export function isProviderTransportFrame(frame: unknown): boolean {
  return asRec(frame).type === PROVIDER_TRANSPORT_TYPE;
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
  /** Canonical mux event identity for selecting an owner request. */
  id?: string;
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
  /** User only: the daemon's owner-message id (🎯T806). */
  msgId?: string;
  /**
   * User only: delivery state painted into the message row (🎯T811).
   * 'held' (🎯T1130): the daemon is holding this message behind a running
   * turn — same chrome slot as undelivered, but offers Steer / Cut-in
   * instead of Resend.
   */
  delivery?: { state: 'undelivered' | 'delivered' | 'held' | 'removed'; reason?: string };
  /** Assistant only, during displayRows: the bubble's stream (🎯T907). */
  streamId?: string;
  /** Assistant only, during displayRows: its untrimmed text (🎯T907). */
  raw?: string;
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

/** Prose block: typed text/output_text, or typeless `{text}` (daily statedb). */
export function isProseBlock(b: Record<string, unknown>): boolean {
  if (b.type === 'tool_use' || b.type === 'tool_result') return false;
  const t = String(b.text || '').trim();
  if (!t) return false;
  return !b.type || b.type === 'text' || b.type === 'output_text';
}

export function proseText(frame: unknown): string {
  const f = asRec(frame);
  if (typeof f.text === 'string' && f.text && f.type === 'agent_note') return f.text;
  const msg = message(frame);
  const content = msg?.content ?? f.content ?? f.text;
  if (typeof content === 'string') return content;
  return joinAssistantTexts(
    blocks(frame)
      .filter((b) => isProseBlock(b))
      .map((b) => String(b.text || '')),
  );
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
    if (isSendErrorFrame(f) || isProviderTransportFrame(f)) continue;
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
  const text = bs.filter((b) => isProseBlock(b));
  return tools.length > 0 && text.length === 0;
}

export function stepsLabel(n: number): string {
  if (n <= 0) return '';
  return '⋯ ' + n + (n === 1 ? ' step' : ' steps');
}

function formatToolExecutionSuffix(c: Record<string, unknown>, when?: number): string {
  const st = typeof c.execution_status === 'string' ? c.execution_status.trim().toLowerCase() : '';
  if (!st || st === 'completed' || st === 'complete' || st === 'succeeded' || st === 'success') return '';
  if (st === 'failed' || st === 'error') return ' (failed)';
  if (st === 'cancelled' || st === 'canceled') return ' (cancelled)';
  if (when !== undefined && (st === 'in_progress' || st === 'pending' || st === 'running')) {
    const sec = Math.max(0, Math.floor((now() - when) / 1000));
    if (sec >= 2) {
      const m = Math.floor(sec / 60);
      const s = sec % 60;
      return m > 0 ? ` (${m}m ${s}s)` : ` (${s}s)`;
    }
  }
  if (st === 'in_progress' || st === 'pending' || st === 'running') return ' (running)';
  return '';
}

export function summariseToolUse(c: Record<string, unknown>, when?: number): string {
  const name = typeof c.name === 'string' && c.name.trim() ? c.name : 'tool';
  const extra = summariseInput(c.input);
  const base = isGenericToolName(name) ? extra || name : extra ? name + ': ' + extra : name;
  return base + formatToolExecutionSuffix(c, when);
}

export function stepItems(frame: unknown): StepItem[] {
  if (isAgentNote(frame)) {
    const t = String(asRec(frame).text || '').trim();
    // 🎯T921: a supervisor's pass names its sender; it is not the owner's.
    const who = asRec(asRec(frame).notice).kind === 'supervisor-pass' ? 'Supervisor: ' : '';
    return t ? [{ cls: 'agent-note', text: who + t }] : [];
  }
  const f = asRec(frame);
  if (f.type === 'tool_result' || f.type === 'result') return [];
  const when = frameWhen(frame);
  if (f.type === 'tool_use') return [{ cls: 'tool-use', text: summariseToolUse(f, when) }];
  const out: StepItem[] = [];
  for (const b of blocks(frame)) {
    if (b.type === 'tool_use') out.push({ cls: 'tool-use', text: summariseToolUse(b, when) });
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
  const f = asRec(frame);
  const origin = f.turn_origin ?? f.turnOrigin ?? f.origin;
  if (typeof origin === 'string' && origin.trim().toLowerCase() === 'owner') return 'owner';
  // Main chat: the owner's own echo is also user_query-wrapped (🎯T537.1.2), so the
  // wrapper is provenance only on the RHS inspect surface (vanilla T221 was inspect-only).
  return inspect && isWrappedInjectUserText(raw) ? 'agent' : 'owner';
}

export type DisplayRowsOpts = {
  inspect?: boolean;
  /**
   * The owner's main chat with the overseer (🎯T1122). A turn the owner did
   * not start (a worker report, a fleet notice, a harness inject) is machine
   * traffic: its user row and the overseer's replies fold into that turn's
   * steps, so the live end shows the owner's conversation, and the machine
   * chatter is one click away instead of a column of "No action needed."
   */
  ownerChat?: boolean;
  /** Live TUI preview suffix keyed by stream_id (🎯T584). */
  provisionalByStream?: ProvisionalMap;
};

/** 🎯T569: identical assistant prose is one bubble, even after later steps. */
function alreadyPaintedAssistant(out: DisplayRow[], text: string): boolean {
  return out.some((r) => r.kind === 'assistant' && r.text === text);
}

function overlayProvisional(
  text: string,
  sealed: boolean,
  streamId?: string,
  provisional?: ProvisionalMap,
): string {
  if (sealed || !streamId || !provisional) return text;
  const prov = provisional[streamId];
  if (!text.trim() && prov) return prov;
  return text;
}

function pushAssistant(
  out: DisplayRow[],
  flush: () => void,
  text: string,
  when: number | undefined,
  sealed: boolean,
  streamId?: string,
  raw?: string,
): void {
  if (alreadyPaintedAssistant(out, text)) return;
  flush();
  const row: DisplayRow = { kind: 'assistant', text, when, sealed };
  if (streamId) {
    row.streamId = streamId;
    row.raw = raw ?? text;
  }
  out.push(row);
}

/** How far the rest of a cut line may run (🎯T907). */
const CUT_TAIL_MAX = 240;

/** Whether text stops at the end of a line or sentence. */
function endsAtBoundary(text: string): boolean {
  return text.trim() === '' || /[\n\r]\s*$/.test(text) || /[.!?:;]["')\]]?\s*$/.test(text);
}

/**
 * Where the rest of a cut line ends in text: just past the first line break
 * or sentence end; -1 when text holds neither yet (🎯T907).
 */
export function tailBoundary(text: string): number {
  const nl = text.search(/[\n\r]/);
  const sentence = /[.!?]["')\]]?(?=\s)/.exec(text);
  const ends = [nl >= 0 ? nl + 1 : -1, sentence ? sentence.index + sentence[0].length : -1].filter((n) => n >= 0);
  return ends.length ? Math.min(...ends) : -1;
}

/**
 * 🎯T907: an owner message that lands while an answer is streaming is
 * painted where it lands, and the answer continues below it — but a
 * sentence the owner's bubble cut into is finished above it. The rest of
 * the cut line (up to its line or sentence end) moves up to the cut bubble.
 * When the stream is interrupted before the line ends (another stream takes
 * over, the turn ends, a tool runs), what it said is all the rest of that
 * line and moves up whole; so do late fragments of it. Only owner bubbles
 * and other streams' bubbles may sit between the cut and its rest.
 */
function joinCutLines(rows: DisplayRow[]): DisplayRow[] {
  const dropped = new Set<number>();
  for (let i = 0; i < rows.length; i++) {
    const cut = rows[i];
    if (cut.kind !== 'assistant' || !cut.streamId || dropped.has(i)) continue;
    for (let j = i + 1; j < rows.length && !endsAtBoundary(cut.raw ?? cut.text); j++) {
      const r = rows[j];
      if (dropped.has(j)) continue;
      if (r.kind === 'steps') break; // a tool ran: the line is over
      if (r.kind !== 'assistant' || r.streamId !== cut.streamId) continue;
      if (!rows.slice(i + 1, j).some((x) => x.kind === 'user')) break; // not cut by the owner
      const rest = r.raw ?? r.text;
      const end = tailBoundary(rest);
      const interrupted = !!r.sealed || rows.slice(j + 1).some((x) => x.kind === 'assistant' && x.streamId !== cut.streamId) ||
        rows.slice(i + 1, j).some((x) => x.kind === 'assistant' && x.streamId !== cut.streamId);
      let head: string;
      if (end >= 0 && end <= CUT_TAIL_MAX) head = rest.slice(0, end);
      else if (interrupted && rest.length <= CUT_TAIL_MAX) head = rest;
      else break; // still streaming toward the end of the line
      cut.raw = coalesceAssistantText(cut.raw ?? cut.text, head);
      cut.text = cut.raw.trim();
      const left = rest.slice(head.length);
      if (left.trim()) {
        r.raw = left.replace(/^\s+/, '');
        r.text = r.raw.trim();
      } else {
        dropped.add(j);
      }
    }
  }
  const out = dropped.size ? rows.filter((_, k) => !dropped.has(k)) : rows;
  // The join state is not part of a painted row.
  for (const r of out) {
    delete r.raw;
    delete r.streamId;
  }
  return out;
}

export function displayRows(frames: unknown[], opts?: DisplayRowsOpts): DisplayRow[] {
  const out: DisplayRow[] = [];
  const userRows = new Map<string, DisplayRow>();
  const undeliveredRows = new Map<string, DisplayRow>();
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
  // 🎯T1122: inside a turn the owner did not start, nothing paints as a
  // bubble in the owner's chat (DisplayRowsOpts.ownerChat). A window that
  // opens mid-turn has no boundary yet, so it starts as machine traffic; the
  // mux window floor keeps the owner's latest turn in view (🎯T1159).
  let machineTurn = !!opts?.ownerChat;
  // The owner's turn ends when the overseer seals its reply (a terminal stop,
  // not tool_use). Prose after that and before the next boundary is a turn
  // nobody journalled a boundary for (transcripts before 🎯T1169): machine.
  let ownerSealedStream: string | undefined;
  let ownerSealed = false;
  const foldAfterSeal = (f: unknown) => ownerSealed && streamIdOf(f) !== ownerSealedStream;
  const noteSeal = (f: unknown) => {
    if (opts?.ownerChat && !machineTurn && isSealedAssistant(f)) {
      ownerSealed = true;
      ownerSealedStream = streamIdOf(f);
    }
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
    if (rec.type === TUI_PREVIEW_FAULT) {
      const text = String(rec.text || '').trim();
      if (!text) continue;
      flush();
      out.push({ kind: 'diagnostic', text, count: 1, when });
      continue;
    }
    if (isSendErrorFrame(f) || isProviderTransportFrame(f)) {
      const text = String(rec.text || '').trim();
      if (!text) continue;
      // 🎯T806: a delivered frame flips the undelivered row for the same
      // message in place, so the owner sees one row change state, not a pile.
      const msgId = typeof rec.msg_id === 'string' ? rec.msg_id : '';
      // 🎯T811: with the owner bubble on screen the state lives in that row.
      const owner = msgId ? userRows.get(msgId) : undefined;
      if (owner && (rec.state === 'undelivered' || rec.state === 'delivered' || rec.state === 'held' || rec.state === 'removed')) {
        owner.delivery =
          rec.state === 'undelivered'
            ? { state: 'undelivered', reason: typeof rec.reason === 'string' ? rec.reason : text }
            : rec.state === 'held'
              ? { state: 'held' }
              : rec.state === 'removed' ? { state: 'removed' } : { state: 'delivered' };
        continue;
      }
      flush();
      if (msgId && rec.state === 'delivered') {
        const prior = undeliveredRows.get(msgId);
        if (prior) {
          prior.text = text;
          prior.count = 1;
          undeliveredRows.delete(msgId);
          continue;
        }
      }
      const last = out[out.length - 1];
      if (last && last.kind === 'diagnostic' && diagnosticBase(last) === text) {
        const n = (last.count || 1) + 1;
        last.count = n;
        last.text = diagnosticLabel(text, n);
        continue;
      }
      const row: DisplayRow = { kind: 'diagnostic', text, count: 1, when };
      out.push(row);
      if (msgId && rec.state === 'undelivered') undeliveredRows.set(msgId, row);
      continue;
    }
    const addStep = (it: StepItem) => {
      runItems.push(it);
      run += 1;
      if (when != null) runWhen = when;
    };
    // Old foldDisplayEvent: agent_note is a turn-slot item, not a painted note.
    if (isAgentNote(f)) {
      if (opts?.ownerChat) machineTurn = true;
      for (const it of stepItems(f)) addStep(it);
      continue;
    }
    if (isUserFrame(f)) {
      const raw = proseText(f);
      // 🎯T250 / 🎯T264: [attention:] / [target-aside:] wires never paint on
      // main. Inspect (sidebar) still sees them so T265 can strip the header.
      if (!opts?.inspect && isAsideWireUserText(raw)) continue;
      const explicitOwner = rec.turn_origin === 'owner';
      // 🎯T233: harness injects fold to a compact ⋯ nugget with hover detail.
      // Canonical owner provenance wins even when the owner quotes a report.
      const nug = explicitOwner ? null : classifyInjectUserText(raw);
      if (nug) {
        if (opts?.ownerChat) machineTurn = true;
        flush();
        out.push({ kind: 'steps', text: nug.label, steps: 0, inject: nug.injectKind, items: [{ cls: 'inject-detail', text: nug.detail }], when });
        continue;
      }
      if (!explicitOwner && isNonBoundaryUserText(raw)) continue;
      const text = normalizeOwnerEchoText(raw);
      if (!text) continue;
      const frameId = asRec(f).id;
      const id = typeof frameId === 'string' ? frameId : undefined;
      let origin = userTurnOrigin(f, raw, !!opts?.inspect);
      if (isAgentForgedOwnerEvent(id)) {
        origin = 'agent';
      }
      if (opts?.ownerChat) {
        machineTurn = origin !== 'owner';
        ownerSealed = false;
        ownerSealedStream = undefined;
        if (machineTurn) {
          addStep({ cls: 'machine-message', text });
          continue;
        }
      }
      const last = out[out.length - 1];
      // Canonical identities distinguish real repeated prompts. Text-only
      // echo folding is limited to old unindexed frames of the same origin.
      if (last && last.kind === 'user' && last.origin === origin &&
        (id ? last.id === id : !last.id) && normalizeOwnerEchoText(last.text) === text) continue;
      flush();
      const msgId = typeof asRec(f).msg_id === 'string' ? (asRec(f).msg_id as string) : undefined;
      const row: DisplayRow = { kind: 'user', text, when, id, origin, msgId };
      out.push(row);
      if (msgId) userRows.set(msgId, row);
      continue;
    }
    // Walk content in order so a mixed text+tool_use frame reports every
    // tool (🎯T119.10). tool_result is not a second step.
    const bs = blocks(f);
    if (bs.length) {
      for (const b of bs) {
        if (b.type === 'tool_use') {
          addStep({ cls: 'tool-use', text: summariseToolUse(b, when) });
          continue;
        }
        if (!isProseBlock(b)) continue;
        const t = String(b.text || '').trim();
        if (!t || isSilentAssistantText(t)) continue;
        if (machineTurn || foldAfterSeal(f)) {
          addStep({ cls: 'machine-reply', text: t });
          continue;
        }
        pushAssistant(
          out,
          flush,
          overlayProvisional(t, isSealedAssistant(f), streamIdOf(f), opts?.provisionalByStream),
          when,
          isSealedAssistant(f),
          streamIdOf(f),
          String(b.text || ''),
        );
      }
      noteSeal(f);
      continue;
    }
    if (rec.type === 'tool_use') {
      addStep({ cls: 'tool-use', text: summariseToolUse(rec, when) });
      continue;
    }
    const rawText = proseText(f);
    const t = rawText.trim();
    if (!t || isSilentAssistantText(t)) continue;
    if (machineTurn || foldAfterSeal(f)) {
      addStep({ cls: 'machine-reply', text: t });
      continue;
    }
    pushAssistant(
      out,
      flush,
      overlayProvisional(t, isSealedAssistant(f), streamIdOf(f), opts?.provisionalByStream),
      when,
      isSealedAssistant(f),
      streamIdOf(f),
      rawText,
    );
    noteSeal(f);
  }
  if (opts?.provisionalByStream) {
    for (const [sid, prov] of Object.entries(opts.provisionalByStream)) {
      if (!prov.trim()) continue;
      if (out.some((r) => r.kind === 'assistant' && r.streamId === sid && !r.sealed)) continue;
      pushAssistant(out, flush, prov, undefined, false, sid, prov);
    }
  }
  flush();
  return joinCutLines(out);
}
