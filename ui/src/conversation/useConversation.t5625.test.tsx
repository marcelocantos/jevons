// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useConversation } from './useConversation';
import { useDrafts } from '../store/drafts';
import type { MuxEnvelope } from '../mux/protocol';

// 🎯T562.5: a composer send has a correlated outcome — an ack (status) or a
// definite failure (error) matched by the id the send carried — so a
// pre-delivery echo of the same text is never mistaken for success, and a
// failed send keeps the draft instead of silently losing it.

type Handler = (env: MuxEnvelope) => void;

function fakeMux() {
  const handlers = new Set<Handler>();
  const sent: { name: string; text: string; id: string }[] = [];
  let seq = 0;
  const mux = {
    subscribe: (_ch: string, h: Handler) => {
      handlers.add(h);
      return () => handlers.delete(h);
    },
    openTranscript: vi.fn(),
    closeTranscript: vi.fn(),
    windowTranscript: vi.fn(),
    sendTranscript: (name: string, text: string) => {
      const id = 's' + ++seq;
      sent.push({ name, text, id });
      return id;
    },
  };
  const emit = (env: MuxEnvelope) => {
    for (const h of handlers) h(env);
  };
  return { mux, emit, sent };
}

const CH = 'transcript:jv-worker';

beforeEach(() => {
  localStorage.clear();
  useDrafts.setState({ drafts: {} });
});

describe('useConversation correlated send outcome (T562.5)', () => {
  it('clears the draft on a status ack matched by id, not on bare text match', () => {
    const { mux, emit, sent } = fakeMux();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const { result } = renderHook(() => useConversation(mux as any, 'jv-worker'));

    useDrafts.getState().setDraft('jv-worker', 'go left');
    act(() => {
      result.current.send('go left');
    });
    expect(sent).toHaveLength(1);
    const id = sent[0].id;

    // A same-text frame with NO id correlation must not clear the draft —
    // this is the pre-delivery-echo hazard the target names.
    act(() => {
      emit({ v: 1, ch: CH, t: 'meta', body: {} });
    });
    expect(useDrafts.getState().drafts['jv-worker']).toBe('go left');

    // The daemon's status reply, correlated by id, is the real ack.
    act(() => {
      emit({ v: 1, ch: CH, t: 'status', body: { id, status: 'sent' } });
    });
    expect(useDrafts.getState().drafts['jv-worker']).toBe('');
  });

  it('a definite failure (error) matched by id keeps the draft for retry', () => {
    const { mux, emit, sent } = fakeMux();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const { result } = renderHook(() => useConversation(mux as any, 'jv-worker'));

    useDrafts.getState().setDraft('jv-worker', 'urgent fix');
    act(() => {
      result.current.send('urgent fix');
    });
    const id = sent[0].id;

    act(() => {
      emit({ v: 1, ch: CH, t: 'error', body: { id, error: 'agent not registered' } });
    });
    // Draft survives — a definite failure never silently discards the text.
    expect(useDrafts.getState().drafts['jv-worker']).toBe('urgent fix');
    // The failure is visible in the transcript as a diagnostic frame.
    const hasSendError = result.current.frames.some(
      (f) => f && typeof f === 'object' && (f as { type?: string }).type === 'send_error',
    );
    expect(hasSendError).toBe(true);
  });

  it('a status ack for a DIFFERENT id (a race with a second send) does not fire early', () => {
    const { mux, emit, sent } = fakeMux();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const { result } = renderHook(() => useConversation(mux as any, 'jv-worker'));

    useDrafts.getState().setDraft('jv-worker', 'second message');
    act(() => {
      result.current.send('second message');
    });
    const realId = sent[0].id;

    act(() => {
      emit({ v: 1, ch: CH, t: 'status', body: { id: 'not-mine', status: 'sent' } });
    });
    expect(useDrafts.getState().drafts['jv-worker']).toBe('second message');

    act(() => {
      emit({ v: 1, ch: CH, t: 'status', body: { id: realId, status: 'sent' } });
    });
    expect(useDrafts.getState().drafts['jv-worker']).toBe('');
  });
});
