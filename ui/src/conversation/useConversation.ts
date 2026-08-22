// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useReducer } from 'react';
import { MuxClient } from '../mux/client';
import { transcriptChannel } from '../mux/protocol';
import { applyConversationEvent, emptyConversation, type ConversationEvent } from './reduce';
import type { MuxEnvelope } from '../mux/protocol';
import { pixelFixtureActive, pixelFixtureFrames } from '../visual/oldCockpitFixture';

export type { ConversationMeta } from './reduce';

export function useConversation(mux: MuxClient | null, name: string) {
  const [state, dispatch] = useReducer(applyConversationEvent, undefined, emptyConversation);
  const fixture = pixelFixtureActive() && name === 'jevons';

  useEffect(() => {
    if (fixture) return;
    if (!mux || !name) return;
    dispatch({ v: 1, ch: transcriptChannel(name), t: 'reset' });
    const ch = transcriptChannel(name);
    let buffer: unknown[] = [];
    let hydrating = true;
    const unsub = mux.subscribe(ch, (env: MuxEnvelope) => {
      if (env.t === 'reset') {
        buffer = [];
        hydrating = true;
        dispatch(env);
        return;
      }
      if (hydrating && env.t === 'frame') {
        if (env.body !== undefined) buffer.push(env.body);
        return;
      }
      if (env.t === 'meta') {
        hydrating = false;
        const frames = buffer;
        buffer = [];
        if (frames.length) {
          dispatch({ v: 1, ch, t: 'batch', body: { frames } } as ConversationEvent);
        }
        dispatch(env);
        return;
      }
      dispatch(env);
    });
    mux.openTranscript(name);
    return () => {
      mux.closeTranscript(name);
      unsub();
    };
  }, [mux, name, fixture]);

  if (fixture) {
    const frames = pixelFixtureFrames();
    return {
      frames,
      meta: { start: 0, older: 0, total: frames.length },
      error: '',
      ready: true,
      send: (_text: string) => {},
      page: (_end: number, _limit: number) => {},
    };
  }

  return {
    frames: state.frames,
    meta: state.meta,
    error: state.error,
    ready: state.ready,
    send: (text: string) => mux?.sendTranscript(name, text),
    page: (end: number, limit: number) => mux?.pageTranscript(name, end, limit),
  };
}
