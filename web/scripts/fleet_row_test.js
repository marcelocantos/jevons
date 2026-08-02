// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for 🎯T115 fleet-row chrome (overseer path omit + aside 💡).
//
//   node web/scripts/fleet_row_test.js

'use strict';

const assert = require('assert');
const FR = require('./fleet_row.js');

function test(name, fn) {
  try {
    fn();
    console.log('  ok -', name);
  } catch (e) {
    console.error('  FAIL -', name);
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 6).join('\n    ') : e);
    process.exitCode = 1;
  }
}

console.log('fleet_row_test (🎯T115)');

test('overseer state-dir home omits path', function () {
  const row = FR.fleetRowModel({
    name: 'jevons',
    workdir: '/Users/marcelo/.jevons/jevons',
    status: 'running',
  });
  assert.strictEqual(row.omitPath, true);
  assert.strictEqual(row.dirHtml, '');
  assert.strictEqual(row.title, 'jevons');
  assert.strictEqual(row.isAside, false);
  assert.ok(FR.isStateDirOverseerHome('/Users/marcelo/.jevons/jevons', 'jevons'));
  assert.ok(FR.isStateDirOverseerHome('~/.jevons/jevons', 'jevons'));
  assert.ok(!FR.isStateDirOverseerHome('/Users/marcelo/work/github.com/org/repo', 'jevons'));
  assert.ok(!FR.isStateDirOverseerHome('/Users/marcelo/.jevons/other', 'jevons'));
});

test('aside purpose gets 💡 title and no path', function () {
  const row = FR.fleetRowModel({
    name: 'att-abc',
    description: 'billing nit',
    purpose: 'aside',
    workdir: '/Users/x/.jevons/threads/att-abc',
    status: 'running',
  });
  assert.strictEqual(row.isAside, true);
  assert.strictEqual(row.omitPath, true);
  assert.strictEqual(row.dirHtml, '');
  assert.strictEqual(row.title, '💡 billing nit');
  assert.ok(row.dirHtml.indexOf('billing') === -1);
  assert.ok(row.dirHtml.indexOf('.jevons') === -1);
});

test('asideTitle is idempotent and defaults', function () {
  assert.strictEqual(FR.asideTitle('ship checklist'), '💡 ship checklist');
  assert.strictEqual(FR.asideTitle('💡 already'), '💡 already');
  assert.strictEqual(FR.asideTitle(''), '💡 aside');
  assert.ok(FR.isAsidePurpose('file-target'));
  assert.ok(FR.isAsidePurpose('side-chat'));
  assert.ok(!FR.isAsidePurpose('work'));
});

test('work agents keep path / GitHub chrome (T84)', function () {
  const work = FR.fleetRowModel({
    name: 'jv-t115',
    workdir: '/Users/x/work/github.com/org/repo',
    parent: 'jevons-po',
    status: 'running',
  });
  assert.strictEqual(work.omitPath, false);
  assert.ok(/<svg/.test(work.dirHtml));
  assert.ok(work.dirHtml.indexOf('org/repo') !== -1);
  assert.strictEqual(work.title, 'jv-t115');
  assert.strictEqual(work.isAside, false);

  const plain = FR.formatAgentDir('/tmp/other');
  assert.ok(!/<svg/i.test(plain));
  assert.ok(plain.indexOf('/tmp/other') !== -1 || plain.indexOf('tmp/other') !== -1);
});

test('role=aside also treated as aside; empty agent omits path', function () {
  const byRole = FR.fleetRowModel({ name: 'side-1', role: 'aside', workdir: '/tmp/x' });
  assert.strictEqual(byRole.isAside, true);
  assert.strictEqual(byRole.omitPath, true);
  assert.strictEqual(FR.shouldOmitPath(null), true);
});

if (process.exitCode) {
  console.error('fleet_row_test: FAILED');
  process.exit(1);
}
console.log('ok - fleet_row_test (🎯T115)');
