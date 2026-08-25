// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { MuxEnvelope, MuxType } from '../mux/protocol';

export type ConversationEvent = Omit<MuxEnvelope, 't'> & {
  t: MuxType | 'batch';
};
import {
  applyTranscriptFrame,
  emptyStream,
  offsetStream,
  reduceTranscriptBodies,
  type StreamJoin,
} from './stream';

export type ConversationMeta = {
  older?: number;
  total?: number;
  start?: number;
  lo?: number;
  hi?: number;
  n?: number;
  following?: boolean;
  working?: boolean;
  owner_ux?: string;
  overseer_down?: string;
  truncated?: boolean;
};

export type ConversationState = {
  frames: unknown[];
  meta: ConversationMeta | null;
  error: string | null;
  ready: boolean;
  stream: StreamJoin;
};

export const emptyConversation = (): ConversationState => ({
  frames: [],
  meta: null,
  error: null,
  ready: false,
  stream: emptyStream(),
});

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

/** 🎯T537.1.3 framed event: identity + 1-based coalesced index. */
export function isWindowEvent(body: unknown): boolean {
  const o = rec(body);
  return typeof o.id === 'string' && typeof o.index === 'number';
}

function attachWindow(event: unknown, win: Record<string, unknown>): Record<string, unknown> {
  return { ...rec(event), id: win.id, index: win.index };
}

function windowPayload(body: unknown): unknown {
  const o = rec(body);
  if (o.event != null) return attachWindow(o.event, o);
  return o;
}

function findId(frames: unknown[], id: string): number {
  return frames.findIndex((f) => rec(f).id === id);
}

function eventMessage(body: unknown): Record<string, unknown> {
  return rec(rec(rec(body).event).message);
}

/** Mux append sends the full event alongside the token; copy terminal stop. */
function withStopFromWindow(frame: unknown, body: unknown): unknown {
  const msg = eventMessage(body);
  const stop = msg.stop_reason ?? msg.stopReason;
  if (typeof stop !== 'string' || !stop) return frame;
  const f = rec(frame);
  const fmsg = rec(f.message);
  return { ...f, message: { ...fmsg, stop_reason: stop } };
}

function appendTextToFrame(frame: unknown, text: string): unknown {
  const f = rec(frame);
  const msg = rec(f.message);
  const content = msg.content;
  if (typeof content === 'string') {
    return { ...f, message: { ...msg, content: content + text } };
  }
  if (Array.isArray(content)) {
    let joined = false;
    const next = content.map((b) => {
      const blk = rec(b);
      if (joined || (blk.type !== 'text' && blk.type !== 'output_text')) return b;
      joined = true;
      return { ...blk, text: String(blk.text || '') + text };
    });
    if (!joined) next.push({ type: 'text', text });
    return { ...f, message: { ...msg, content: next } };
  }
  return { ...f, message: { ...msg, content: [{ type: 'text', text }] } };
}

function sortByIndex(frames: unknown[]): unknown[] {
  return frames.slice().sort((a, b) => {
    const ia = rec(a).index;
    const ib = rec(b).index;
    const na = typeof ia === 'number' ? ia : 0;
    const nb = typeof ib === 'number' ? ib : 0;
    return na - nb;
  });
}

export function applyWindowBody(state: ConversationState, body: unknown): ConversationState {
  const o = rec(body);
  const id = String(o.id);
  const op = o.op === 'append' ? 'append' : 'put';
  if (op === 'append') {
    const idx = findId(state.frames, id);
    if (idx >= 0) {
      const frames = state.frames.slice();
      const text = typeof o.text === 'string' ? o.text : '';
      if (text) {
        frames[idx] = {
          ...rec(withStopFromWindow(appendTextToFrame(frames[idx], text), o)),
          id,
          index: o.index,
        };
      } else if (o.event != null) {
        frames[idx] = windowPayload(o);
      }
      return { ...state, frames: sortByIndex(frames) };
    }
  }
  const payload = windowPayload(o);
  const idx = findId(state.frames, id);
  const frames = state.frames.slice();
  if (idx >= 0) frames[idx] = payload;
  else frames.push(payload);
  return { ...state, frames: sortByIndex(frames) };
}

function applyWindowBodies(state: ConversationState, bodies: unknown[]): ConversationState {
  let next = state;
  for (const b of bodies) {
    if (isWindowEvent(b)) next = applyWindowBody(next, b);
    else {
      const folded = applyTranscriptFrame(next.frames, next.stream, b);
      next = { ...next, frames: folded.frames, stream: folded.stream };
    }
  }
  return next;
}

export function applyConversationEvent(
  state: ConversationState,
  env: ConversationEvent,
): ConversationState {
  if (env.t === 'reset') return emptyConversation();
  if (env.t === 'batch') {
    const body = (env.body || {}) as { frames?: unknown[] };
    const lines = Array.isArray(body.frames) ? body.frames : [];
    if (lines.some(isWindowEvent)) {
      return applyWindowBodies(state, lines);
    }
    const next = reduceTranscriptBodies(lines);
    return { ...state, frames: next.frames, stream: next.stream };
  }
  if (env.t === 'frame') {
    if (isWindowEvent(env.body)) {
      return applyWindowBody(state, env.body);
    }
    const next = applyTranscriptFrame(state.frames, state.stream, env.body);
    return { ...state, frames: next.frames, stream: next.stream };
  }
  if (env.t === 'meta') {
    // Working-level fans are partial ({working, owner_ux, overseer_down}).
    // Replacing the window meta drops older/truncated and PageUp dies
    // after the first-paint halo (🎯T494.1.4).
    return {
      ...state,
      meta: { ...(state.meta || {}), ...((env.body || {}) as ConversationMeta) },
      ready: true,
    };
  }
  if (env.t === 'page') {
    const body = (env.body || {}) as {
      lines?: unknown[];
      start?: number;
      older?: number;
      total?: number;
      lo?: number;
      hi?: number;
      n?: number;
      following?: boolean;
      truncated?: boolean;
    };
    const lines = Array.isArray(body.lines) ? body.lines : [];
    const start =
      typeof body.start === 'number'
        ? body.start
        : typeof body.older === 'number'
          ? body.older
          : 0;
    const total = typeof body.total === 'number' ? body.total : state.meta?.total;
    const truncated = body.truncated === true;
    // Empty lines means "already have this slice" (Need filtered), not EOF.
    // EOF is start <= 1 only when the journal is no longer truncated
    // (🎯T494.1.4 first-paint tail is not the journal head).
    const older =
      start <= 1 && !truncated
        ? 0
        : typeof body.older === 'number'
          ? body.older
          : start;
    const sameWindow =
      !!state.meta &&
      typeof state.meta.start === 'number' &&
      state.meta.start === start &&
      lines.length > 0;
    if (sameWindow) {
      return {
        ...state,
        meta: { ...(state.meta || {}), start, total, older, lo: body.lo, hi: body.hi, n: body.n, following: body.following, truncated },
      };
    }
    if (lines.some(isWindowEvent)) {
      const next = applyWindowBodies(state, lines);
      return {
        ...next,
        meta: { ...(next.meta || {}), start, total, older, lo: body.lo, hi: body.hi, n: body.n, following: body.following === true, truncated },
      };
    }
    const olderFrames = reduceTranscriptBodies(lines);
    return {
      ...state,
      frames: [...olderFrames.frames, ...state.frames],
      stream: offsetStream(state.stream, olderFrames.frames.length),
      meta: { ...(state.meta || {}), start, total, older, truncated },
    };
  }
  if (env.t === 'error') {
    const body = env.body as { error?: string };
    const text = String(body?.error || 'error').trim() || 'error';
    return {
      ...state,
      error: null,
      frames: [...state.frames, { type: 'send_error', text }],
    };
  }
  return state;
}
