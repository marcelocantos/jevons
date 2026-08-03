// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for mid-turn fleet rollup in working progress (🎯T202).
// Run: node web/scripts/working_progress_test.js

'use strict';

const assert = require('assert');
const WP = require('./working_progress.js');

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

// ── format ───────────────────────────────────────────────────────

test('formatFleetRollup shapes product chrome string', function () {
  assert.strictEqual(
    WP.formatFleetRollup(16, 0),
    'fleet 16 running · 0 stopped'
  );
  assert.strictEqual(
    WP.formatFleetRollup(3, 2),
    'fleet 3 running · 2 stopped'
  );
});

// ── strip ────────────────────────────────────────────────────────

test('stripFleetRollup removes single fleet segment', function () {
  assert.strictEqual(
    WP.stripFleetRollup('3 steps · use_tool:foo · fleet 16 running · 0 stopped'),
    '3 steps · use_tool:foo'
  );
});

test('stripFleetRollup removes accumulated fleet segments', function () {
  const thrash =
    '3 steps · use_tool:foo · fleet 16 running · 0 stopped · fleet 16 running · 0 stopped · fleet 15 running · 1 stopped';
  assert.strictEqual(WP.stripFleetRollup(thrash), '3 steps · use_tool:foo');
  assert.strictEqual(WP.countFleetSegments(WP.stripFleetRollup(thrash)), 0);
});

test('stripFleetRollup fleet-only becomes empty', function () {
  assert.strictEqual(WP.stripFleetRollup('fleet 16 running · 0 stopped'), '');
  assert.strictEqual(
    WP.stripFleetRollup('fleet 16 running · 0 stopped · fleet 15 running · 1 stopped'),
    ''
  );
});

// ── merge: acceptance oracle ─────────────────────────────────────

test('T202: two fleet updates → progress string contains one fleet rollup segment', function () {
  let progress = '3 steps · use_tool:bullseye__bullseye_query';
  progress = WP.mergeFleetProgress(progress, 16, 0);
  progress = WP.mergeFleetProgress(progress, 16, 0);
  assert.strictEqual(WP.countFleetSegments(progress), 1, 'got: ' + progress);
  assert.strictEqual(
    progress,
    '3 steps · use_tool:bullseye__bullseye_query · fleet 16 running · 0 stopped'
  );
});

test('T202: two fleet updates with different counts still one segment', function () {
  let progress = '2 steps · use_tool:x';
  progress = WP.mergeFleetProgress(progress, 16, 0);
  progress = WP.mergeFleetProgress(progress, 15, 1);
  assert.strictEqual(WP.countFleetSegments(progress), 1, 'got: ' + progress);
  assert.ok(progress.includes('fleet 15 running · 1 stopped'), 'got: ' + progress);
  assert.ok(!progress.includes('fleet 16'), 'old count must not remain: ' + progress);
});

test('T202: many successive agents_changed thrash still one segment', function () {
  let progress = '5 steps · use_tool:spawn';
  for (let i = 0; i < 20; i++) {
    progress = WP.mergeFleetProgress(progress, 10 + (i % 3), i % 2);
  }
  assert.strictEqual(WP.countFleetSegments(progress), 1, 'got: ' + progress);
  assert.ok(progress.startsWith('5 steps · use_tool:spawn · fleet '), 'got: ' + progress);
});

test('merge without tool base is fleet-only once', function () {
  let progress = '';
  progress = WP.mergeFleetProgress(progress, 2, 1);
  progress = WP.mergeFleetProgress(progress, 3, 0);
  assert.strictEqual(progress, 'fleet 3 running · 0 stopped');
  assert.strictEqual(WP.countFleetSegments(progress), 1);
});

test('merge recovers from already-accumulated thrash string', function () {
  const thrash =
    '1 step · use_tool:a · fleet 16 running · 0 stopped · fleet 16 running · 0 stopped';
  const fixed = WP.mergeFleetProgress(thrash, 14, 2);
  assert.strictEqual(WP.countFleetSegments(fixed), 1);
  assert.strictEqual(fixed, '1 step · use_tool:a · fleet 14 running · 2 stopped');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall ok');
