// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { MuxClient } from '../mux/client';
import { transcriptChannel } from '../mux/protocol';

export type ConversationMeta = {
  older?: number;
  total?: number;
  start?: number;
};

export function useConversation(mux: MuxClient | null, name: string) {
  const [frames, setFrames] = useState<unknown[]>([]);
  const [meta, setMeta] = useState<ConversationMeta | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!mux || !name) return;
    setFrames([]);
    setMeta(null);
    setError(null);
    const unsub = mux.subscribe(transcriptChannel(name), (env) => {
      if (env.t === 'frame') {
        setFrames((cur) => [...cur, env.body]);
        return;
      }
      if (env.t === 'meta') {
        setMeta((env.body || {}) as ConversationMeta);
        return;
      }
      if (env.t === 'page') {
        const body = env.body as { lines?: unknown[] };
        const lines = Array.isArray(body?.lines) ? body.lines : [];
        setFrames((cur) => [...lines, ...cur]);
        return;
      }
      if (env.t === 'error') {
        const body = env.body as { error?: string };
        setError(body?.error || 'error');
      }
    });
    mux.openTranscript(name);
    return () => {
      mux.closeTranscript(name);
      unsub();
    };
  }, [mux, name]);

  return {
    frames,
    meta,
    error,
    send: (text: string) => mux?.sendTranscript(name, text),
    page: (end: number, limit: number) => mux?.pageTranscript(name, end, limit),
  };
}
