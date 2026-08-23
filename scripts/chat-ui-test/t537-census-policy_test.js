// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');

const census = fs.readFileSync(path.join(__dirname, 't537-dom-census.js'), 'utf8');
const shape = fs.readFileSync(path.join(__dirname, 'plan_ticker_shape.js'), 'utf8');

const selectors = census.match(/const SELECTORS = \[([\s\S]*?)\];/);
assert.ok(selectors, 'SELECTORS list missing');
assert.ok(
  !/agent-inspect/.test(selectors[1]),
  'sidebar transcript selectors must not be in the chrome census',
);

const named = census.match(/const NAMED = \{([\s\S]*?)\n\};/);
assert.ok(named, 'NAMED exception map missing');
const namedKeys = named[1].match(/^\s*'[^']+'/gm) || [];
assert.ok(
  namedKeys.length > 0 && namedKeys.length <= 8,
  'named exceptions must stay a short explicit list, got ' + namedKeys.length,
);

assert.ok(/const SKIP_TREE = new Set\(\['#rhs-bottom'/.test(census), 'SKIP_TREE must ignore #rhs-bottom kids');
const skipOnly = census.match(/const SKIP = new Set\(\[([\s\S]*?)\]\)/);
assert.ok(skipOnly, 'SKIP set missing');
assert.ok(!/^\s*'\.msg'\s*,/m.test(skipOnly[1]), 'transcript .msg CSS must be in the compare');
assert.ok(census.includes('boxShadow'), 'census must compare box-shadow');
assert.ok(census.includes('borderRadius'), 'census must compare border-radius');
assert.ok(census.includes('::placeholder'), 'census must observe ::placeholder');
assert.ok(census.includes('transform'), 'census must compare transform (glyph-phase forks hide here)');
assert.ok(census.includes('webkitFontSmoothing'), 'census must compare -webkit-font-smoothing');
assert.ok(census.includes('scrollbarGutter'), 'census must compare scrollbar-gutter');
assert.ok(census.includes('::-webkit-scrollbar'), 'census must observe scrollbar pseudos');
assert.ok(census.includes('paintsPlaceholder'), 'census must record whether #input is empty enough to paint placeholder');
assert.ok(census.includes('SKIP_RECT'), 'used boxes (journal vs fixture) skip rect, not CSS');
assert.ok(census.includes('compareTickers'), 'census must require ticker tree match');
assert.ok(
  /if \(!ticker\.ok \|\| unexplained\.length\) process\.exitCode = 1/.test(census),
  'unexplained chrome diffs or ticker fail must fail the census',
);

assert.ok(shape.includes("fillWidth: '*'"), 'ticker skeleton must wildcard fill width');
assert.ok(shape.includes("triLeft: '*'"), 'ticker skeleton must wildcard triangle left');
assert.ok(shape.includes('iconPath:'), 'ticker skeleton must pin company-mark SVG path');
assert.ok(shape.includes('iconTag:'), 'ticker skeleton must pin company-mark SVG tag');

console.log('ok - t537 census policy: named exceptions, no sidebar transcript, required ticker');
