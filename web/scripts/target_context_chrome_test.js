// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for target-ask context chrome (🎯T266) + speaker/context split (🎯T273).
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

console.log('target_context_chrome_test (🎯T266 / 🎯T273)');

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

// ── 🎯T273 hermetic 1: pure overseer / Jevons speaker → no speaker label ──
test('T273 isOmittedSpeakerIdentity: pure overseer/Jevons only (not jevons-po)', function () {
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('jevons'), true);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('Jevons'), true);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('overseer'), true);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity(''), true);
  // Owning PO is context, not bare speaker stamp — never speaker-omit.
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('jevons-po'), false);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('bullseye-po'), false);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('bullseye'), false);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('jv-t273-speaker'), false);
});

test('T273 overseer speaker alone → no speaker label', function () {
  const LT = TCC.SPEAKER_LT;
  const GT = TCC.SPEAKER_GT;
  assert.strictEqual(TCC.formatSpeakerLabel('jevons'), '');
  assert.strictEqual(TCC.formatSpeakerLabel('Jevons'), '');
  assert.strictEqual(TCC.formatSpeakerLabel('overseer'), '');
  assert.strictEqual(TCC.formatSpeakerLabel({ product: 'jevons' }), '');
  // jevons-po is a valid speaker identity (not omitted); chrome path uses context paint for jevons product.
  assert.strictEqual(TCC.formatSpeakerLabel('jevons-po'), LT + 'jevons-po' + GT);
  assert.strictEqual(
    TCC.formatSpeakerLabel({ product: 'bullseye', po: 'bullseye-po' }),
    LT + 'bullseye-po' + GT
  );
  assert.strictEqual(
    TCC.formatSpeakerLabel({ product: 'bullseye' }),
    LT + 'bullseye' + GT
  );
  const label = TCC.formatSpeakerLabel({ po: 'minicades-po', product: 'squz' });
  assert.strictEqual(label, LT + 'minicades-po' + GT);
  assert.ok(label.indexOf('\u00b7') < 0, 'no middle-dot in speaker label');
  assert.ok(label.indexOf(' · ') < 0, 'no · separator in speaker label');
});

test('T273 formatSpeakerHTML is bold-purple span only for non-overseer', function () {
  const html = TCC.formatSpeakerHTML({ po: 'bullseye-po', product: 'bullseye' });
  assert.ok(html.indexOf('ctx-speaker') >= 0, 'ctx-speaker class');
  assert.ok(html.indexOf(TCC.SPEAKER_LT + 'bullseye-po' + TCC.SPEAKER_GT) >= 0);
  assert.ok(html.indexOf('ctx-sep') < 0, 'no sep span on speaker HTML');
  assert.ok(html.indexOf('\u00b7') < 0, 'no · in speaker HTML');
  // Pure overseer product alone → no speaker HTML
  assert.strictEqual(TCC.formatSpeakerHTML({ product: 'jevons' }), '');
});

// ── 🎯T273 hermetic 2: jevons-po context still shows context chrome ──
test('T273 jevons-po context fixture still shows context chrome', function () {
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
  assert.strictEqual(model.repo, 'marcelocantos/jevons');
  assert.strictEqual(model.product, 'jevons');
  assert.strictEqual(model.po, 'jevons-po');
  // Context chrome MUST paint — never blanked because PO is jevons-po.
  assert.strictEqual(model.show, true, 'jevons-po context must show chrome');
  assert.strictEqual(model.label, 'jevons · jevons-po');
  assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0, 'ctx-repo present');
  assert.ok(model.innerHTML.indexOf('jevons-po') >= 0, 'owning PO in context');
  assert.ok(model.innerHTML.indexOf('ctx-po') >= 0, 'ctx-po span present');
  assert.ok(model.title.indexOf('T266') >= 0, 'target in title');
});

// ── 🎯T273 hermetic 3: non-overseer speaker → 〈agent〉 bold purple, no · ──
test('T273 non-overseer speaker paints 〈name〉 bold purple without ·', function () {
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
  const model = TCC.chromeModel({
    text: '🎯T57 needs-owner: accept graph expansion API?',
    agents: agents,
  });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.repo, 'marcelocantos/bullseye');
  assert.strictEqual(model.po, 'bullseye-po');
  assert.strictEqual(model.speaker, 'bullseye-po');
  const want = TCC.SPEAKER_LT + 'bullseye-po' + TCC.SPEAKER_GT;
  assert.strictEqual(model.label, want);
  assert.ok(model.innerHTML.indexOf('ctx-speaker') >= 0, 'ctx-speaker for bold purple');
  assert.ok(model.innerHTML.indexOf(want) >= 0, '〈bullseye-po〉 in HTML');
  assert.ok(model.innerHTML.indexOf('\u00b7') < 0, 'no middle-dot in speaker paint');
  assert.ok(model.innerHTML.indexOf(' · ') < 0, 'no · separator');
  assert.ok(model.innerHTML.indexOf('Jevons') < 0, 'no Jevons prefix');
  assert.ok(model.innerHTML.indexOf('ctx-sep') < 0, 'no ctx-sep on speaker paint');
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
  // resolveTargetContext label is context label (product · po), not speaker paint.
  assert.strictEqual(ctx.label, 'bullseye · bullseye-po');
});

test('ledger fallback when no engaged worker — jevons-po context still shows', function () {
  const ctx = TCC.resolveTargetContext({
    text: 'Decision packet for 🎯T262.4 — please confirm owner accept.',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    agents: [{
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    }],
  });
  assert.strictEqual(ctx.repo, 'marcelocantos/jevons');
  assert.strictEqual(ctx.show, true, 'context-paint gates on ask+repo only');
  assert.strictEqual(ctx.label, 'jevons · jevons-po');
  const model = TCC.chromeModel({
    text: 'Decision packet for 🎯T262.4 — please confirm owner accept.',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    agents: [{
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    }],
  });
  assert.strictEqual(model.show, true);
  assert.ok(model.innerHTML.indexOf('jevons-po') >= 0);
  assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0);
});

test('no chrome without repo resolution', function () {
  const ctx = TCC.resolveTargetContext({
    text: '🎯T999 needs-owner: accept?',
    agents: [],
  });
  assert.strictEqual(ctx.show, false);
});

test('force + explicit non-jevons paints 〈po〉 only', function () {
  const model = TCC.chromeModel({
    force: true,
    targetId: 'T57',
    repo: 'marcelocantos/bullseye',
    po: 'bullseye-po',
  });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.label, TCC.SPEAKER_LT + 'bullseye-po' + TCC.SPEAKER_GT);
  assert.ok(model.innerHTML.indexOf('\u00b7') < 0);
  assert.ok(model.innerHTML.indexOf('ctx-speaker') >= 0);
});

test('force + jevons-po keeps context chrome (not blanked)', function () {
  const model = TCC.chromeModel({
    force: true,
    targetId: 'T266',
    repo: 'marcelocantos/jevons',
    po: 'jevons-po',
  });
  assert.strictEqual(model.show, true, 'must not blank for jevons-po');
  assert.strictEqual(model.label, 'jevons · jevons-po');
  assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0);
  assert.ok(model.innerHTML.indexOf('jevons-po') >= 0);
  assert.ok(model.innerHTML.indexOf('ctx-po') >= 0);
});

test('formatContextHTML paints product · po including jevons-po', function () {
  const html = TCC.formatContextHTML({ product: 'jevons', po: 'jevons-po', repo: 'marcelocantos/jevons' });
  assert.ok(html.indexOf('ctx-repo') >= 0);
  assert.ok(html.indexOf('ctx-po') >= 0);
  assert.ok(html.indexOf('jevons-po') >= 0);
  assert.ok(html.indexOf('ctx-sep') >= 0);
  assert.strictEqual(
    TCC.formatChromeLabel({ product: 'jevons', po: 'jevons-po' }),
    'jevons · jevons-po'
  );
});

test('index.html wires TargetContextChrome + T273 styles', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/target_context_chrome.js') >= 0, 'script tag');
  assert.ok(html.indexOf('msg-context-tab') >= 0, 'msg-context-tab class');
  assert.ok(html.indexOf('syncTargetContextChrome') >= 0, 'sync attach');
  assert.ok(html.indexOf('TargetContextChrome') >= 0, 'global use');
  assert.ok(html.indexOf('ctx-speaker') >= 0, 'T273 speaker class in CSS');
  assert.ok(html.indexOf('ctx-repo') >= 0, 'T266 context class in CSS');
  assert.ok(/ctx-speaker[\s\S]*font-weight:\s*700/.test(html) ||
    html.indexOf('font-weight: 700') >= 0, 'bold speaker style present');
});

if (!process.exitCode) {
  console.log('all target_context_chrome_test passed');
}
