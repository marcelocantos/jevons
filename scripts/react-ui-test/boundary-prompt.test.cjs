// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

const assert = require('node:assert/strict');
const { test } = require('node:test');
const {
  MAX_JOURNEY_PROMPT_BYTES,
  prePrompt,
  preReaskPrompt,
  ownerPrompt,
  assertJourneyPromptShape,
  providerPasteDeclined,
} = require('./boundary-prompt.cjs');

const pre = 'PRE-00000000-0000-4000-8000-000000000001';
const post = 'POST-00000000-0000-4000-8000-000000000002';
const ack = 'ACK-00000000-0000-4000-8000-000000000003';
const helper = '/var/folders/zz/yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy/00000000000000000000000000000000/boundary-aside-abc123/boundary-xyz/wait.cjs';

test('T831 PRE and owner prompts stay under the paste threshold', () => {
  const preText = prePrompt(pre, helper, post);
  const ownerText = ownerPrompt(ack);
  assertJourneyPromptShape(preText, 'PRE');
  assertJourneyPromptShape(ownerText, 'owner');
  assert.equal(preReaskPrompt(pre, helper, post), preText);
  assert.ok(preText.length < MAX_JOURNEY_PROMPT_BYTES);
});

test('T831 detects provider paste-decline prose', () => {
  assert.equal(providerPasteDeclined('Your message contained only pasted text with no message of your own'), true);
  assert.equal(providerPasteDeclined('You pasted a set of instructions with no message of your own'), true);
  assert.equal(providerPasteDeclined('PRE-abc'), false);
});
