// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for 🎯T325.3 idea capture helpers + attention dual-write.
// Run: node web/scripts/idea_capture_test.js

'use strict';

const assert = require('assert');
const IC = require('./idea_capture.js');
const AT = require('./attention_threads.js');

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

test('createIdeaRequestBody shapes POST body', function () {
  const b = IC.createIdeaRequestBody('  spark text  ', 'capture', 'att-1', 'health');
  assert.strictEqual(b.text, 'spark text');
  assert.strictEqual(b.source, 'capture');
  assert.strictEqual(b.aside_id, 'att-1');
  assert.strictEqual(b.domain, 'health');
});

test('capture: dual-writes ideaCapture flags (T325.3)', function () {
  const r = AT.handleComposer(AT.emptyState(), 'capture: later cat-flap camera idea');
  assert.strictEqual(r.kind, 'local');
  assert.ok(r.threadId);
  assert.strictEqual(r.ideaCapture, true);
  assert.strictEqual(r.ideaSource, 'capture');
  assert.ok(String(r.ideaText).indexOf('cat-flap') >= 0);
  assert.ok(IC.shouldCaptureIdea(r));
  const body = IC.ideaCaptureFromComposer(r);
  assert.ok(body);
  assert.strictEqual(body.source, 'capture');
  assert.strictEqual(body.aside_id, r.threadId);
  assert.ok(body.text.indexOf('cat-flap') >= 0);
});

test('idea: durable ledger only — no threadId (T325.3)', function () {
  const r = AT.handleComposer(AT.emptyState(), 'idea: track blood pressure without evaporating');
  assert.strictEqual(r.kind, 'local');
  assert.strictEqual(r.ideaCapture, true);
  assert.strictEqual(r.ideaSource, 'idea');
  assert.ok(!r.threadId, 'idea: does not open fleet aside');
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  const body = IC.ideaCaptureFromComposer(r);
  assert.strictEqual(body.source, 'idea');
  assert.ok(!body.aside_id);
});

test('idea: empty body is empty kind', function () {
  const r = AT.handleComposer(AT.emptyState(), 'idea:   ');
  assert.strictEqual(r.kind, 'empty');
  assert.ok(!IC.shouldCaptureIdea(r));
});

test('parsePrefix recognizes idea:', function () {
  const p = AT.parsePrefix('IDEA: hello');
  assert.strictEqual(p.command, 'idea');
  assert.strictEqual(p.body, 'hello');
});

test('nextCeremony documents triage path', function () {
  assert.ok(IC.nextCeremony('file').indexOf('bullseye') >= 0);
  assert.ok(IC.nextCeremony('hold').indexOf('parked') >= 0);
  assert.ok(IC.nextCeremony('inbox').indexOf('triage') >= 0);
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All idea_capture tests passed');
