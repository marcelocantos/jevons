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

test('opacity-0 / covered / off-viewport are gate failures', function () {
  // These are the serialized shapes collect() produces; the hermetic
  // suite cannot call checkVisibility, so it asserts the predicates
  // that turn those API results into a fail.
  assert.strictEqual(VC.hitTestPasses(null, 'msg', false), false, 'off-viewport centre → no hit');
  assert.strictEqual(VC.hitTestPasses('cover', 'msg', false), false, 'covered centre');
  assert.strictEqual(VC.emptyPaneFail(8, 0), true, 'all candidates opacity-0 / not intersecting');
});

console.log('viewport_census_test: ' + passed + ' passed');
