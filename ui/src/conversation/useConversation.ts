// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useReducer, useRef } from 'react';
import { MuxClient } from '../mux/client';
import { transcriptChannel } from '../mux/protocol';
import { applyConversationEvent, emptyConversation, type ConversationEvent } from './reduce';
import { optimisticReceived } from './overseerPhase';
import type { MuxEnvelope } from '../mux/protocol';
import { pixelFixtureActive, pixelFixtureFrames } from '../visual/oldCockpitFixture';
import { normalizeOwnerEchoText, shouldAckPendingSend } from './display';
import { useDrafts } from '../store/drafts';

type PendingSend = { text: string; at: number };

function clearDraftIfEchoed(name: string, pending: PendingSend | null, frames: unknown[]): boolean {
  if (!pending || !shouldAckPendingSend(pending.text, frames, pending.at)) return false;
  const cur = useDrafts.getState().drafts[name] || '';
  if (normalizeOwnerEchoText(cur) === normalizeOwnerEchoText(pending.text)) {
    useDrafts.getState().setDraft(name, '');
  }
  return true;
}

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
  const pendingSendRef = useRef<PendingSend | null>(null);

  useEffect(() => {
    if (fixture) return;
    if (!mux || !name) return;
    frozenRef.current = false;
    pendingSendRef.current = null;
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
          const batch = { v: 1, ch, t: 'batch', body: { frames } } as ConversationEvent;
          const next = applyConversationEvent(stateRef.current, batch);
          stateRef.current = next;
          dispatch(batch);
        }
        const afterMeta = applyConversationEvent(stateRef.current, env);
        stateRef.current = afterMeta;
        dispatch(env);
        if (clearDraftIfEchoed(name, pendingSendRef.current, afterMeta.frames)) {
          pendingSendRef.current = null;
        }
        return;
      }
      const next = applyConversationEvent(stateRef.current, env);
      stateRef.current = next;
      dispatch(env);
      if (clearDraftIfEchoed(name, pendingSendRef.current, next.frames)) {
        pendingSendRef.current = null;
      }
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
      pendingSendRef.current = { text: t, at: stateRef.current.frames.length };
      // Optimistic received on send; the next interleaved progress/meta frame wins (🎯T555.2).
      if (name === 'jevons') {
        const env = {
          v: 1,
          ch: transcriptChannel(name),
          t: 'meta',
          body: { phase: optimisticReceived() },
        } as ConversationEvent;
        const next = applyConversationEvent(stateRef.current, env);
        stateRef.current = next;
        dispatch(env);
      }
      mux?.sendTranscript(name, t);
    },
    page: (end: number, limit: number) => mux?.pageTranscript(name, end, limit),
    pageOlder: (limit = 50) => {
      const first = rec(stateRef.current.frames[0]);
      if (typeof first.id === 'string' && first.id) {
        mux?.pageTranscript(name, { before: first.id, limit });
        return;
      }
      if (typeof first.index === 'number') {
        mux?.pageTranscript(name, { before: `e:${first.index}`, limit });
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
