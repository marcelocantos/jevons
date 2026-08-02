// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for composer growth vs latest-reply visibility (🎯T70.1).
// Run: node web/scripts/composer_layout_test.js
// NOT Playwright — pure layout policy.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CL = require('./composer_layout.js');

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

// ── scrollTopAfterComposerGrow ──────────────────────────────────

test('no growth leaves scrollTop unchanged', function () {
  assert.strictEqual(CL.scrollTopAfterComposerGrow(100, 400, 1000, 0), 100);
  assert.strictEqual(CL.scrollTopAfterComposerGrow(100, 400, 1000, -10), 100);
});

test('growth shifts scrollTop by growPx when room allows', function () {
  // Was at bottom: scrollTop = 1000-400 = 600. Grow 80 → client 320,
  // maxScroll 680; want 680 so bottom content stays flush.
  assert.strictEqual(CL.scrollTopAfterComposerGrow(600, 320, 1000, 80), 680);
});

test('growth clamps to maxScroll', function () {
  // Already near end with little room: scrollTop 900, client after 100,
  // scrollHeight 1000 → max 900; grow 50 cannot push past max.
  assert.strictEqual(CL.scrollTopAfterComposerGrow(900, 100, 1000, 50), 900);
});

test('mid-list growth still advances scrollTop (keeps lower content)', function () {
  assert.strictEqual(CL.scrollTopAfterComposerGrow(200, 350, 1000, 50), 250);
});

// ── lastMessageFullyVisible ─────────────────────────────────────

test('last bubble fully in viewport is visible', function () {
  assert.strictEqual(CL.lastMessageFullyVisible(700, 200, 600, 400), true);
});

test('last bubble bottom past viewBot is not fully visible', function () {
  // viewBot = 600+320 = 920; lastBot = 1100 → covered by 180px
  assert.strictEqual(CL.lastMessageFullyVisible(900, 200, 600, 320), false);
});

// ── growth-without-cover oracle (acceptance scenario) ───────────

test('multi-line composer growth does not cover tall latest assistant bubble', function () {
  // Tall latest reply (280px) in a 400px transcript viewport; type enough
  // multi-line content that the composer grows 80px (messages shrink 80px).
  assert.strictEqual(CL.growthWithoutCoverHolds(280, 400, 80), true);
});

test('larger growth still keeps latest reply fully visible after adjust', function () {
  assert.strictEqual(CL.growthWithoutCoverHolds(320, 500, 120), true);
});

test('zero growth is a no-op that remains visible', function () {
  assert.strictEqual(CL.growthWithoutCoverHolds(200, 400, 0), true);
});

test('without scroll adjust, growth would cover (invariant of oracle)', function () {
  // Recompute the unfixed path to prove the bug the fix addresses.
  const lastHeight = 280;
  const clientHeight = 400;
  const growPx = 80;
  const filler = Math.max(clientHeight, 200);
  const scrollHeight = filler + lastHeight;
  const lastTop = filler;
  const scrollTop = Math.max(0, scrollHeight - clientHeight);
  const clientAfter = clientHeight - growPx;
  assert.strictEqual(
    CL.lastMessageFullyVisible(lastTop, lastHeight, scrollTop, clientAfter),
    false,
    'unfixed scroll must cover the last bubble (documents the bug)'
  );
  const fixed = CL.scrollTopAfterComposerGrow(scrollTop, clientAfter, scrollHeight, growPx);
  assert.strictEqual(
    CL.lastMessageFullyVisible(lastTop, lastHeight, fixed, clientAfter),
    true,
    'fixed scroll must keep last bubble fully visible'
  );
});

// ── index.html wiring ───────────────────────────────────────────

test('index.html wires ComposerLayout into autoGrow', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/composer_layout.js'), 'must load composer_layout.js');
  assert.ok(
    html.includes('ComposerLayout.scrollTopAfterComposerGrow'),
    'autoGrow must apply scrollTopAfterComposerGrow'
  );
  assert.ok(html.includes('const growPx = input.offsetHeight - prevH'), 'must measure growth delta');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS composer_layout_test');
