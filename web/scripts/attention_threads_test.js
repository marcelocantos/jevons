// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for 🎯T65 attention-thread model (prefix-first).
// Run: node web/scripts/attention_threads_test.js

'use strict';

const assert = require('assert');
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

test('parsePrefix is case-insensitive and strips prefix', function () {
  const a = AT.parsePrefix('aside: hello world');
  assert.strictEqual(a.command, 'aside');
  assert.strictEqual(a.body, 'hello world');
  const b = AT.parsePrefix('  CAPTURE : side thought');
  assert.strictEqual(b.command, 'capture');
  assert.strictEqual(b.body, 'side thought');
  const c = AT.parsePrefix('no prefix here');
  assert.strictEqual(c.command, null);
  assert.strictEqual(c.body, 'no prefix here');
});

test('aside: sends routed wire text and keeps main focus', function () {
  const r = AT.handleComposer(AT.emptyState(), 'aside: billing nit');
  assert.strictEqual(r.kind, 'send');
  assert.ok(r.text.indexOf('[attention:') === 0);
  assert.ok(r.text.indexOf('billing nit') > 0);
  assert.ok(r.text.indexOf('aside:') === -1);
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads.length, 1);
  assert.strictEqual(r.state.threads[0].body, 'billing nit');
});

test('capture: is local — no send, main focus, thread tracked', function () {
  const r = AT.handleComposer(AT.emptyState(), 'capture: later idea');
  assert.strictEqual(r.kind, 'local');
  assert.strictEqual(r.text, '');
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads.length, 1);
  assert.strictEqual(r.state.threads[0].body, 'later idea');
  assert.strictEqual(r.clearComposer, true);
});

test('park: and pursue: keep both threads tracked', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: First').state;
  s = AT.handleComposer(s, 'capture: Second').state;
  assert.strictEqual(s.threads.length, 2);
  const firstId = s.threads.find(function (t) { return t.body === 'First'; }).id;
  s = AT.pursue(s, firstId);
  assert.strictEqual(s.focusId, firstId);
  s = AT.handleComposer(s, 'park:').state;
  assert.strictEqual(s.focusId, AT.MAIN_ID);
  const first = s.threads.find(function (t) { return t.id === firstId; });
  assert.strictEqual(first.status, 'parked');
  s = AT.handleComposer(s, 'pursue: First').state;
  assert.strictEqual(s.focusId, firstId);
  assert.strictEqual(AT.findThread(s, firstId).status, 'open');
  assert.strictEqual(s.threads.length, 2);
});

test('main: returns focus; main: body sends on main', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Side').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const local = AT.handleComposer(s, 'main:');
  assert.strictEqual(local.kind, 'local');
  assert.strictEqual(local.state.focusId, AT.MAIN_ID);
  s = AT.pursue(local.state, id);
  const send = AT.handleComposer(s, 'main: back to work');
  assert.strictEqual(send.kind, 'send');
  assert.strictEqual(send.text, 'back to work');
  assert.strictEqual(send.state.focusId, AT.MAIN_ID);
  assert.ok(!send.routed);
});

test('pursued focus without prefix routes as aside wire', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Draft').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const r = AT.handleComposer(s, 'Pursued body');
  assert.strictEqual(r.kind, 'send');
  assert.ok(r.routed);
  assert.ok(r.text.indexOf('[attention:' + id + '|') === 0);
  assert.ok(r.text.endsWith('Pursued body'));
});

test('plain main send is unchanged', function () {
  const r = AT.handleComposer(AT.emptyState(), 'Hello main');
  assert.strictEqual(r.kind, 'send');
  assert.strictEqual(r.text, 'Hello main');
  assert.ok(!r.routed);
  assert.strictEqual(r.state.threads.length, 0);
});

test('serialize round-trip drops legacy asideNext', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Persist me').state;
  const again = AT.deserialize(AT.serialize(s));
  assert.strictEqual(again.threads.length, 1);
  assert.strictEqual(again.threads[0].body, 'Persist me');
  assert.strictEqual(again.asideNext, undefined);
});

test('load/save via mock storage', function () {
  const store = {};
  const storage = {
    getItem: function (k) { return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null; },
    setItem: function (k, v) { store[k] = String(v); },
  };
  let s = AT.handleComposer(AT.emptyState(), 'capture: Stored').state;
  AT.save(storage, s);
  const loaded = AT.load(storage);
  assert.strictEqual(loaded.threads[0].body, 'Stored');
});

test('composerPlaceholder: main is clean; side uses [aside: title] hint', function () {
  const mainPh = 'Write a message to Jevons. Enter to send, Shift-Enter for a new line.';
  assert.strictEqual(AT.composerPlaceholder(AT.emptyState(), mainPh), mainPh);
  assert.ok(AT.composerPlaceholder(AT.emptyState(), mainPh).indexOf('[main') === -1);

  let s = AT.handleComposer(AT.emptyState(), 'capture: billing nit later').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const ph = AT.composerPlaceholder(s, mainPh);
  assert.ok(ph.indexOf('[aside: ') === 0);
  assert.ok(ph.indexOf('] Write a message to Jevons') > 0);
  assert.ok(ph.indexOf('billing') > 0);
  assert.ok(ph.indexOf('[main') === -1);
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall passed');
