// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import type { EscalationNotice } from '../conversation/useConversation';

/** Seconds left before the deadline, floored at zero. */
export function secondsLeft(deadline: number, now: number): number {
  return Math.max(0, Math.ceil((deadline - now) / 1000));
}

/**
 * 🎯T899: a message steered into the agent's busy turn, counting down to the
 * interrupt that fires unless the agent takes it first.
 */
export function EscalationStrip(props: { notice: EscalationNotice | null; agent: string; onDismiss: () => void }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!props.notice) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [props.notice]);
  if (!props.notice) return null;
  const left = secondsLeft(props.notice.deadline, now);
  const label =
    left > 0
      ? `Steered into ${props.agent}'s busy turn · interrupts in ${left}s unless it takes the message`
      : `Interrupt sent to ${props.agent} unless it had already taken the message`;
  return (
    <div className="escalation-strip" role="status" aria-live="polite" title={props.notice.message || undefined}>
      <span className="escalation-text">{label}</span>
      <button type="button" className="escalation-dismiss" aria-label="Dismiss" onClick={props.onDismiss}>
        ×
      </button>
    </div>
  );
}
