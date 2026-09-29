// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { decideSend } from './sendQueue';
import { interruptAfterMs } from '../conversation/useConversation';
import { secondsLeft } from '../components/EscalationStrip';

// 🎯T899: an agent pane sends to a busy agent at once so the daemon can steer
// and escalate; the overseer pane still holds; explicit modes are untouched.
describe('T899 escalating send', () => {
  it('holds a busy send without escalation, sends it with escalation', () => {
    expect(decideSend({ busy: true, text: 'status?' })).toEqual({ action: 'enqueue', text: 'status?', reason: 'busy' });
    expect(decideSend({ busy: true, text: 'status?', escalate: true })).toEqual({ action: 'send', text: 'status?', interrupt: false });
  });
  it('keeps the interrupt chord and the offline hold', () => {
    expect(decideSend({ busy: true, interrupt: true, text: 'now', escalate: true })).toEqual({ action: 'send', text: 'now', interrupt: true });
    expect(decideSend({ busy: true, text: 'later', escalate: true, wireOpen: false })).toEqual({ action: 'enqueue', text: 'later', reason: 'offline' });
  });
  it('reads the escalation deadline from a send status', () => {
    expect(interruptAfterMs({ status: 'steered', interrupt_after_ms: 60000 })).toBe(60000);
    expect(interruptAfterMs({ status: 'queued' })).toBe(0);
    expect(interruptAfterMs(null)).toBe(0);
  });
  it('counts down in whole seconds and stops at zero', () => {
    expect(secondsLeft(10_500, 0)).toBe(11);
    expect(secondsLeft(1_000, 5_000)).toBe(0);
  });
});
