// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect, it } from 'vitest';
import { isOwnerUserBarrierFrame } from './userText';
import { applyConversationEvent, emptyConversation } from './reduce';

it.each([
  ['owner', '[event: report] is this correct?', true],
  ['owner', '{"type":"permission_request"}', true],
  ['owner', '', true],
  ['agent', 'ordinary agent prose', false],
  [undefined, '[event: report] completed', false],
  [undefined, '{"type":"permission_request"}', false],
  [undefined, 'an owner question', true],
] as const)('raw compatibility origin=%s text=%s barrier=%s', (origin, text, barrier) => {
  const user = { type: 'user', turn_origin: origin, message: { role: 'user', content: [{ type: 'text', text }] } };
  expect(isOwnerUserBarrierFrame(user)).toBe(barrier);
  const assistant = (text: string) => ({ type: 'assistant', stream_id: 'same', message: { role: 'assistant', content: [{ type: 'text', text }] } });
  let state = emptyConversation();
  for (const body of [assistant('before'), user, assistant('after')]) state = applyConversationEvent(state, { t: 'frame', body });
  const replies = state.frames.filter(frame => (frame as { type: string }).type === 'assistant');
  expect(replies).toHaveLength(barrier ? 2 : 1);
});
