// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useReducer } from 'react';
import { MuxClient } from '../mux/client';
import { transcriptChannel } from '../mux/protocol';
import { applyConversationEvent, emptyConversation } from './reduce';

export type { ConversationMeta } from './reduce';

export function useConversation(mux: MuxClient | null, name: string) {
  const [state, dispatch] = useReducer(applyConversationEvent, undefined, emptyConversation);

  useEffect(() => {
    if (!mux || !name) return;
    dispatch({ v: 1, ch: transcriptChannel(name), t: 'reset' });
    const unsub = mux.subscribe(transcriptChannel(name), dispatch);
    mux.openTranscript(name);
    return () => {
      mux.closeTranscript(name);
      unsub();
    };
  }, [mux, name]);

  return {
    frames: state.frames,
    meta: state.meta,
    error: state.error,
    ready: state.ready,
    send: (text: string) => mux?.sendTranscript(name, text),
    page: (end: number, limit: number) => mux?.pageTranscript(name, end, limit),
  };
}
