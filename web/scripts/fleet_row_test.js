// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for fleet-row chrome (🎯T115 overseer/aside + 🎯T118 progress).
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

console.log('fleet_row_test (🎯T115 + 🎯T118 + 🎯T211)');

test('overseer state-dir home omits path', function () {
  const row = FR.fleetRowModel({
    name: 'jevons',
    workdir: '/Users/marcelo/.jevons/jevons',
    status: 'running',
  });
  assert.strictEqual(row.omitPath, true);
  assert.strictEqual(row.dirHtml, '');
  assert.strictEqual(row.showPath, false);
  assert.strictEqual(row.secondaryKind, '');
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
    progress: 'working · tool',
  });
  assert.strictEqual(row.isAside, true);
  assert.strictEqual(row.omitPath, true);
  assert.strictEqual(row.dirHtml, '');
  assert.strictEqual(row.secondaryHtml, '');
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

test('work agents keep path / GitHub chrome (T84) when policy says path', function () {
  const work = FR.fleetRowModel({
    name: 'jv-t115',
    workdir: '/Users/x/work/github.com/org/repo',
    parent: 'jevons-po',
    status: 'running',
  }, { parentWorkdir: '/Users/x/work/github.com/other/repo' });
  assert.strictEqual(work.showPath, true);
  assert.strictEqual(work.secondaryKind, 'path');
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

// ── 🎯T118 ──────────────────────────────────────────────────────────

test('T118 same-workdir leaf worker prefers progress over path', function () {
  const repo = '/Users/x/work/github.com/org/repo';
  const row = FR.fleetRowModel({
    name: 'jv-t118',
    workdir: repo,
    parent: 'jevons-po',
    status: 'running',
    phase: 'working',
    step: 'Bash: go test',
    progress: 'working · Bash: go test',
  }, { parentWorkdir: repo, hasChildren: false });
  assert.strictEqual(row.showPath, false);
  assert.strictEqual(row.secondaryKind, 'progress');
  assert.ok(!/<svg/.test(row.secondaryHtml));
  assert.ok(row.secondaryHtml.indexOf('working') !== -1);
  assert.ok(row.secondaryHtml.indexOf('Bash') !== -1);
  assert.ok(row.secondaryHtml.indexOf('org/repo') === -1);
  assert.strictEqual(row.dirHtml, '');
});

test('T118 same-workdir worker without ACP falls back to idle status (T211)', function () {
  const repo = '/Users/x/work/github.com/org/repo';
  const row = FR.fleetRowModel({
    name: 'alpha-worker',
    workdir: repo,
    parent: 'po',
    status: 'running',
  }, { parentWorkdir: repo, hasChildren: false });
  assert.strictEqual(row.showPath, false);
  assert.strictEqual(row.secondaryKind, 'status');
  // 🎯T211: process-alive alone → idle, never bare "running" as busy chrome.
  assert.strictEqual(row.secondaryHtml, 'idle');
  assert.strictEqual(row.busy, false);
  assert.ok(row.secondaryHtml.indexOf('org/repo') === -1);
});

test('T118 PO with children keeps path even when workdir matches parent', function () {
  const repo = '/Users/x/work/github.com/org/po-repo';
  const row = FR.fleetRowModel({
    name: 'po',
    workdir: repo,
    parent: 'jevons',
    status: 'running',
    progress: 'working · spawn',
  }, { parentWorkdir: repo, hasChildren: true });
  assert.strictEqual(row.showPath, true);
  assert.strictEqual(row.secondaryKind, 'path');
  assert.ok(row.dirHtml.indexOf('org/po-repo') !== -1);
});

test('T118 root keeps path; distinct workdir worker keeps path', function () {
  const root = FR.fleetRowModel({
    name: 'jevons-po',
    workdir: '/Users/x/work/github.com/org/repo',
    parent: '',
    status: 'running',
  });
  assert.strictEqual(root.showPath, true);
  assert.strictEqual(root.secondaryKind, 'path');

  const other = FR.fleetRowModel({
    name: 'jv-ios',
    workdir: '/Users/x/work/github.com/org/ios',
    parent: 'po',
    status: 'running',
    progress: 'working · xcodebuild',
  }, { parentWorkdir: '/Users/x/work/github.com/org/repo', hasChildren: false });
  assert.strictEqual(other.showPath, true);
  assert.strictEqual(other.secondaryKind, 'path');
  assert.ok(other.dirHtml.indexOf('org/ios') !== -1);
});

test('T118 formatFleetProgress truncates glanceable line', function () {
  const long = 'working · ' + 'x'.repeat(80);
  const s = FR.formatFleetProgress({ progress: long });
  assert.ok(s.length <= FR.PROGRESS_MAX);
  assert.ok(/…$/.test(s) || s.length < long.length);
  assert.strictEqual(FR.formatFleetProgress({ phase: 'blocked', step: 'waiting on CI' }), 'blocked · waiting on CI');
  assert.strictEqual(FR.formatFleetProgress({ status: 'stopped' }), 'stopped');
});

test('T118 shouldShowPathSecondary policy matrix', function () {
  const a = { name: 'w', workdir: '/r', parent: 'po', status: 'running' };
  assert.strictEqual(FR.shouldShowPathSecondary(a, { parentWorkdir: '/r', hasChildren: false }), false);
  assert.strictEqual(FR.shouldShowPathSecondary(a, { parentWorkdir: '/r', hasChildren: true }), true);
  assert.strictEqual(FR.shouldShowPathSecondary(a, { parentWorkdir: '/other', hasChildren: false }), true);
  assert.strictEqual(FR.shouldShowPathSecondary({ name: 'root', workdir: '/r', parent: '' }, {}), true);
  assert.strictEqual(FR.shouldShowPathSecondary({ name: 'att', purpose: 'aside', workdir: '/r' }, {}), false);
});

// ── 🎯T211 process-alive vs turn-busy ─────────────────────────────

test('T211: status=running phase=idle is not classified or rendered busy', function () {
  const fixture = {
    name: 'jv-idle',
    workdir: '/Users/x/work/github.com/org/repo',
    parent: 'jevons-po',
    status: 'running',
    phase: 'idle',
    progress: 'running', // live API used to emit this misleadingly
  };
  assert.strictEqual(FR.isBusyAgent(fixture), false);
  assert.strictEqual(FR.formatFleetProgress(fixture), 'idle');
  assert.ok(!/running/i.test(FR.formatFleetProgress(fixture)) || FR.formatFleetProgress(fixture) === 'idle');

  const row = FR.fleetRowModel(fixture, {
    parentWorkdir: fixture.workdir,
    hasChildren: false,
  });
  assert.strictEqual(row.busy, false);
  assert.strictEqual(row.secondaryKind, 'status');
  assert.strictEqual(row.secondaryHtml, 'idle');
  assert.ok(row.secondaryHtml.indexOf('running') === -1);
  assert.ok(row.secondaryKind !== 'progress');
});

test('T211: phase=working shows action chrome and is busy', function () {
  const fixture = {
    name: 'jv-busy',
    workdir: '/Users/x/work/github.com/org/repo',
    parent: 'jevons-po',
    status: 'running',
    phase: 'working',
    step: 'use_tool:read_file',
    progress: 'working · use_tool:read_file',
  };
  assert.strictEqual(FR.isBusyAgent(fixture), true);
  assert.ok(FR.formatFleetProgress(fixture).indexOf('use_tool') !== -1);
  const row = FR.fleetRowModel(fixture, {
    parentWorkdir: fixture.workdir,
    hasChildren: false,
  });
  assert.strictEqual(row.busy, true);
  assert.strictEqual(row.secondaryKind, 'progress');
  assert.ok(row.secondaryHtml.indexOf('use_tool') !== -1 || row.progressText.indexOf('working') !== -1);
});

test('T211: bare status=running with no phase → idle not busy', function () {
  assert.strictEqual(FR.isBusyAgent({ status: 'running' }), false);
  assert.strictEqual(FR.formatFleetProgress({ status: 'running' }), 'idle');
  assert.strictEqual(FR.isBusyAgent({ status: 'running', progress: 'running' }), false);
  assert.strictEqual(FR.formatFleetProgress({ status: 'running', progress: 'running' }), 'idle');
});

if (process.exitCode) {
  console.error('fleet_row_test: FAILED');
  process.exit(1);
}
console.log('ok - fleet_row_test (🎯T115 + 🎯T118 + 🎯T211)');
