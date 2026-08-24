// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Shared mux-shaped frames for hermetic fold / stream oracles. */

export function userTurn(text: string): unknown {
  return { type: 'user', message: { role: 'user', content: [{ type: 'text', text }] } };
}

export function assistantProse(text: string, stopReason = 'end_turn'): unknown {
  return {
    type: 'assistant',
    message: {
      role: 'assistant',
      stop_reason: stopReason,
      content: [{ type: 'text', text }],
    },
  };
}

export function assistantTool(name: string, input?: Record<string, unknown>): unknown {
  return {
    type: 'assistant',
    message: { content: [{ type: 'tool_use', name, input: input || {} }] },
  };
}

export function agentNote(text: string): unknown {
  return { type: 'agent_note', text };
}

export function silentAssistant(text = '[silent] done'): unknown {
  return assistantProse(text);
}
