// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T831 hermetic: J31 prompt length/shape and paste-decline classifier.
const assert = require('node:assert/strict');
const { test } = require('node:test');
const {
  MAX_PROMPT_CHARS,
  isPasteDecline,
  prePrompt,
  reaskPrompt,
  ownerPrompt,
  assertPromptShape,
} = require('./boundary-prompts.cjs');

const longHelper =
  '/var/folders/fn/8ffdcmbd13lfwzsm2h93xrx00000gn/T/jevons-journey-999/boundary-aside-zzzzzzzz/boundary-wwwwwwww/wait.cjs';
const pre = 'PRE-77818bed-8e1e-4da8-9cd8-b61033984efd';
const post = 'POST-2b0e9442-9466-4053-8eb9-78e2d87e965b';
const ack = 'ACK-d8b35bf3-731d-492a-9eba-68a1159b6d0e';

test('T831 PRE prompt is a short single-line request under the paste ceiling', () => {
  const text = assertPromptShape('PRE', prePrompt({ pre, post, helper: longHelper }));
  assert.ok(text.length <= MAX_PROMPT_CHARS, text.length);
  assert.ok(!text.includes('\n'));
  assert.match(text, /^Please do this now:/);
  assert.ok(text.includes(pre) && text.includes(post) && text.includes(longHelper));
});

test('T831 re-ask prompt is a short single-line request under the paste ceiling', () => {
  const text = assertPromptShape('re-ask', reaskPrompt({ pre, post, helper: longHelper }));
  assert.ok(text.length <= MAX_PROMPT_CHARS, text.length);
  assert.ok(!text.includes('\n'));
  assert.match(text, /^Do this now:/);
});

test('T831 owner follow-up is a short single-line request', () => {
  const text = assertPromptShape('owner', ownerPrompt(ack));
  assert.ok(text.length < 80);
  assert.equal(text, `Reply with exactly: ${ack}`);
});

test('T831 rejects the pre-fix long instruction dump that invited paste declines', () => {
  const legacy =
    `Perform this short interaction check in order. First emit ${pre} as a visible commentary message, not just tool input. ` +
    `Then run exactly this shell command using your tool: node ${longHelper}. Wait for it to finish. Then reply with exactly ${post}. ` +
    'Do not read or change the helper or its files; the test harness releases it. Do not spawn agents.';
  assert.ok(legacy.length > MAX_PROMPT_CHARS, 'control: legacy must exceed the ceiling');
  assert.throws(() => assertPromptShape('PRE', legacy), /T831 PRE: .*exceeds/);
});

test('T831 rejects multi-line prompts', () => {
  assert.throws(
    () => assertPromptShape('PRE', `Please do this now:\nwrite ${pre}`),
    /multi-line/,
  );
});

test('T831 paste-decline classifier matches the 2026-09-22 specimens', () => {
  const specimens = [
    'Your message contained only pasted text, with nothing from you asking me to act on it, so I have not run anything.',
    'Your message contained only pasted text, with no request of your own alongside it, so I have not acted on it.',
    'You pasted a set of instructions with no message of your own, so I have not acted on them.',
    "Your message contained only pasted text and nothing typed by you, so I haven't acted on it.",
    'Your message contained only pasted text, with nothing written by you asking me to act on it, so I have not run anything.',
    "Your message contained only pasted text, with nothing from you saying what to do with it, so I haven't acted on it.",
  ];
  for (const s of specimens) assert.equal(isPasteDecline(s), true, s);
  assert.equal(isPasteDecline(`PRE-${pre}`), false);
  assert.equal(isPasteDecline('I will run the helper now.'), false);
  assert.equal(isPasteDecline(''), false);
});
