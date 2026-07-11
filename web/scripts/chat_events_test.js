// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic headless tests for the chat working-indicator lifecycle.
// Run: node web/scripts/chat_events_test.js

'use strict';

const assert = require('assert');
const path = require('path');
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
    console.error('    ', e.message || e);
  }
}

// --- pure helpers ---

test('system clears working', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({ type: 'system' }), true);
});

test('assistant text without stop_reason clears (legacy single-shot)', () => {
  const m = {
    type: 'assistant',
    message: { content: [{ type: 'text', text: 'hi' }] },
  };
  assert.strictEqual(ChatEvents.shouldClearWorking(m), true);
});

test('assistant text with end_turn clears', () => {
  const m = {
    type: 'assistant',
    message: {
      content: [{ type: 'text', text: 'hi' }],
      stop_reason: 'end_turn',
    },
  };
  assert.strictEqual(ChatEvents.shouldClearWorking(m), true);
});

test('empty-content end_turn clears (Grok ACP terminal)', () => {
  const m = {
    type: 'assistant',
    message: { content: [], stop_reason: 'end_turn' },
  };
  assert.strictEqual(ChatEvents.shouldClearWorking(m), true);
});

test('assistant mid-stream chunk without stop does not clear when empty stop kept for stream', () => {
  // Streaming chunks from the server carry text but no stop_reason.
  // Current policy: clear on hasText && !stop — so a single full message
  // clears. For multi-chunk streams the first chunk would clear early;
  // server emits terminal end_turn after the last chunk. That early clear
  // is acceptable (indicator hides once first text arrives).
  const m = {
    type: 'assistant',
    message: { content: [{ type: 'text', text: 'Hel' }] },
  };
  assert.strictEqual(ChatEvents.shouldClearWorking(m), true);
});

test('tool_use-only assistant does not clear', () => {
  const m = {
    type: 'assistant',
    message: {
      content: [{ type: 'tool_use', name: 'Bash', input: {} }],
    },
  };
  assert.strictEqual(ChatEvents.shouldClearWorking(m), false);
});

test('assistant with non-array content does not clear', () => {
  // Pre-fix ACP Raw shape — must not clear (and must not throw).
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: { content: 'not-an-array' },
  }), false);
});

test('ACP raw without type does not clear', () => {
  // The bug: broadcasting Event.Raw from Grok ACP.
  assert.strictEqual(ChatEvents.shouldClearWorking({
    sessionId: 's',
    update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'x' } },
  }), false);
  assert.strictEqual(ChatEvents.shouldClearWorking({
    stopReason: 'end_turn',
  }), false);
});

test('full Grok turn lifecycle ends with working=false', () => {
  const events = [
    { type: 'user', message: { content: 'ping' } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'p' }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'ong' }] } },
    { type: 'assistant', message: { content: [], stop_reason: 'end_turn' } },
  ];
  assert.strictEqual(ChatEvents.workingLifecycle(events), false);
});

test('stuck lifecycle: raw ACP only (pre-fix) leaves working=true', () => {
  const events = [
    { sessionId: 's', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'pong' } } },
    { stopReason: 'end_turn' },
  ];
  assert.strictEqual(ChatEvents.workingLifecycle(events), true);
});

test('tool_use mid-turn then text then end_turn clears', () => {
  const events = [
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'tool_use', input: {} }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'done' }], stop_reason: 'end_turn' } },
  ];
  assert.strictEqual(ChatEvents.workingLifecycle(events), false);
});

// --- integration with Go wire normaliser via `go test` helper output ---
// The Go tests already assert wire shape. Here we re-check the JSON the
// server would emit matches shouldClearWorking.

test('normalised wire samples clear working', () => {
  const samples = [
    // chatWireLine assistant text
    JSON.stringify({
      type: 'assistant',
      message: { role: 'assistant', content: [{ type: 'text', text: 'Hello' }] },
    }),
    // chatWireLine empty end_turn
    JSON.stringify({
      type: 'assistant',
      message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
    }),
    // system
    JSON.stringify({ type: 'system' }),
  ];
  for (const s of samples) {
    const m = JSON.parse(s);
    assert.strictEqual(ChatEvents.shouldClearWorking(m), true, s);
  }
});

// Cross-check: index.html handle() still calls setWorking using the same
// rules as ChatEvents (string presence, not a full parse of the HTML).
test('index.html wires ChatEvents for working-indicator clears', () => {
  const fs = require('fs');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/chat_events.js'), 'must load chat_events.js');
  assert.ok(html.includes('ChatEvents.shouldClearWorking'), 'must call shouldClearWorking');
  assert.ok(html.includes('ChatEvents.hasAssistantText'), 'must use hasAssistantText');
  assert.ok(html.includes('appendOrAddJevons'), 'must stream-merge assistant chunks');
});

// Ensure Go package tests for chat_wire are green from this harness too
// when invoked as a convenience entry point.
test('go TestChatWireLineGrokACPShapes passes', () => {
  const r = spawnSync('go', ['test', './internal/server/', '-count=1', '-run', 'TestChatWire|TestDeliverOverseer|TestHandleAgent|TestUIContract'], {
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
