// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for target-ask context chrome (🎯T266).
//   node web/scripts/target_context_chrome_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const TCC = require('./target_context_chrome.js');

function test(name, fn) {
  try {
    fn();
    console.log('  ok -', name);
  } catch (e) {
    console.error('  FAIL -', name);
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 8).join('\n    ') : e);
    process.exitCode = 1;
  }
}

console.log('target_context_chrome_test (🎯T266)');

test('extractTargetIDs finds 🎯 and bare T-ids', function () {
  assert.deepStrictEqual(
    TCC.extractTargetIDs('About 🎯T262.4 and T266 — also 🎯T10.2'),
    ['T262.4', 'T266', 'T10.2']
  );
  assert.deepStrictEqual(TCC.extractTargetIDs('no targets here'), []);
  assert.strictEqual(TCC.normalizeTargetID('🎯T198'), 'T198');
});

test('looksLikeTargetAsk requires target + ask/decision cues', function () {
  assert.ok(TCC.looksLikeTargetAsk('🎯T262.4 needs-owner: accept frontier-as-ready-set?'));
  assert.ok(TCC.looksLikeTargetAsk('Decision packet for 🎯T262.4 — please confirm.'));
  assert.ok(TCC.looksLikeTargetAsk('Owner decision on 🎯T266: which repo owns this chrome?'));
  assert.ok(!TCC.looksLikeTargetAsk('Worker engaged on 🎯T198 and progressing.'));
  assert.ok(!TCC.looksLikeTargetAsk('Hello owner, how are you?'));
});

test('repoLabelFromPath prefers github org/repo', function () {
  assert.strictEqual(
    TCC.repoLabelFromPath('/Users/m/work/github.com/marcelocantos/jevons'),
    'marcelocantos/jevons'
  );
  assert.strictEqual(
    TCC.repoLabelFromPath('/Users/m/work/github.com/marcelocantos/bullseye/bullseye.yaml'),
    'marcelocantos/bullseye'
  );
  assert.strictEqual(TCC.productFromRepoLabel('marcelocantos/jevons'), 'jevons');
});

test('fixture target ask paints repo + PO chrome model', function () {
  const agents = [
    { name: 'jevons', purpose: 'overseer', workdir: '/Users/m/.jevons/jevons' },
    {
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
      parent: 'jevons',
    },
    {
      name: 'jv-t266-target-context-chrome',
      purpose: 'work',
      parent: 'jevons-po',
      target_id: 'T266',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    },
  ];
  const text =
    '🎯T266 needs-owner residual? Owner-facing target asks should show ' +
    'built-in context chrome (repo / PO). Accept this design?';
  const model = TCC.chromeModel({ text: text, agents: agents });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.targetId, 'T266');
  assert.strictEqual(model.repo, 'marcelocantos/jevons');
  assert.strictEqual(model.product, 'jevons');
  assert.strictEqual(model.po, 'jevons-po');
  assert.strictEqual(model.label, 'jevons · jevons-po');
  assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0);
  assert.ok(model.innerHTML.indexOf('jevons-po') >= 0);
  assert.ok(model.title.indexOf('T266') >= 0);
});

test('disambiguates jevons vs bullseye via engaged target_id workdir', function () {
  const agents = [
    {
      name: 'bullseye-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/bullseye',
      parent: 'jevons',
    },
    {
      name: 'jv-t57-graph',
      purpose: 'work',
      parent: 'bullseye-po',
      target_id: 'T57',
      workdir: '/Users/m/work/github.com/marcelocantos/bullseye',
    },
    {
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
      parent: 'jevons',
    },
  ];
  const ctx = TCC.resolveTargetContext({
    text: '🎯T57 needs-owner: accept graph expansion API?',
    agents: agents,
  });
  assert.strictEqual(ctx.show, true);
  assert.strictEqual(ctx.repo, 'marcelocantos/bullseye');
  assert.strictEqual(ctx.po, 'bullseye-po');
  assert.strictEqual(ctx.label, 'bullseye · bullseye-po');
});

test('ledger fallback when no engaged worker', function () {
  const ctx = TCC.resolveTargetContext({
    text: 'Decision packet for 🎯T262.4 — please confirm owner accept.',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    agents: [{
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    }],
  });
  assert.strictEqual(ctx.show, true);
  assert.strictEqual(ctx.repo, 'marcelocantos/jevons');
});

test('no chrome without repo resolution', function () {
  const ctx = TCC.resolveTargetContext({
    text: '🎯T999 needs-owner: accept?',
    agents: [],
  });
  assert.strictEqual(ctx.show, false);
});

test('force + explicit repo paints without ask cues', function () {
  const model = TCC.chromeModel({
    force: true,
    targetId: 'T266',
    repo: 'marcelocantos/jevons',
    po: 'jevons-po',
  });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.label, 'jevons · jevons-po');
});

test('index.html wires TargetContextChrome script + attach', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/target_context_chrome.js') >= 0, 'script tag');
  assert.ok(html.indexOf('msg-context-tab') >= 0, 'msg-context-tab class');
  assert.ok(html.indexOf('syncTargetContextChrome') >= 0, 'sync attach');
  assert.ok(html.indexOf('TargetContextChrome') >= 0, 'global use');
});

if (!process.exitCode) {
  console.log('all target_context_chrome_test passed');
}
