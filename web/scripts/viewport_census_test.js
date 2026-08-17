// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for viewport_census.js (🎯T493). No browser, no journal.
// Run: node web/scripts/viewport_census_test.js

'use strict';

const assert = require('assert');
const VC = require('./viewport_census.js');

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed++;
  } catch (e) {
    console.error('FAIL', name);
    console.error(e && e.stack || e);
    process.exit(1);
  }
}

test('oracle viewport is pinned 1280x800 @ dpr 1', function () {
  assert.deepStrictEqual(VC.ORACLE_VIEWPORT, { width: 1280, height: 800 });
  assert.strictEqual(VC.ORACLE_DPR, 1);
  assert.strictEqual(VC.viewportPinned(1280, 800, 1), true);
  assert.strictEqual(VC.viewportPinned(1100, 900, 1), false, 'host-sized viewport is not pinned');
  assert.strictEqual(VC.viewportPinned(1280, 800, 2), false, 'retina dpr is not pinned');
});

test('boxCentre is the geometric centre', function () {
  const c = VC.boxCentre({ left: 10, top: 20, width: 100, height: 40 });
  assert.strictEqual(c.x, 60);
  assert.strictEqual(c.y, 40);
});

test('pointInRect / rectsIntersect reject zero-size and misses', function () {
  const box = { left: 0, top: 0, right: 100, bottom: 80, width: 100, height: 80 };
  assert.strictEqual(VC.pointInRect({ x: 50, y: 40 }, box), true);
  assert.strictEqual(VC.pointInRect({ x: 100, y: 40 }, box), false);
  assert.strictEqual(VC.rectsIntersect(box, { left: 90, top: 70, right: 120, bottom: 90, width: 30, height: 20 }), true);
  assert.strictEqual(VC.rectsIntersect(box, { left: 200, top: 0, right: 240, bottom: 10, width: 40, height: 10 }), false);
  assert.strictEqual(VC.rectsIntersect(box, { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 }), false);
});

test('hitTestPasses: self or descendant; null / stranger fail', function () {
  assert.strictEqual(VC.hitTestPasses('msg', 'msg', false), true, 'self');
  assert.strictEqual(VC.hitTestPasses('span', 'msg', true), true, 'descendant');
  assert.strictEqual(VC.hitTestPasses(null, 'msg', false), false, 'no hit (off viewport / covered)');
  assert.strictEqual(VC.hitTestPasses('overlay', 'msg', false), false, 'stranger on top');
});

test('emptyPaneFail: model rows with zero visible is a fail', function () {
  assert.strictEqual(VC.emptyPaneFail(328, 0), true, 'daily empty pane');
  assert.strictEqual(VC.emptyPaneFail(16, 0), true, 'seeded isolate, nothing painted');
  assert.strictEqual(VC.emptyPaneFail(16, 3), false);
  assert.strictEqual(VC.emptyPaneFail(0, 0), false, 'empty model is not the empty-pane bug');
});

test('T494.1 Latest on hard-reload is a fail', function () {
  assert.strictEqual(VC.latestOnHardReloadFail({
    fabHidden: false, followMode: 'track', atBottom: false,
  }), true, 'owner screenshot: Latest visible after reload');
  assert.strictEqual(VC.latestOnHardReloadFail({
    fabHidden: true, followMode: 'track', atBottom: false,
  }), true, 'tracking but not at live end');
  assert.strictEqual(VC.latestOnHardReloadFail({
    fabHidden: true, followMode: 'track', atBottom: true,
  }), false);
});

test('T494.1 empty unlabelled slots are a desert', function () {
  assert.strictEqual(VC.emptySlotDesertFail(255), true, 'daily 255 empty slots');
  assert.strictEqual(VC.emptySlotDesertFail(1), true);
  assert.strictEqual(VC.emptySlotDesertFail(0), false);
});

test('T494.1 packed pane needs two bubbles on the oracle viewport', function () {
  assert.strictEqual(VC.packedPaneFail(1, 2), true, 'one leftover bubble');
  assert.strictEqual(VC.packedPaneFail(0, 2), true);
  assert.strictEqual(VC.packedPaneFail(3, 2), false);
});

test('T119.7 overlappingRectsFail is a stale-prefix overlap', function () {
  assert.strictEqual(VC.overlappingRectsFail([
    { top: 100, bottom: 220 },
    { top: 180, bottom: 280 },
  ]), true, 'owner screenshot');
  assert.strictEqual(VC.overlappingRectsFail([
    { top: 100, bottom: 180 },
    { top: 188, bottom: 260 },
  ]), false);
});

test('T494.1 maxInkGap is the void between consecutive bubbles', function () {
  // Owner screenshot: leftover turn near the top, leftover turn near
  // the bottom, hundreds of px of empty canvas between them.
  const desert = VC.maxInkGapPx([
    { top: 80, bottom: 150 },
    { top: 900, bottom: 970 },
  ]);
  assert.strictEqual(desert, 750);
  assert.strictEqual(VC.desertGapFail(desert, 1000), true);
  const packed = VC.maxInkGapPx([
    { top: 100, bottom: 180 },
    { top: 188, bottom: 260 },
    { top: 268, bottom: 340 },
  ]);
  assert.strictEqual(packed, 8);
  assert.strictEqual(VC.desertGapFail(packed, 689), false);
  assert.strictEqual(VC.maxInkGapPx([{ top: 0, bottom: 80 }]), 0, 'one bubble is packedPane, not a gap');
});

test('T494.1.2 canvas min-height ratchet is a fail', function () {
  assert.strictEqual(VC.canvasRatchetFail(17827, 17102, 120), true, 'daily after detour');
  assert.strictEqual(VC.canvasRatchetFail(17102, 17102, 120), false);
  assert.strictEqual(VC.canvasRatchetFail(0, 800, 120), false);
});

test('T494.1.2 void under the last turn is a fail', function () {
  // Owner screenshot: last bubble in the upper half, inches of canvas below.
  assert.strictEqual(VC.voidBelowLastFail(400, 1200, 120), true);
  assert.strictEqual(VC.voidBelowLastFail(16989, 17040, 120), false, 'one compact ⋯ slot');
  assert.strictEqual(VC.voidBelowLastFail(0, 800, 120), false, 'no content yet');
  assert.strictEqual(VC.VOID_BELOW_VISIBLE_PX, 120);
});

test('T494.1 pin and canvas-end must agree', function () {
  assert.strictEqual(VC.liveEndDisagreeFail(22366.6, 22585, 16), true, '218px tail');
  assert.strictEqual(VC.liveEndDisagreeFail(22585, 22585, 16), false);
  assert.strictEqual(VC.liveEndDisagreeFail(22580, 22585, 16), false, 'within Latest ε');
  // T351 over-assign: write scrollHeight, engine clamps to sh − ch.
  // Census must compare the clamped target, not the write.
  const sh = 3252, ch = 689;
  const write = sh;
  const clamped = Math.min(write, sh - ch);
  assert.strictEqual(VC.liveEndDisagreeFail(clamped, sh - ch, 16), false);
});

test('opacity-0 / covered / off-viewport are gate failures', function () {
  // These are the serialized shapes collect() produces; the hermetic
  // suite cannot call checkVisibility, so it asserts the predicates
  // that turn those API results into a fail.
  assert.strictEqual(VC.hitTestPasses(null, 'msg', false), false, 'off-viewport centre → no hit');
  assert.strictEqual(VC.hitTestPasses('cover', 'msg', false), false, 'covered centre');
  assert.strictEqual(VC.emptyPaneFail(8, 0), true, 'all candidates opacity-0 / not intersecting');
});

console.log('viewport_census_test: ' + passed + ' passed');
