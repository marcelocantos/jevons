// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// J31 owner/PRE prompt shape (🎯T831). Kept short and single-line so the
// Claude provider path does not collapse the journey text into a paste block
// (claudia pasteBlockThreshold is 400 bytes; newlines force the paste branch).

const assert = require('node:assert/strict');

/** Bytes under claudia's pasteBlockThreshold with room for long temp paths. */
const MAX_JOURNEY_PROMPT_BYTES = 380;

const PASTE_DECLINE_MARKERS = [
  'only pasted text',
  'pasted a set of instructions',
  'nothing typed by you',
  'entirely pasted text',
];

function prePrompt(pre, helper, post) {
  return `Do this now: write ${pre}, then run node ${helper}, then reply ${post}.`;
}

function preReaskPrompt(pre, helper, post) {
  return prePrompt(pre, helper, post);
}

function ownerPrompt(ack) {
  return `Reply with exactly: ${ack}`;
}

function assertJourneyPromptShape(text, label = 'journey prompt') {
  assert.equal(typeof text, 'string', `${label} must be a string`);
  assert(!text.includes('\n'), `${label} must be a single line (no newlines)`);
  assert(text.length <= MAX_JOURNEY_PROMPT_BYTES,
    `${label} len=${text.length} exceeds ${MAX_JOURNEY_PROMPT_BYTES}`);
  assert(!/^(perform|first emit|step \d|do not spawn)/i.test(text.trim()),
    `${label} must not read like a multi-step instruction block`);
}

function providerPasteDeclined(text) {
  const lower = String(text || '').toLowerCase();
  return PASTE_DECLINE_MARKERS.some(marker => lower.includes(marker));
}

module.exports = {
  MAX_JOURNEY_PROMPT_BYTES,
  prePrompt,
  preReaskPrompt,
  ownerPrompt,
  assertJourneyPromptShape,
  providerPasteDeclined,
};
