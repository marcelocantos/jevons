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

// 🎯T247: already-routed wire markers must not re-suggest Continue-in / create.
test('T247 target-aside and attention wire markers never auto-route', function () {
  const wireTarget = '[target-aside: att-xyz | Chat paste images work]\nChat paste images work\n\n(Ceremony: …)';
  const r1 = TR.route(wireTarget, [
    { id: 'att-xyz', title: 'Chat paste images work', body: 'Chat paste images work' },
  ]);
  assert.strictEqual(r1.reason, 'explicit-prefix');
  assert.strictEqual(r1.threadId, null);

  const wireAtt = '[attention:att-xyz|billing nit]\nbilling nit body';
  const r2 = TR.route(wireAtt, [
    { id: 'att-xyz', title: 'billing nit', body: 'billing nit body' },
  ]);
  assert.strictEqual(r2.reason, 'explicit-prefix');
  assert.strictEqual(r2.threadId, null);
});

// 🎯T134: never silent-match done/archive ghosts (even if passed raw).
test('T134 route ignores done status ghosts', function () {
  const mixed = [
    { id: 'att-ghost', title: 'restic backup', body: 'restic snapshots prune', status: 'done' },
    { id: 'att-live', title: 'billing nit', body: 'invoice stripe', status: 'open' },
  ];
  const r = TR.route("How's restic going?", mixed);
  assert.strictEqual(r.threadId, null);
  assert.ok(r.reason === 'no-match' || r.reason === 'no-terms');
});

test('T134 route ignores archived alias', function () {
  const mixed = [
    { id: 'att-ghost', title: 'restic backup', body: 'restic prune', status: 'archived' },
  ];
  const r = TR.route('restic backup status please', mixed);
  assert.strictEqual(r.threadId, null);
});

test('T134 open thread still matches when status open', function () {
  const openOnly = [
    { id: 'att-restic', title: 'restic backup', body: 'restic snapshots', status: 'open' },
  ];
  const r = TR.route("How's restic going?", openOnly);
  assert.strictEqual(r.threadId, 'att-restic');
  assert.strictEqual(r.reason, 'match');
});

test('T134 isRouteable rejects done', function () {
  assert.strictEqual(TR.isRouteable({ id: 'x', status: 'done' }), false);
  assert.strictEqual(TR.isRouteable({ id: 'x', status: 'archived' }), false);
  assert.strictEqual(TR.isRouteable({ id: 'x', status: 'open' }), true);
  assert.strictEqual(TR.isRouteable({ id: 'main' }), false);
});

console.log(process.exitCode ? 'FAIL' : 'PASS thread_route_test');
