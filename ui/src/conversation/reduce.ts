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
    const body = env.body as { lines?: unknown[] };
    const lines = Array.isArray(body?.lines) ? body.lines : [];
    return { ...state, frames: [...lines, ...state.frames] };
  }
  if (env.t === 'error') {
    const body = env.body as { error?: string };
    return { ...state, error: body?.error || 'error' };
  }
  return state;
}
