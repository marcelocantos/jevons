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

export function applyConversationEvent(
  state: ConversationState,
  env: ConversationEvent,
): ConversationState {
  if (env.t === 'reset') return emptyConversation();
  if (env.t === 'batch') {
    const body = (env.body || {}) as { frames?: unknown[] };
    const lines = Array.isArray(body.frames) ? body.frames : [];
    const next = reduceTranscriptBodies(lines);
    return { ...state, frames: next.frames, stream: next.stream };
  }
  if (env.t === 'frame') {
    const next = applyTranscriptFrame(state.frames, state.stream, env.body);
    return { ...state, frames: next.frames, stream: next.stream };
  }
  if (env.t === 'meta') {
    return { ...state, meta: (env.body || {}) as ConversationMeta, ready: true };
  }
  if (env.t === 'page') {
    const body = (env.body || {}) as {
      lines?: unknown[];
      start?: number;
      older?: number;
      total?: number;
    };
    const lines = Array.isArray(body.lines) ? body.lines : [];
    const start =
      typeof body.start === 'number'
        ? body.start
        : typeof body.older === 'number'
          ? body.older
          : 0;
    const total = typeof body.total === 'number' ? body.total : state.meta?.total;
    const older = lines.length === 0 || start <= 0 ? 0 : (typeof body.older === 'number' ? body.older : start);
    const sameWindow =
      !!state.meta &&
      typeof state.meta.start === 'number' &&
      state.meta.start === start &&
      lines.length > 0;
    if (sameWindow) {
      return {
        ...state,
        meta: { ...(state.meta || {}), start, total, older },
      };
    }
    const olderFrames = reduceTranscriptBodies(lines);
    return {
      ...state,
      frames: [...olderFrames.frames, ...state.frames],
      stream: offsetStream(state.stream, olderFrames.frames.length),
      meta: { ...(state.meta || {}), start, total, older },
    };
  }
  if (env.t === 'error') {
    const body = env.body as { error?: string };
    return { ...state, error: body?.error || 'error' };
  }
  return state;
}
