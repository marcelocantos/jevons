// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const Clock = require('./clock.js');

function test(name, fn) {
  fn();
  console.log('ok', name);
}

test('now tracks wall clock when unfrozen', function () {
  Clock.reset();
  const a = Clock.now();
  const b = Date.now();
  assert.ok(Math.abs(b - a) < 50, 'unfrozen now is wall clock');
});

test('setNow freezes now and date', function () {
  Clock.setNow(1_700_000_000_000);
  assert.strictEqual(Clock.now(), 1_700_000_000_000);
  assert.strictEqual(Clock.date().getTime(), 1_700_000_000_000);
  assert.ok(Clock.isFrozen());
  Clock.reset();
  assert.ok(!Clock.isFrozen());
});

test('product JS reads wall time only through clock.js', function () {
  const root = path.join(__dirname);
  const html = path.join(__dirname, '..', 'index.html');
  const hits = [];
  const files = fs.readdirSync(root).filter((n) => n.endsWith('.js') && !n.endsWith('_test.js'));
  for (const n of files) {
    if (n === 'clock.js') continue;
    const text = fs.readFileSync(path.join(root, n), 'utf8');
    if (/\bDate\.now\s*\(/.test(text) || /\bnew Date\s*\(\s*\)/.test(text)) {
      hits.push('web/scripts/' + n);
    }
  }
  const index = fs.readFileSync(html, 'utf8');
  if (/\bDate\.now\s*\(/.test(index) || /\bnew Date\s*\(\s*\)/.test(index)) {
    hits.push('web/index.html');
  }
  assert.deepStrictEqual(hits, [], 'Date.now / new Date() only in clock.js, got ' + hits.join(', '));
});

console.log('clock_test.js ok');
