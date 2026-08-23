// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useReducer, useRef } from 'react';
import { MuxClient } from '../mux/client';
import { transcriptChannel } from '../mux/protocol';
import { applyConversationEvent, emptyConversation, type ConversationEvent } from './reduce';
import type { MuxEnvelope } from '../mux/protocol';
import { pixelFixtureActive, pixelFixtureFrames } from '../visual/oldCockpitFixture';

export type { ConversationMeta } from './reduce';

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

export function useConversation(mux: MuxClient | null, name: string) {
  const [state, dispatch] = useReducer(applyConversationEvent, undefined, emptyConversation);
  const stateRef = useRef(state);
  stateRef.current = state;
  const fixture = pixelFixtureActive() && name === 'jevons';

  const frozenRef = useRef(false);

  useEffect(() => {
    if (fixture) return;
    if (!mux || !name) return;
    frozenRef.current = false;
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
    mux.openTranscript(name, { lo: -30, hi: 0 });
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
      pageOlder: (_limit?: number) => {},
      leaveLive: () => {},
      rejoinLive: () => {},
    };
  }

  const rejoinLive = () => {
    frozenRef.current = false;
    mux?.windowTranscript(name, { lo: -30, hi: 0 });
  };

  return {
    frames: state.frames,
    meta: state.meta,
    error: state.error,
    ready: state.ready,
    send: (text: string) => {
      const t = String(text || '').trim();
      if (!t) return;
      if (frozenRef.current) rejoinLive();
      dispatch({
        v: 1,
        ch: transcriptChannel(name),
        t: 'frame',
        body: {
          type: 'user',
          turn_origin: 'owner',
          message: { role: 'user', content: t },
        },
      });
      mux?.sendTranscript(name, t);
    },
    page: (end: number, limit: number) => mux?.pageTranscript(name, end, limit),
    pageOlder: (limit = 50) => {
      const first = rec(stateRef.current.frames[0]);
      if (typeof first.id === 'string') {
        mux?.pageTranscript(name, { before: first.id, limit });
      }
    },
    leaveLive: () => {
      if (frozenRef.current) return;
      const frames = stateRef.current.frames;
      const lo = rec(frames[0]).index;
      const hiIdx = rec(frames[frames.length - 1]).index;
      if (typeof lo !== 'number' || typeof hiIdx !== 'number') return;
      frozenRef.current = true;
      mux?.windowTranscript(name, { lo, hi: hiIdx + 1 });
    },
    rejoinLive,
  };
}
