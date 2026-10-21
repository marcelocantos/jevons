// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { isAgentForgedOwnerEvent } from './agentForgedOwnerTurns';
import { displayRows } from './display';

describe('agentForgedOwnerTurns', () => {
  it('marks the 2026-10-21 specimen as agent-origin in the transcript', () => {
    expect(isAgentForgedOwnerEvent('e:137063')).toBe(true);
    const rows = displayRows([
      {
        id: 'e:137063',
        type: 'user',
        turn_origin: 'owner',
        message: { role: 'user', content: [{ type: 'text', text: 'T1120 live-status-line worker check' }] },
      },
    ]);
    expect(rows[0]?.kind).toBe('user');
    expect(rows[0]?.origin).toBe('agent');
  });
});
