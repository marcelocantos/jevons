// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for closed-aside history (🎯T270).
//
//   node web/scripts/aside_history_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const AH = require('./aside_history.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('  ok -', name);
  } catch (e) {
    failed++;
    console.error('  FAIL -', name);
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 8).join('\n    ') : e);
  }
}

console.log('aside_history_test (🎯T270)');

test('normalizeKind maps side/capture/target (+ aliases)', function () {
  assert.strictEqual(AH.normalizeKind('side'), 'side');
  assert.strictEqual(AH.normalizeKind('aside'), 'side');
  assert.strictEqual(AH.normalizeKind(''), 'side');
  assert.strictEqual(AH.normalizeKind('capture'), 'capture');
  assert.strictEqual(AH.normalizeKind('target'), 'target');
  assert.strictEqual(AH.normalizeKind('file-target'), 'target');
  assert.strictEqual(AH.normalizeKind('', 'file-target'), 'target');
  assert.strictEqual(AH.normalizeKind('target-aside'), 'target');
});

test('kindFromCommand matches composer prefixes', function () {
  assert.strictEqual(AH.kindFromCommand('aside'), 'side');
  assert.strictEqual(AH.kindFromCommand('capture'), 'capture');
  assert.strictEqual(AH.kindFromCommand('target'), 'target');
  assert.strictEqual(AH.kindFromCommand('TARGET'), 'target');
});

test('kind labels delineate filing vs side-chat', function () {
  assert.strictEqual(AH.kindLabel('side'), 'Side chat');
  assert.strictEqual(AH.kindLabel('capture'), 'Capture');
  assert.strictEqual(AH.kindLabel('target'), 'Target filing');
  assert.ok(AH.isTargetFiling('target'));
  assert.ok(AH.isExplicitSideChat('side'));
  assert.ok(AH.isExplicitSideChat('capture'));
  assert.ok(!AH.isTargetFiling('side'));
});

test('recordsFromPayload + types distinguishable', function () {
  const rows = AH.recordsFromPayload({
    asides: [
      { id: 'att-a', title: 'billing', kind: 'side', closed_unix: 10 },
      { id: 'att-b', title: 'file T', kind: 'target', closed_unix: 20 },
    ],
    count: 2,
  });
  assert.strictEqual(rows.length, 2);
  assert.strictEqual(rows[0].id, 'att-b', 'newest first');
  assert.strictEqual(rows[0].kindLabel, 'Target filing');
  assert.strictEqual(rows[1].kindLabel, 'Side chat');
  assert.ok(AH.typesDistinguishable(rows[0], rows[1]));
  assert.ok(rows[0].kindClass !== rows[1].kindClass);
});

test('historyListHtml paints kind badges and empty state', function () {
  const empty = AH.historyListHtml([]);
  assert.ok(empty.indexOf('data-aside-history-empty') >= 0);
  const html = AH.historyListHtml(AH.recordsFromPayload({
    asides: [
      { id: 'att-side', title: 'nit', kind: 'side', closed_unix: 1 },
      { id: 'att-tgt', title: 'file X', kind: 'target', closed_unix: 2 },
    ],
  }));
  assert.ok(html.indexOf('data-aside-history-list') >= 0);
  assert.ok(html.indexOf('data-aside-kind="side"') >= 0);
  assert.ok(html.indexOf('data-aside-kind="target"') >= 0);
  assert.ok(html.indexOf('Side chat') >= 0);
  assert.ok(html.indexOf('Target filing') >= 0);
  assert.ok(html.indexOf('aside-hist-side') >= 0);
  assert.ok(html.indexOf('aside-hist-target') >= 0);
  assert.ok(html.indexOf('file X') >= 0);
});

test('historyPath is durable API not session-only', function () {
  assert.strictEqual(AH.historyPath(), '/api/asides/history');
  assert.strictEqual(AH.HISTORY_PATH, '/api/asides/history');
});

test('index.html wires Closed asides affordance + script', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/aside_history.js') >= 0, 'script tag');
  assert.ok(html.indexOf('aside-history') >= 0 || html.indexOf('asideHistory') >= 0,
    'history affordance id/class');
  assert.ok(html.indexOf('/api/asides/history') >= 0 ||
    html.indexOf('AsideHistory.historyPath') >= 0 ||
    html.indexOf('AsideHistory') >= 0,
    'product path or AsideHistory use');
  assert.ok(html.indexOf('🎯T270') >= 0 || html.indexOf('T270') >= 0, 'T270 marker');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('ok - aside_history_test (🎯T270)');
