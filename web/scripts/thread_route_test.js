// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const TR = require('./thread_route.js');

function test(name, fn) {
  try {
    fn();
    console.log('ok - ' + name);
  } catch (e) {
    console.error('not ok - ' + name);
    console.error(e);
    process.exitCode = 1;
  }
}

const threads = [
  { id: 'att-restic', title: 'restic backup', digest: 'restic snapshots prune', body: '' },
  { id: 'att-billing', title: 'billing nit', digest: 'invoice stripe', body: '' },
];

test('continuation matches restic thread', function () {
  const r = TR.route("How's restic going?", threads);
  assert.strictEqual(r.threadId, 'att-restic');
  assert.strictEqual(r.reason, 'match');
});

test('unrelated stays on main', function () {
  const r = TR.route('What time is lunch?', threads);
  assert.strictEqual(r.threadId, null);
  assert.ok(r.reason === 'no-match' || r.reason === 'no-terms');
});

test('explicit prefix never auto-routes', function () {
  const r = TR.route('aside: restic status', threads);
  assert.strictEqual(r.reason, 'explicit-prefix');
  assert.strictEqual(r.threadId, null);
});

test('target: prefix never auto-routes', function () {
  const r = TR.route('target: virtualise chat', threads);
  assert.strictEqual(r.reason, 'explicit-prefix');
});

console.log(process.exitCode ? 'FAIL' : 'PASS thread_route_test');
