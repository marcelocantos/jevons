// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic headless tests for chat working-indicator + stream coalesce.
// Run: node web/scripts/chat_events_test.js
//
// Oracle for the token-per-bubble regression: a Grok turn that streams
// ["Hello", ".", "What", "do", "you", "need", "?"] must produce exactly
// ONE assistant bubble containing the full sentence.

'use strict';

const assert = require('assert');
const path = require('path');
const fs = require('fs');
const { spawnSync } = require('child_process');
const ChatEvents = require('./chat_events.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n     ') : e);
  }
}

function chunk(text) {
  return {
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text }] },
  };
}

function endTurn() {
  return {
    type: 'assistant',
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  };
}

function user(text) {
  return { type: 'user', message: { role: 'user', content: text } };
}

// ── shouldClearWorking ──────────────────────────────────────────

test('system clears working', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({ type: 'system' }), true);
});

test('mid-stream text chunk does NOT clear working', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking(chunk('Hello')), false);
  assert.strictEqual(ChatEvents.shouldClearWorking(chunk('.')), false);
});

test('assistant text with end_turn clears', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: {
      content: [{ type: 'text', text: 'hi' }],
      stop_reason: 'end_turn',
    },
  }), true);
});

test('empty-content end_turn clears (Grok ACP terminal)', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking(endTurn()), true);
});

test('tool_use-only assistant does not clear', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: { content: [{ type: 'tool_use', name: 'Bash', input: {} }] },
  }), false);
});

test('assistant with non-array content does not clear', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: { content: 'not-an-array' },
  }), false);
});

test('ACP raw without type does not clear', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    sessionId: 's',
    update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'x' } },
  }), false);
  assert.strictEqual(ChatEvents.shouldClearWorking({ stopReason: 'end_turn' }), false);
});

// ── working stays true across stream, false after end_turn ─────

test('working stays true across multi-chunk stream until end_turn', () => {
  const events = [
    chunk('Hello'),
    chunk('.'),
    chunk(' What'),
    chunk(' do'),
    chunk(' you'),
    chunk(' need'),
    chunk('?'),
  ];
  // After stream only (no end_turn) working must remain true.
  assert.strictEqual(ChatEvents.workingLifecycle(events), true);
  assert.strictEqual(ChatEvents.workingLifecycle([...events, endTurn()]), false);
});

test('stuck lifecycle: raw ACP only (pre-fix) leaves working=true', () => {
  const events = [
    { sessionId: 's', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'pong' } } },
    { stopReason: 'end_turn' },
  ];
  assert.strictEqual(ChatEvents.workingLifecycle(events), true);
});

// ── stream coalesce (the screenshot regression) ─────────────────

test('screenshot regression: token stream → one bubble, full text', () => {
  // Exact pattern from the user screenshot: one user "hello", then
  // assistant tokens each rendered as their own bubble before the fix.
  const tokens = ['Hello', '.', 'What', 'do', 'you', 'need', '?'];
  const events = [user('hello'), ...tokens.map(chunk), endTurn()];
  const state = ChatEvents.applyChatEvents(events);

  assert.strictEqual(state.working, false, 'working must clear after end_turn');
  assert.strictEqual(state.userTexts.length, 1, 'one user bubble');
  assert.strictEqual(state.userTexts[0], 'hello');
  assert.strictEqual(
    state.assistantBubbles.length,
    1,
    `expected 1 assistant bubble, got ${state.assistantBubbles.length}: ${JSON.stringify(state.assistantBubbles)}`,
  );
  assert.strictEqual(state.assistantBubbles[0], tokens.join(''));
  assert.strictEqual(state.openStream, -1, 'stream must be sealed');
});

test('two turns produce two sealed assistant bubbles', () => {
  const events = [
    user('hi'),
    chunk('Hello'),
    chunk(' there'),
    endTurn(),
    user('again'),
    chunk('Hi'),
    chunk(' again'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 2);
  assert.strictEqual(state.assistantBubbles[0], 'Hello there');
  assert.strictEqual(state.assistantBubbles[1], 'Hi again');
  assert.strictEqual(state.working, false);
});

test('user echo dedupe does not double-count', () => {
  const events = [user('hello'), user('hello'), chunk('Hi'), endTurn()];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.userTexts.length, 1);
});

test('tool_use mid-turn then text then end_turn: one bubble, cleared', () => {
  const events = [
    user('run it'),
    {
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'tool_use', input: {} }] },
    },
    chunk('done'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.assistantBubbles[0], 'done');
  assert.strictEqual(state.working, false);
});

test('text+end_turn in one event: one bubble and clear', () => {
  const events = [
    user('x'),
    {
      type: 'assistant',
      message: {
        content: [{ type: 'text', text: 'complete' }],
        stop_reason: 'end_turn',
      },
    },
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.assistantBubbles[0], 'complete');
  assert.strictEqual(state.working, false);
});

// ── wire shape from Go normaliser (static samples) ──────────────

test('normalised wire samples: mid-stream keeps working, end clears', () => {
  const mid = JSON.parse(JSON.stringify({
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text: 'Hello' }] },
  }));
  const term = JSON.parse(JSON.stringify({
    type: 'assistant',
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  }));
  assert.strictEqual(ChatEvents.shouldClearWorking(mid), false);
  assert.strictEqual(ChatEvents.shouldClearWorking(term), true);
  assert.strictEqual(ChatEvents.shouldClearWorking({ type: 'system' }), true);
});

// ── index.html wiring ───────────────────────────────────────────

test('index.html wires ChatEvents + stream seal', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/chat_events.js'), 'must load chat_events.js');
  assert.ok(html.includes('ChatEvents.shouldClearWorking'), 'must call shouldClearWorking');
  assert.ok(html.includes('ChatEvents.hasAssistantText'), 'must use hasAssistantText');
  assert.ok(html.includes('appendOrAddJevons'), 'must stream-merge assistant chunks');
  assert.ok(html.includes('sealAssistantStream'), 'must seal stream on terminal');
  assert.ok(
    html.includes('_streamRaw'),
    'merge must key on _streamRaw (not only workingEl)',
  );
});

// ── regression guard: old clear-on-first-text policy is gone ────

test('regression guard: hasText without stop must not clear', () => {
  // This was the broken policy that caused the screenshot.
  const m = chunk('Hello');
  assert.strictEqual(ChatEvents.hasAssistantText(m), true);
  assert.strictEqual(ChatEvents.stopReason(m), '');
  assert.strictEqual(ChatEvents.shouldClearWorking(m), false);
});

// ── Go package tests ────────────────────────────────────────────

test('go chat wire + roundtrip tests pass', () => {
  const r = spawnSync('go', [
    'test', './internal/server/', '-count=1',
    '-run', 'TestChat|TestDeliver|TestHandleAgent|TestUIContract|TestMultiChunk',
  ], {
    cwd: path.join(__dirname, '..', '..'),
    encoding: 'utf8',
    timeout: 120000,
  });
  if (r.status !== 0) {
    throw new Error((r.stdout || '') + (r.stderr || '') || `exit ${r.status}`);
  }
});

if (failed) {
  console.error(`\n${failed} test(s) failed`);
  process.exit(1);
}
console.log('\nall chat_events tests passed');
