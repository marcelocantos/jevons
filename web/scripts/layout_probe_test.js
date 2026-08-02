// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for layout probe pure helpers (🎯T110.1 / T70.1).
// Run: node web/scripts/layout_probe_test.js

'use strict';

const assert = require('assert');
const LayoutProbe = require('./layout_probe.js');

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

test('visibleRatio full', () => {
  assert.strictEqual(LayoutProbe.visibleRatio(10, 50, 0, 100), 1);
});

test('visibleRatio half', () => {
  const r = LayoutProbe.visibleRatio(75, 50, 0, 100);
  assert.ok(r > 0.49 && r < 0.51, 'ratio=' + r);
});

test('visibleRatio none', () => {
  assert.strictEqual(LayoutProbe.visibleRatio(200, 50, 0, 100), 0);
});

test('nearBottom at end', () => {
  assert.strictEqual(LayoutProbe.nearBottom(900, 1000, 100, 5), true);
});

test('nearBottom at top', () => {
  assert.strictEqual(LayoutProbe.nearBottom(0, 1000, 100, 5), false);
});

test('nearBottom short content', () => {
  assert.strictEqual(LayoutProbe.nearBottom(0, 50, 100, 5), true);
});

test('growthWithoutCover policy pass at 28vh', () => {
  const g = LayoutProbe.growthWithoutCover(800, 28, 500, 120, 0.15);
  assert.strictEqual(g.ok, true);
  assert.ok(g.ratio >= 0.15, 'ratio=' + g.ratio);
});

test('growthWithoutCover rejects tall composer cap', () => {
  const g = LayoutProbe.growthWithoutCover(800, 50, 500, 120, 0.15);
  assert.strictEqual(g.ok, false);
});

test('COMPOSER_MAX_VH matches product CSS (28)', () => {
  assert.strictEqual(LayoutProbe.COMPOSER_MAX_VH, 28);
});

test('snapshotFromBoxes shape', () => {
  const s = LayoutProbe.snapshotFromBoxes({
    composerHeight: 120,
    messagesViewportHeight: 400,
    messagesScrollTop: 200,
    messagesScrollHeight: 800,
    lastReplyTopInMessages: 700,
    lastReplyHeight: 80,
    viewportHeight: 600,
    composerMaxVh: 28,
    nearBottomPx: 48,
  });
  assert.strictEqual(s.composerHeight, 120);
  assert.strictEqual(s.messagesViewportHeight, 400);
  assert.strictEqual(typeof s.lastReplyVisibleRatio, 'number');
  assert.strictEqual(typeof s.nearBottom, 'boolean');
  assert.strictEqual(s.composerMaxVh, 28);
  assert.strictEqual(typeof s.growthWithoutCoverOk, 'boolean');
});

test('bind installs JevonsProbe.snapshot', () => {
  const root = {};
  const probe = LayoutProbe.bind(root, null, null);
  assert.ok(probe);
  assert.strictEqual(root.JevonsProbe, probe);
  const snap = root.JevonsProbe.snapshot();
  assert.ok('composerHeight' in snap);
  assert.ok('messagesViewportHeight' in snap);
  assert.ok('lastReplyVisibleRatio' in snap);
  assert.ok('nearBottom' in snap);
});

test('index.html wires layout_probe + JevonsProbe.bind', () => {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/layout_probe.js'), 'must load layout_probe.js');
  assert.ok(html.includes('LayoutProbe.bind'), 'must bind JevonsProbe');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('all layout_probe tests passed');
