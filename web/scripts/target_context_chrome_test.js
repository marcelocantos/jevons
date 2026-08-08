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

// ── 🎯T273 hermetic: pure overseer speaker must not stamp in painted DOM ──
// Fails on b81267d residual: product leaf "jevons" painted as ctx-repo head.
test('T273 pure overseer message: no speaker token; context heading kept', function () {
  const LT = TCC.SPEAKER_LT;
  const GT = TCC.SPEAKER_GT;
  // (a) Pure overseer product + PO context — no 〈jevons〉 / bare "jevons" stamp.
  const withPo = TCC.chromeModel({
    force: true,
    targetId: 'T273',
    repo: 'marcelocantos/jevons',
    product: 'jevons',
    po: 'jevons-po',
  });
  assert.strictEqual(withPo.show, true, 'context heading must stay');
  assert.ok(withPo.innerHTML.indexOf('ctx-speaker') < 0, 'no ctx-speaker for pure overseer');
  assert.ok(withPo.innerHTML.indexOf(LT + 'jevons' + GT) < 0, 'no 〈jevons〉 speaker token');
  assert.ok(withPo.innerHTML.indexOf(LT + 'overseer' + GT) < 0, 'no 〈overseer〉');
  assert.notStrictEqual(withPo.label, 'jevons', 'must not paint pure product leaf as heading');
  assert.notStrictEqual(withPo.label, LT + 'jevons' + GT, 'must not paint 〈jevons〉 label');
  assert.ok(withPo.innerHTML.indexOf('jevons-po') >= 0, 'owning PO context kept');
  assert.ok(withPo.innerHTML.indexOf('ctx-repo') >= 0, 'context chrome kept');
  assert.ok(withPo.innerHTML.indexOf('ctx-po') >= 0, 'PO span kept');
  assert.strictEqual(withPo.label, 'marcelocantos/jevons · jevons-po');
  // Product-only (no PO): still no bare "jevons" speaker stamp; full repo context.
  const alone = TCC.chromeModel({
    force: true,
    repo: 'marcelocantos/jevons',
    product: 'jevons',
  });
  assert.strictEqual(alone.show, true, 'repo context must still paint');
  assert.strictEqual(alone.label, 'marcelocantos/jevons');
  assert.ok(alone.innerHTML.indexOf('ctx-speaker') < 0);
  assert.ok(alone.innerHTML.indexOf(LT + 'jevons' + GT) < 0);
  assert.notStrictEqual(alone.label, 'jevons');
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
  // Repo head (not bare product "jevons") · owning PO.
  assert.strictEqual(model.label, 'marcelocantos/jevons · jevons-po');
  assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0, 'ctx-repo present');
  assert.ok(model.innerHTML.indexOf('jevons-po') >= 0, 'owning PO in context');
  assert.ok(model.innerHTML.indexOf('ctx-po') >= 0, 'ctx-po span present');
  assert.ok(model.innerHTML.indexOf('ctx-speaker') < 0, 'no speaker token for overseer product');
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
  assert.strictEqual(ctx.label, 'marcelocantos/jevons · jevons-po');
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
  assert.ok(model.innerHTML.indexOf('ctx-speaker') < 0);
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
  assert.strictEqual(model.label, 'marcelocantos/jevons · jevons-po');
  assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0);
  assert.ok(model.innerHTML.indexOf('jevons-po') >= 0);
  assert.ok(model.innerHTML.indexOf('ctx-po') >= 0);
  assert.ok(model.innerHTML.indexOf('ctx-speaker') < 0);
});

test('formatContextHTML prefers repo head when product is pure overseer', function () {
  const html = TCC.formatContextHTML({ product: 'jevons', po: 'jevons-po', repo: 'marcelocantos/jevons' });
  assert.ok(html.indexOf('ctx-repo') >= 0);
  assert.ok(html.indexOf('ctx-po') >= 0);
  assert.ok(html.indexOf('jevons-po') >= 0);
  assert.ok(html.indexOf('ctx-sep') >= 0);
  assert.ok(html.indexOf('marcelocantos/jevons') >= 0, 'repo head not bare jevons');
  // Bare product leaf alone is speaker-stamp residual — not the painted head.
  assert.ok(!/^<span class="ctx-repo">jevons<\/span>/.test(html));
  assert.strictEqual(
    TCC.formatChromeLabel({ product: 'jevons', po: 'jevons-po', repo: 'marcelocantos/jevons' }),
    'marcelocantos/jevons · jevons-po'
  );
  // Non-overseer product still uses product leaf as head.
  assert.strictEqual(
    TCC.formatChromeLabel({ product: 'bullseye', po: 'bullseye-po', repo: 'marcelocantos/bullseye' }),
    'bullseye · bullseye-po'
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

// ── 🎯T306: context chrome is PROVENANCE — owner/user bubbles never get it ──

test('T306 isOwnerRole classifies owner-authored roles', function () {
  assert.strictEqual(TCC.isOwnerRole('user'), true);
  assert.strictEqual(TCC.isOwnerRole('User'), true);
  assert.strictEqual(TCC.isOwnerRole(' owner '), true);
  assert.strictEqual(TCC.isOwnerRole('me'), true);
  assert.strictEqual(TCC.isOwnerRole('jevons'), false);
  assert.strictEqual(TCC.isOwnerRole('assistant'), false);
  assert.strictEqual(TCC.isOwnerRole('system'), false);
  assert.strictEqual(TCC.isOwnerRole(''), false);
  assert.strictEqual(TCC.isOwnerRole(null), false);
});

test('T306 owner line with a 🎯 id + ambient ledger gets NO chrome', function () {
  const agents = [{
    name: 'jevons-po',
    purpose: 'work',
    workdir: '/Users/m/work/github.com/marcelocantos/jevons',
  }];
  const opts = {
    text: 'T302 is back to life now',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    cwd: '/Users/m/work/github.com/marcelocantos/jevons',
    agents: agents,
  };
  // Pre-fix: ask-fallback (primary id + ambient ledger/cwd) made this paint.
  const asOwner = TCC.resolveTargetContext(Object.assign({ role: 'user' }, opts));
  assert.strictEqual(asOwner.show, false, 'owner bubble must never show chrome');
  const ownerModel = TCC.chromeModel(Object.assign({ role: 'user' }, opts));
  assert.strictEqual(ownerModel.show, false);
  assert.strictEqual(ownerModel.innerHTML, '', 'no painted chrome for owner');
  assert.strictEqual(ownerModel.label, '');
});

test('T306 owner role beats force and explicit repo/po', function () {
  const model = TCC.chromeModel({
    role: 'user',
    force: true,
    targetId: 'T306',
    repo: 'marcelocantos/jevons',
    product: 'jevons',
    po: 'jevons-po',
  });
  assert.strictEqual(model.show, false, 'force must not override the role gate');
  assert.strictEqual(model.innerHTML, '');
  assert.ok(model.label.indexOf('jevons-po') < 0, 'no PO tab on owner bubble');
});

test('T306 owner ask cues still get no chrome', function () {
  const agents = [{
    name: 'bullseye-po',
    purpose: 'work',
    workdir: '/Users/m/work/github.com/marcelocantos/bullseye',
  }];
  const model = TCC.chromeModel({
    role: 'owner',
    text: '🎯T57 needs-owner: do you want the graph expansion API?',
    ledger: '/Users/m/work/github.com/marcelocantos/bullseye/bullseye.yaml',
    agents: agents,
  });
  assert.strictEqual(model.show, false, 'ask cues do not resurrect owner chrome');
  assert.strictEqual(model.innerHTML, '');
});

test('T306 assistant/jevons bubbles keep provenance chrome', function () {
  const agents = [{
    name: 'jevons-po',
    purpose: 'work',
    workdir: '/Users/m/work/github.com/marcelocantos/jevons',
  }];
  const base = {
    text: 'Decision packet for 🎯T262.4 — please confirm owner accept.',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    agents: agents,
  };
  ['jevons', 'assistant', 'system', ''].forEach(function (role) {
    const model = TCC.chromeModel(Object.assign({ role: role }, base));
    assert.strictEqual(model.show, true, 'chrome kept for role "' + role + '"');
    assert.strictEqual(model.label, 'marcelocantos/jevons · jevons-po');
    assert.ok(model.innerHTML.indexOf('ctx-repo') >= 0, 'ctx-repo for role ' + role);
  });
  // Undeclared role (legacy callers) behaves as before the role gate.
  const legacy = TCC.chromeModel(base);
  assert.strictEqual(legacy.show, true);
});

test('T306 index.html role-gates the paint path on owner bubbles', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('isOwnerRole') >= 0, 'index paint path consults isOwnerRole');
  const sync = /function syncTargetContextChrome\(d, paintOpts\) \{[\s\S]*?\n\}/.exec(html);
  assert.ok(sync, 'syncTargetContextChrome found');
  const body = sync[0];
  assert.ok(/isOwnerRole/.test(body), 'owner-role gate inside syncTargetContextChrome');
  const gateAt = body.indexOf('isOwnerRole');
  const paintAt = body.indexOf('chromeModel');
  assert.ok(paintAt < 0 || gateAt < paintAt, 'role gate precedes chromeModel paint');
  assert.ok(/isOwnerRole[\s\S]{0,200}clearTargetContextChrome/.test(body),
    'owner role clears chrome');
});

if (!process.exitCode) {
  console.log('all target_context_chrome_test passed');
}
