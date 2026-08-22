// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import type { MuxEnvelope } from '../mux/protocol';

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
};

export const emptyConversation = (): ConversationState => ({
  frames: [],
  meta: null,
  error: null,
  ready: false,
});

export function applyConversationEvent(
  state: ConversationState,
  env: MuxEnvelope,
): ConversationState {
  if (env.t === 'reset') return emptyConversation();
  if (env.t === 'frame') {
    return { ...state, frames: [...state.frames, env.body] };
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
    return {
      ...state,
      frames: sameWindow ? state.frames : [...lines, ...state.frames],
      meta: { ...(state.meta || {}), start, total, older },
    };
  }
  if (env.t === 'error') {
    const body = env.body as { error?: string };
    return { ...state, error: body?.error || 'error' };
  }
  return state;
}
