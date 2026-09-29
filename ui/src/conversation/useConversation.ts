// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { deliveryModeOf, type DeliveryMode } from '../composer/deliveryMode';
import { useEffect, useReducer, useRef, useState } from 'react';
import { MuxClient } from '../mux/client';
import { transcriptChannel } from '../mux/protocol';
import { applyConversationEvent, emptyConversation, type ConversationEvent } from './reduce';
import { optimisticReceived, PHASE_IDLE, phaseSampleFromUnknown } from './overseerPhase';
import type { MuxEnvelope } from '../mux/protocol';
import { normalizeOwnerEchoText, shouldAckPendingSend } from './display';
import { useDrafts } from '../store/drafts';
import { now as clockNow } from '../clock';

// 🎯T562.5: id correlates the daemon's per-send reply (status = ack,
// error = definite failure) to exactly THIS send. Text-echo matching alone
// cannot tell "the daemon confirmed my send" from "a pre-delivery paint of
// the same text arrived for some other reason" — the id-matched status/error
// is the real definite outcome; the echo match stays as a resilience
// fallback for a status frame that never arrives (dropped connection).
type PendingSend = { text: string; at: number; id?: string };

function clearDraftForText(name: string, text: string): void {
  const cur = useDrafts.getState().drafts[name] || '';
  if (normalizeOwnerEchoText(cur) === normalizeOwnerEchoText(text)) {
    useDrafts.getState().setDraft(name, '');
  }
}

function clearDraftIfEchoed(name: string, pending: PendingSend | null, frames: unknown[]): boolean {
  if (!pending || !shouldAckPendingSend(pending.text, frames, pending.at)) return false;
  clearDraftForText(name, pending.text);
  return true;
}

/** True when `env` is a status/error reply correlated to `pending` by id. */
function matchesPendingId(env: MuxEnvelope, pending: PendingSend | null): boolean {
  if (!pending || !pending.id) return false;
  if (env.t !== 'status' && env.t !== 'error') return false;
  const body = env.body && typeof env.body === 'object' ? (env.body as Record<string, unknown>) : {};
  return typeof body.id === 'string' && body.id === pending.id;
}

export type { ConversationMeta } from './reduce';

/**
 * 🎯T899: a message the daemon steered into this agent's busy turn, with the
 * moment the turn is interrupted unless the agent takes it first. Cleared
 * when the agent goes idle, which is when anything still queued is taken.
 */
export type EscalationNotice = { text: string; deadline: number; message: string };

/** Escalation deadline from a send's status body, in ms; 0 = none. */
export function interruptAfterMs(body: unknown): number {
  const v = body && typeof body === 'object' ? (body as Record<string, unknown>).interrupt_after_ms : undefined;
  return typeof v === 'number' && v > 0 ? v : 0;
}

function rec(v: unknown): Record<string, unknown> {
  return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
}

export function useConversation(mux: MuxClient | null, name: string) {
  const [state, dispatch] = useReducer(applyConversationEvent, undefined, emptyConversation);
  const stateRef = useRef(state);
  stateRef.current = state;

  const frozenRef = useRef(false);
  const pendingSendRef = useRef<PendingSend | null>(null);
  // 🎯T903: the last send, kept past its echo. A steered message usually
  // paints in the transcript before the daemon's status arrives, and the
  // echo retires pendingSendRef; the countdown still belongs to this send.
  const lastSendRef = useRef<PendingSend | null>(null);
  const [escalation, setEscalation] = useState<EscalationNotice | null>(null);

  useEffect(() => {
    if (!mux || !name) return;
    frozenRef.current = false;
    pendingSendRef.current = null;
    lastSendRef.current = null;
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
      // 🎯T562.5: the definite per-send outcome. status = daemon confirmed
      // this exact send (ack) — clear the draft NOW, do not wait for a
      // transcript echo that may be a same-text coincidence or may never
      // paint before the owner navigates away. error = definite failure —
      // stop waiting on this id but leave the draft alone (falls through to
      // the generic path below, which still paints the send_error diagnostic).
      // 🎯T899 / 🎯T903: an escalating send's status starts the countdown,
      // whether or not its echo already retired the pending send.
      if (env.t === 'status' && matchesPendingId(env, lastSendRef.current)) {
        const sent = lastSendRef.current;
        lastSendRef.current = null;
        const ms = interruptAfterMs(env.body);
        if (sent && ms > 0) {
          const body = rec(env.body);
          setEscalation({
            text: sent.text,
            deadline: clockNow() + ms,
            message: typeof body.message === 'string' ? body.message : '',
          });
        }
      }
      const idMatch = matchesPendingId(env, pendingSendRef.current);
      if (idMatch) {
        const pending = pendingSendRef.current;
        pendingSendRef.current = null;
        if (env.t === 'status' && pending) {
          clearDraftForText(name, pending.text);
          return;
        }
        // env.t === 'error': fall through so the send_error diagnostic frame
        // still paints; the draft is deliberately left untouched.
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
  }, [mux, name]);

  // 🎯T899: the agent going idle ends the wait; whatever was still queued
  // is taken now, so the notice has nothing left to count down to.
  const idleNow = (() => {
    const phase = phaseSampleFromUnknown(state.meta);
    return !!phase && phase.phase === PHASE_IDLE;
  })();
  useEffect(() => {
    if (escalation && idleNow) setEscalation(null);
  }, [escalation, idleNow]);
  useEffect(() => {
    setEscalation(null);
  }, [name]);

  const rejoinLive = () => {
    frozenRef.current = false;
    mux?.windowTranscript(name, { lo: -30, hi: 0 });
  };

  return {
    frames: state.frames,
    meta: state.meta,
    error: state.error,
    ready: state.ready,
    send: (text: string, opts?: { mode?: DeliveryMode; interrupt?: boolean }) => {
      const t = String(text || '').trim();
      const mode = deliveryModeOf(opts);
      const interrupt = mode === 'interrupt';
      if (!t && !interrupt) return;
      if (frozenRef.current) rejoinLive();
      if (!t) {
        mux?.interruptTranscript(name);
        return;
      }
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
      const id = mux?.sendTranscript(name, t, mode === 'submit' ? undefined : { mode });
      pendingSendRef.current = { text: t, at: stateRef.current.frames.length, id };
      lastSendRef.current = pendingSendRef.current;
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
    resend: (msgId: string) => mux?.resendTranscript(name, msgId),
    escalation,
    dismissEscalation: () => setEscalation(null),
  };
}
