// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const TT = require('./tool_tooltip.js');
const TS = require('./tool_summary.js');

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

test('policy: min width high enough for glanceable tool lines', function () {
  assert.ok(TT.MIN_WIDTH_PX >= 280);
  assert.ok(TT.MAX_WIDTH_PX >= 640);
  assert.ok(TT.MAX_WIDTH_PX > TT.MIN_WIDTH_PX);
});

test('typical T116-length summary is single line within max tip width', function () {
  // T116 MAX_LEN is 60; strip shows "name: gist". At tip max-width (720) that
  // is always one line; even at min-width 320 most gists are one line.
  const s = 'search_tool: "tooltip wrap fix"';
  assert.ok(s.length <= 60, 'length ' + s.length);
  assert.strictEqual(TT.estimateLineCount(s, TT.MAX_WIDTH_PX), 1);
  assert.ok(TT.estimateLineCount(s, TT.MIN_WIDTH_PX) <= 2);
});

test('very long line may wrap past max width (threshold exists)', function () {
  const long = 'use_tool: ' + 'x'.repeat(200);
  assert.ok(TT.estimateLineCount(long, TT.MAX_WIDTH_PX) >= 2);
});

test('ToolSummary still value-first (not bare key dump)', function () {
  const nested = TS.summariseInput({
    tool_name: 'search_tool',
    tool_input: { query: 'tooltip wrap', limit: 5 },
  });
  assert.ok(nested.indexOf('tooltip') >= 0 || nested.indexOf('search_tool') >= 0);
  assert.ok(nested.indexOf('tool_input, tool_name') < 0);
  assert.ok(nested.indexOf('limit, query') < 0);
});

test('index.html turn-tip uses wide max-content policy', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Tip escapes narrow marker shrink-to-fit.
  assert.ok(/\.turn-tip\s*\{[^}]*width:\s*max-content/s.test(html), 'width:max-content');
  assert.ok(html.indexOf('min-width: 320px') >= 0 || html.indexOf('min-width:320px') >= 0, 'min-width 320');
  assert.ok(/max-width:\s*min\(720px/.test(html), 'max-width min(720px,…)');
  // Items must not use aggressive break-word as the only policy.
  const itemBlock = html.match(/\.turn-item\s*\{[^}]+\}/);
  assert.ok(itemBlock, 'turn-item rule');
  assert.ok(itemBlock[0].indexOf('word-break: normal') >= 0, 'word-break: normal');
  assert.ok(itemBlock[0].indexOf('word-break: break-word') < 0, 'no break-word on turn-item');
  assert.ok(itemBlock[0].indexOf('white-space: pre-wrap') >= 0, 'pre-wrap');
});

test('addTurnItem uses ToolSummary path in index.html', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ToolSummary.summariseInput') >= 0);
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall tool_tooltip tests passed');
