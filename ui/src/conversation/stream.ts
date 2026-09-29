// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Grok ACP token join — same rules as web/scripts/chat_events.js (🎯T537.1.1). */

import { isOwnerUserBarrierFrame } from './userText';

const TERMINAL_STOPS = new Set(['end_turn', 'stop_sequence', 'max_tokens']);

export type StreamJoin = {
  streamBubbles: Record<string, number>;
  openById: Record<string, boolean>;
  openStream: number;
  segmentEdgeById: Record<string, boolean>;
  segmentEdgePending: boolean;
  /**
   * 🎯T907: an owner bubble landed while the assistant bubble at idx ended
   * mid-line. The stream's next text, up to the end of that line or
   * sentence, still belongs to that bubble; the rest goes below the owner's.
   */
  owedTail?: { sid: string; idx: number } | null;
};

export function emptyStream(): StreamJoin {
  return {
    streamBubbles: {},
    openById: {},
    openStream: -1,
    segmentEdgeById: {},
    segmentEdgePending: false,
    owedTail: null,
  };
}

/** Whether text stops at the end of a line or sentence (🎯T907). */
function endsAtBoundary(text: string): boolean {
  return text === '' || /[\n\r]$/.test(text) || /[.!?:;]["')\]]?\s*$/.test(text);
}

/**
 * Where the owed tail of a broken line ends in text (🎯T907): just past the
 * first line break or sentence end. -1 when text holds neither yet.
 */
export function tailBoundary(text: string): number {
  const nl = text.search(/[\n\r]/);
  const sentence = /[.!?]["')\]]?(?=\s)/.exec(text);
  const ends = [nl >= 0 ? nl + 1 : -1, sentence ? sentence.index + sentence[0].length : -1].filter((n) => n >= 0);
  return ends.length ? Math.min(...ends) : -1;
}

function frameText(frame: unknown): string {
  const msg = messageOf(frame);
  if (typeof msg.content === 'string') return msg.content;
  return contentBlocks(frame)
    .filter((b) => b.type === 'text')
    .map((b) => String(b.text || ''))
    .join('');
}

/** How far the continuation may run looking for the end of the cut line. */
const OWED_TAIL_MAX = 240;

/** The bubble the continuation of sid is painting below the owner's. */
function continuationIdx(stream: StreamJoin, sid: string): number {
  const at = sid ? stream.streamBubbles[sid] : stream.openStream;
  return at == null ? -1 : at;
}

/**
 * 🎯T907: once the continuation below the owner's bubble reaches the end of
 * the line it cut into, that head moves up to finish the line.
 */
function settleOwedTail(frames: unknown[], stream: StreamJoin, sid: string): unknown[] {
  const owed = stream.owedTail;
  const at = continuationIdx(stream, sid);
  if (!owed || owed.sid !== sid || at < 0 || at === owed.idx || owed.idx >= frames.length) return frames;
  const text = frameText(frames[at]);
  const cut = tailBoundary(text);
  if (cut < 0 || cut > OWED_TAIL_MAX) return frames;
  const out = frames.slice();
  out[owed.idx] = concatIntoFrame(out[owed.idx], text.slice(0, cut), false);
  out[at] = concatIntoFrame(withoutText(out[at]), text.slice(cut).replace(/^\s+/, ''), false);
  return out;
}

/** Whether the owed tail is resolved: moved up, or past its budget. */
function owedTailSettled(frames: unknown[], stream: StreamJoin, sid: string): boolean {
  const at = continuationIdx(stream, sid);
  if (at < 0 || !stream.owedTail || at === stream.owedTail.idx) return false;
  const text = frameText(frames[at]);
  const cut = tailBoundary(text);
  // A continuation that already gave up its head starts at a boundary.
  return (cut >= 0 && cut <= OWED_TAIL_MAX) || text.length > OWED_TAIL_MAX || frameMovedUp(frames, stream);
}

function frameMovedUp(frames: unknown[], stream: StreamJoin): boolean {
  return !!stream.owedTail && endsAtBoundary(frameText(frames[stream.owedTail.idx]));
}

/** frame with its text blocks removed (tool blocks kept). */
function withoutText(frame: unknown): unknown {
  const f = { ...rec(frame) };
  const msg = { ...messageOf(f) };
  msg.content = typeof msg.content === 'string' ? '' : contentBlocks(f).filter((b) => b.type !== 'text');
  f.message = msg;
  return f;
}

/**
 * Join-time ACP segment repair (vanilla 🎯T147; React restore 🎯T645).
 * T145 ensureFenceNewlines is display-only; this is the join helper.
 * Fence opener with no boundary newline → blank line. Sentence punct +
 * capital → a space. Otherwise bare concat (intra-token streams).
 */
export function coalesceAssistantText(
  prev: string | null | undefined,
  next: string | null | undefined,
): string {
  const a = String(prev ?? '');
  const b = String(next ?? '');
  if (!a) return b;
  if (!b) return a;
  if (/[\n\r]$/.test(a) || /^[\n\r]/.test(b)) return a + b;
  if (/^```/.test(b)) return a + '\n\n' + b;
  if (/[.!?]$/.test(a) && /^[A-Z]/.test(b)) return a + ' ' + b;
  return a + b;
}

export function joinAssistantTexts(
  parts: Array<string | null | undefined> | null | undefined,
): string {
  let acc = '';
  for (const p of parts || []) {
    acc = coalesceAssistantText(acc, p);
  }
  return acc;
}

export function appendAssistantStream(prev: string, next: string): string {
  return coalesceAssistantText(prev, next);
}

export function joinAssistantSegments(prev: string, next: string): string {
  const a = String(prev || '');
  const b = String(next || '');
  if (!a) return b;
  if (!b) return a;
  if (/[\n\r]$/.test(a) || /^[\n\r]/.test(b)) return a + b;
  return a + '\n\n' + b;
}

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

export function streamIdOf(m: unknown): string {
  const o = rec(m);
  const id = o.stream_id != null ? o.stream_id : o.streamId;
  return id == null ? '' : String(id).trim();
}

function messageOf(m: unknown): Record<string, unknown> {
  return rec(rec(m).message);
}

function stopReason(m: unknown): string {
  const msg = messageOf(m);
  const r = msg.stop_reason ?? msg.stopReason;
  return typeof r === 'string' ? r : '';
}

/** Terminal stop on the frame — any unsealed assistant keeps a stream session. */
export function isSealedAssistant(m: unknown): boolean {
  return TERMINAL_STOPS.has(stopReason(m));
}

export function isTerminalAssistant(m: unknown): boolean {
  return rec(m).type === 'assistant' && isSealedAssistant(m);
}

function contentBlocks(m: unknown): Record<string, unknown>[] {
  const c = messageOf(m).content ?? rec(m).content;
  return Array.isArray(c) ? c.filter((b) => b && typeof b === 'object') as Record<string, unknown>[] : [];
}

function concatIntoFrame(frame: unknown, next: string, edge: boolean): unknown {
  const f = { ...rec(frame) };
  const msg = { ...messageOf(f) };
  const join = edge ? joinAssistantSegments : appendAssistantStream;
  if (typeof msg.content === 'string') {
    msg.content = join(msg.content, next);
    f.message = msg;
    return f;
  }
  const blocks = contentBlocks(f);
  let found = false;
  const nextBlocks = blocks.map((b) => {
    if (found || b.type !== 'text') return b;
    found = true;
    return { ...b, text: join(String(b.text || ''), next) };
  });
  if (!found) nextBlocks.push({ type: 'text', text: next });
  msg.content = nextBlocks;
  f.message = msg;
  return f;
}

function replaceFrame(frames: unknown[], idx: number, frame: unknown): unknown[] {
  const out = frames.slice();
  out[idx] = frame;
  return out;
}

function markEdge(stream: StreamJoin, sid: string): StreamJoin {
  if (sid) {
    if (stream.openById[sid]) {
      return { ...stream, segmentEdgeById: { ...stream.segmentEdgeById, [sid]: true } };
    }
    return stream;
  }
  if (stream.openStream >= 0) return { ...stream, segmentEdgePending: true };
  return stream;
}

function seal(stream: StreamJoin, sid: string): StreamJoin {
  if (sid) {
    const openById = { ...stream.openById };
    const segmentEdgeById = { ...stream.segmentEdgeById };
    delete openById[sid];
    delete segmentEdgeById[sid];
    const openStream = stream.streamBubbles[sid] === stream.openStream ? -1 : stream.openStream;
    return { ...stream, openById, segmentEdgeById, openStream, segmentEdgePending: false };
  }
  return { ...stream, openStream: -1, segmentEdgePending: false };
}

export function offsetStream(stream: StreamJoin, n: number): StreamJoin {
  if (n === 0) return stream;
  const streamBubbles: Record<string, number> = {};
  for (const [k, v] of Object.entries(stream.streamBubbles)) streamBubbles[k] = v + n;
  return {
    ...stream,
    streamBubbles,
    openStream: stream.openStream >= 0 ? stream.openStream + n : -1,
  };
}

/** Fold one journal/mux body into frames. Assistant tokens join by stream_id. */
export function applyTranscriptFrame(
  frames: unknown[],
  stream: StreamJoin,
  body: unknown,
): { frames: unknown[]; stream: StreamJoin } {
  const m = rec(body);
  const type = m.type;

  if (type === 'tool_result' || type === 'result') {
    let s: StreamJoin = { ...markEdge(stream, ''), owedTail: null };
    for (const id of Object.keys(s.openById)) {
      if (s.openById[id]) s = markEdge(s, id);
    }
    return { frames: [...frames, body], stream: s };
  }

  if (type !== 'assistant') {
    // 🎯T504: a real owner user seals every open assistant stream AND drops
    // its stream_id → bubble mapping, so a same-stream continuation paints a
    // new bubble below the user instead of growing the pre-user row. T329
    // inject / T362 protocol frames are not barriers.
    if (isOwnerUserBarrierFrame(body)) {
      // 🎯T907: a bubble cut off mid-line is owed the rest of that line.
      let owedTail = stream.owedTail ?? null;
      const open = stream.openStream;
      if (open >= 0 && !endsAtBoundary(frameText(frames[open]))) {
        const sid = Object.keys(stream.streamBubbles).find((k) => stream.streamBubbles[k] === open) ?? '';
        owedTail = { sid, idx: open };
      }
      return { frames: [...frames, body], stream: { ...emptyStream(), owedTail } };
    }
    return { frames: [...frames, body], stream };
  }

  const sid = streamIdOf(m);
  const blocks = contentBlocks(m);
  let nextFrames = frames;
  let nextStream = stream;
  let pushedSelf = false;
  let textParts = 0;

  const takeText = (text: string) => {
    let idx = -1;
    let edge = false;
    if (sid) {
      if (nextStream.streamBubbles[sid] == null) {
        if (!pushedSelf) {
          nextFrames = [...nextFrames, body];
          pushedSelf = true;
        }
        idx = nextFrames.length - 1;
        nextStream = {
          ...nextStream,
          streamBubbles: { ...nextStream.streamBubbles, [sid]: idx },
          openById: { ...nextStream.openById, [sid]: true },
          segmentEdgeById: { ...nextStream.segmentEdgeById, [sid]: false },
          openStream: idx,
        };
        return;
      }
      idx = nextStream.streamBubbles[sid];
      edge = !!(nextStream.segmentEdgeById[sid] || textParts > 0);
      nextFrames = replaceFrame(nextFrames, idx, concatIntoFrame(nextFrames[idx], text, edge));
      nextStream = {
        ...nextStream,
        segmentEdgeById: { ...nextStream.segmentEdgeById, [sid]: false },
        openStream: idx,
      };
      return;
    }
    if (nextStream.openStream >= 0) {
      idx = nextStream.openStream;
      edge = !!(nextStream.segmentEdgePending || textParts > 0);
      nextFrames = replaceFrame(nextFrames, idx, concatIntoFrame(nextFrames[idx], text, edge));
      nextStream = { ...nextStream, segmentEdgePending: false };
      return;
    }
    if (!pushedSelf) {
      nextFrames = [...nextFrames, body];
      pushedSelf = true;
    }
    nextStream = { ...nextStream, openStream: nextFrames.length - 1, segmentEdgePending: false };
  };

  if (blocks.length) {
    for (const c of blocks) {
      if (c.type === 'text' && c.text) {
        takeText(String(c.text));
        textParts += 1;
      } else if (c.type) {
        nextStream = markEdge(nextStream, sid);
        if (c.type === 'tool_use' && !pushedSelf && textParts === 0) {
          nextFrames = [...nextFrames, body];
          pushedSelf = true;
        }
      }
    }
  } else if (typeof messageOf(m).content === 'string' && messageOf(m).content) {
    takeText(String(messageOf(m).content));
  } else if (!pushedSelf && !isTerminalAssistant(m)) {
    nextFrames = [...nextFrames, body];
    pushedSelf = true;
  }

  nextFrames = settleOwedTail(nextFrames, nextStream, sid);
  if (nextStream.owedTail && (nextStream.owedTail.sid !== sid || owedTailSettled(nextFrames, nextStream, sid))) {
    nextStream = { ...nextStream, owedTail: null };
  }
  if (isTerminalAssistant(m)) {
    nextStream = { ...seal(nextStream, sid), owedTail: null };
  }
  return { frames: nextFrames, stream: nextStream };
}

export function reduceTranscriptBodies(bodies: unknown[]): { frames: unknown[]; stream: StreamJoin } {
  let frames: unknown[] = [];
  let stream = emptyStream();
  for (const b of bodies) {
    const next = applyTranscriptFrame(frames, stream, b);
    frames = next.frames;
    stream = next.stream;
  }
  return { frames, stream };
}
