// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T831: J31's owner/PRE prompts must read as a normal short user request.
// Claude Code collapses long composer payloads into a "[Pasted text #N]" chip
// (measured boundary ~355–460 chars on the CLI path). A pure paste chip with
// no typed text makes the model answer "Your message contained only pasted
// text…" / "You pasted a set of instructions with no message of your own"
// and refuse to act — which says nothing about the product under test.
//
// Keep every journey prompt under the soft ceiling, single-line, imperative
// first-person request shape. The helper path is unavoidable (absolute node
// target) but the surrounding prose must stay short.

'use strict';

/** Soft ceiling under Claude Code's paste-chip threshold (claudia T30). */
const MAX_PROMPT_CHARS = 320;

/**
 * Detect a provider decline that is the paste-chip refusal, not a product
 * ordering failure. Specimens from 2026-09-22 J31 REDs (gates 1b60b77b,
 * 26cf6d7a, 5d6bbc4b, 1eb00caf, …).
 */
function isPasteDecline(text) {
  if (typeof text !== 'string' || !text) return false;
  const t = text.toLowerCase();
  return (
    t.includes('only pasted text') ||
    t.includes('pasted a set of instructions') ||
    t.includes('nothing from you asking me to act') ||
    t.includes('nothing typed by you') ||
    t.includes('no message of your own') ||
    t.includes('nothing written by you asking me to act') ||
    t.includes('nothing from you saying what to do')
  );
}

/**
 * Primary PRE+tool+POST ask. Short imperative; one line; under MAX_PROMPT_CHARS
 * for any realistic absolute helper path.
 */
function prePrompt({ pre, post, helper }) {
  assertToken(pre, 'pre');
  assertToken(post, 'post');
  assertToken(helper, 'helper');
  // Lead with a first-person ask so a paste-chip path still has "typed" intent
  // in the text itself if attribution is lost (belt + suspenders for T831).
  return `Please do this now: write ${pre}, then run node ${helper}, then reply ${post}.`;
}

/**
 * Re-ask after a skipped PRE. Same shape, still short.
 */
function reaskPrompt({ pre, post, helper }) {
  assertToken(pre, 'pre');
  assertToken(post, 'post');
  assertToken(helper, 'helper');
  return `Do this now: write ${pre}, then run node ${helper}, then reply ${post}.`;
}

/** Owner follow-up while the tool is held. */
function ownerPrompt(ack) {
  assertToken(ack, 'ack');
  return `Reply with exactly: ${ack}`;
}

/**
 * Hermetic shape check used by the test and by the journey at submit time.
 * Throws with a T831-labelled message when a prompt would invite a paste decline.
 */
function assertPromptShape(label, text) {
  if (typeof text !== 'string' || !text.trim()) {
    throw new Error(`T831 ${label}: prompt is empty`);
  }
  if (text.includes('\n')) {
    throw new Error(`T831 ${label}: multi-line prompts collapse to paste chips (${text.length} chars)`);
  }
  if (text.length > MAX_PROMPT_CHARS) {
    throw new Error(`T831 ${label}: ${text.length} chars exceeds ${MAX_PROMPT_CHARS} (paste-chip threshold)`);
  }
  // Must look like a request, not a pure instruction dump.
  const startsAsRequest = /^(please |do this now:|reply with exactly:)/i.test(text);
  if (!startsAsRequest) {
    throw new Error(`T831 ${label}: prompt must open as a short user request, got ${JSON.stringify(text.slice(0, 40))}`);
  }
  return text;
}

function assertToken(value, name) {
  if (typeof value !== 'string' || !value) {
    throw new Error(`T831: missing ${name}`);
  }
}

module.exports = {
  MAX_PROMPT_CHARS,
  isPasteDecline,
  prePrompt,
  reaskPrompt,
  ownerPrompt,
  assertPromptShape,
};
