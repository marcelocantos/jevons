// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

const assert = require('assert');
const CR = require('./chat_reconnect.js');
const fs = require('fs');
const path = require('path');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL -', name, e.message);
  }
}

test('soft reconnect when everReady and msgs present', function () {
  assert.strictEqual(CR.shouldSoftReconnect({ msgCount: 3, everReady: true }), true);
});

test('hard reconnect on first paint (never ready)', function () {
  assert.strictEqual(CR.shouldSoftReconnect({ msgCount: 0, everReady: false }), false);
  assert.strictEqual(CR.shouldSoftReconnect({ msgCount: 5, everReady: false }), false);
});

test('hard when forceHard', function () {
  assert.strictEqual(CR.shouldSoftReconnect({ msgCount: 10, everReady: true, forceHard: true }), false);
});

test('suppress stream frames during soft; keep control frames', function () {
  assert.strictEqual(CR.shouldSuppressFrame(true, { type: 'user' }), true);
  assert.strictEqual(CR.shouldSuppressFrame(true, { type: 'assistant' }), true);
  assert.strictEqual(CR.shouldSuppressFrame(true, { message: { role: 'assistant' } }), true);
  assert.strictEqual(CR.shouldSuppressFrame(true, { type: 'conn' }), false);
  assert.strictEqual(CR.shouldSuppressFrame(true, { type: 'history_meta' }), false);
  assert.strictEqual(CR.shouldSuppressFrame(true, { type: 'error' }), false);
  // 🎯T209: inspect multiplex frames must not be suppressed on soft reconnect.
  assert.strictEqual(CR.shouldSuppressFrame(true, { type: 'agent_transcript' }), false);
  assert.strictEqual(CR.shouldSuppressFrame(false, { type: 'assistant' }), false);
});

test('isOverseerDownError', function () {
  assert.ok(CR.isOverseerDownError('the overseer is not running'));
  assert.ok(CR.isOverseerDownError('Grok CLI is not installed'));
  assert.ok(!CR.isOverseerDownError('network glitch'));
});

test('degradedBackoff longer than normal 50ms reconnect', function () {
  assert.ok(CR.degradedBackoffMs(0) >= 2000);
  assert.ok(CR.degradedBackoffMs(10) >= 10000);
});

test('index.html wires soft reconnect + degraded banner', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('chat_reconnect.js'), 'load chat_reconnect');
  assert.ok(html.includes('shouldSoftReconnect') || html.includes('ChatReconnect'), 'policy used');
  assert.ok(html.includes('degraded-banner') || html.includes('setDegraded'), 'degraded chrome');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS chat_reconnect_test');
