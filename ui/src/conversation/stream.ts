// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { isOwnerUserBarrierFrame } from './userText';

/** Grok ACP token join — same rules as web/scripts/chat_events.js (🎯T537.1.1). */

const TERMINAL_STOPS = new Set(['end_turn', 'stop_sequence', 'max_tokens']);

export type StreamJoin = {
  streamBubbles: Record<string, number>;
  openById: Record<string, boolean>;
  openStream: number;
  segmentEdgeById: Record<string, boolean>;
  segmentEdgePending: boolean;
};

export function emptyStream(): StreamJoin {
  return {
    streamBubbles: {},
    openById: {},
    openStream: -1,
    segmentEdgeById: {},
    segmentEdgePending: false,
  };
}

export function appendAssistantStream(prev: string, next: string): string {
  return String(prev || '') + String(next || '');
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
    let s = markEdge(stream, '');
    for (const id of Object.keys(s.openById)) {
      if (s.openById[id]) s = markEdge(s, id);
    }
    return { frames: [...frames, body], stream: s };
  }

  if (type !== 'assistant') {
    // 🎯T504: a real owner user seals open streams so later same-sid
    // text cannot grow the bubble above this row. T329 inject / protocol
    // frames are not barriers.
    const next = isOwnerUserBarrierFrame(body) ? emptyStream() : stream;
    return { frames: [...frames, body], stream: next };
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

  if (isTerminalAssistant(m)) {
    nextStream = seal(nextStream, sid);
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
