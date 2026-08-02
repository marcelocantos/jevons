// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const VL = require('./virtual_list.js');

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

test('visibleIndices returns only near-viewport items', function () {
  const tops = [];
  for (let i = 0; i < 100; i++) tops.push({ top: i * 40, height: 40 });
  // Viewport mid-list
  const vis = VL.visibleIndices(tops, 2000, 400, 200);
  assert.ok(vis.length < 30, 'bounded visible set, got ' + vis.length);
  assert.ok(vis[0] > 40, 'starts mid-list');
  assert.ok(vis[vis.length - 1] < 70, 'ends mid-list');
});

test('materialisedCount grows much slower than N', function () {
  const n = 500;
  const mat = VL.materialisedCount(n, 48, 600, 800);
  assert.ok(mat < 80, 'materialised ' + mat + ' for N=' + n);
  assert.ok(mat > 5, 'some materialised');
});

test('shouldMaterialize edge cases', function () {
  assert.strictEqual(VL.shouldMaterialize(0, 100, 0, 300, 0), true);
  assert.strictEqual(VL.shouldMaterialize(5000, 100, 0, 300, 0), false);
});

console.log(process.exitCode ? 'FAIL' : 'PASS virtual_list_test');
