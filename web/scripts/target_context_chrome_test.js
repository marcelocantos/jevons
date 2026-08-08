// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for target-ask context chrome (🎯T266), speaker/context
// split (🎯T273), owner-role gate (🎯T306), and the 🎯T314 composition rule:
// the tab is [optional speaker][optional context] — never repo-as-speaker,
// never a ledger-owning PO invented into the speaker role.
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

const LT = TCC.SPEAKER_LT;
const GT = TCC.SPEAKER_GT;

console.log('target_context_chrome_test (🎯T266 / 🎯T273 / 🎯T306 / 🎯T314)');

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

// ── 🎯T314 identity rules: who may occupy the speaker role ──

test('T314 isOmittedSpeakerIdentity: root overseer / Jevons only', function () {
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('jevons'), true);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('Jevons'), true);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('overseer'), true);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity(''), true);
  // A PO name is a valid identity — but only as a PROVEN author (below).
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('jevons-po'), false);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('bullseye-po'), false);
  assert.strictEqual(TCC.isOmittedSpeakerIdentity('jv-t314-context-tab'), false);
});

test('T314 looksLikeRepoLabel keeps org/repo out of the speaker role', function () {
  assert.strictEqual(TCC.looksLikeRepoLabel('marcelocantos/jevons'), true);
  assert.strictEqual(TCC.looksLikeRepoLabel('jevons'), false);
  assert.strictEqual(TCC.looksLikeRepoLabel('jevons-po'), false);
  assert.strictEqual(TCC.formatSpeakerLabel('marcelocantos/jevons'), '');
  assert.strictEqual(TCC.formatSpeakerHTML('marcelocantos/jevons'), '');
});

test('T314 speaker comes from the proven author, never from repo/product/PO', function () {
  // Ownership context alone invents nothing.
  assert.strictEqual(TCC.messageAuthor({ repo: 'marcelocantos/jevons', po: 'jevons-po' }), '');
  assert.strictEqual(TCC.messageAuthor({ product: 'bullseye', po: 'bullseye-po' }), '');
  assert.strictEqual(TCC.formatSpeakerLabel({ product: 'bullseye', po: 'bullseye-po' }), '');
  // A proven author paints — including a PO that actually wrote the bubble.
  assert.strictEqual(TCC.messageAuthor({ author: 'jv-t57-graph' }), 'jv-t57-graph');
  assert.strictEqual(TCC.messageAuthor({ agent: 'bullseye-po' }), 'bullseye-po');
  assert.strictEqual(TCC.messageAuthor({ speaker: 'jevons-po' }), 'jevons-po');
  assert.strictEqual(TCC.messageAuthor({ from: 'jv-t314-context-tab' }), 'jv-t314-context-tab');
  // Root overseer authorship is omitted (no speaker segment).
  assert.strictEqual(TCC.messageAuthor({ author: 'jevons' }), '');
  assert.strictEqual(TCC.messageAuthor({ author: 'overseer' }), '');
  // Repo-shaped author is context, never a name.
  assert.strictEqual(TCC.messageAuthor({ author: 'marcelocantos/jevons' }), '');
});

test('T314 formatSpeakerHTML is the bold name token only', function () {
  const html = TCC.formatSpeakerHTML({ author: 'bullseye-po' });
  assert.ok(html.indexOf('ctx-speaker') >= 0, 'ctx-speaker class');
  assert.ok(html.indexOf(LT + 'bullseye-po' + GT) >= 0);
  assert.ok(html.indexOf('ctx-sep') < 0, 'no sep span on speaker HTML');
  assert.ok(html.indexOf('·') < 0, 'no · in speaker HTML');
  assert.strictEqual(TCC.formatSpeakerHTML({ product: 'jevons' }), '');
});

test('T314 context chrome is the repo label in its own dim role', function () {
  const html = TCC.formatContextHTML({ repo: 'marcelocantos/jevons', product: 'jevons', po: 'jevons-po' });
  assert.ok(html.indexOf('ctx-context') >= 0, 'dim context role');
  assert.ok(html.indexOf('marcelocantos/jevons') >= 0, 'repo head');
  assert.ok(html.indexOf('ctx-speaker') < 0, 'context never uses the speaker role');
  assert.ok(html.indexOf('jevons-po') < 0, 'ledger PO is not painted context');
  assert.ok(html.indexOf('·') < 0, 'no · tail');
  assert.strictEqual(
    TCC.formatChromeLabel({ repo: 'marcelocantos/jevons', product: 'jevons', po: 'jevons-po' }),
    'marcelocantos/jevons'
  );
  // Bare product leaf is a name-shaped token — never the head when it is the
  // overseer product; a non-overseer product leaf is fine as a fallback.
  assert.strictEqual(TCC.contextHead({ product: 'jevons' }), '');
  assert.strictEqual(TCC.contextHead({ product: 'bullseye' }), 'bullseye');
  assert.strictEqual(
    TCC.contextHead({ product: 'bullseye', repo: 'marcelocantos/bullseye' }),
    'marcelocantos/bullseye'
  );
});

// ── 🎯T314 acceptance 4: hermetic composition fixtures ──

test('T314 overseer bubble about T311 → no speaker, repo context only', function () {
  const agents = [
    { name: 'jevons', purpose: 'overseer', workdir: '/Users/m/.jevons/jevons' },
    {
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
      parent: 'jevons',
    },
    {
      name: 'jv-t311-model-badge',
      purpose: 'work',
      parent: 'jevons-po',
      target_id: 'T311',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    },
  ];
  const model = TCC.chromeModel({
    role: 'jevons',
    text: '🎯T311 status: badge shows the model an agent is RUNNING — accept?',
    agents: agents,
  });
  assert.strictEqual(model.show, true, 'provenance context still paints');
  assert.strictEqual(model.repo, 'marcelocantos/jevons');
  assert.strictEqual(model.speaker, '', 'root overseer author is omitted');
  // The owner screenshot bug, pinned: no "marcelocantos/jevons · jevons-po".
  assert.strictEqual(model.label, 'marcelocantos/jevons');
  assert.ok(model.innerHTML.indexOf('ctx-speaker') < 0, 'no speaker token');
  assert.ok(model.innerHTML.indexOf('jevons-po') < 0, 'no invented PO in the tab');
  assert.ok(model.innerHTML.indexOf('·') < 0, 'no · second-speaker tail');
  assert.ok(model.innerHTML.indexOf('ctx-context') >= 0, 'repo in the dim context role');
  // Ledger ownership survives as hover provenance only, labelled as such.
  assert.strictEqual(model.po, 'jevons-po');
  assert.ok(model.title.indexOf('ledger PO jevons-po') >= 0, 'PO is hover provenance');
  assert.ok(model.title.indexOf('T311') >= 0, 'target in title');
});

test('T314 non-jevons agent bubble → 〈agent〉 then optional repo, in order', function () {
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
  ];
  const model = TCC.chromeModel({
    role: 'assistant',
    author: 'jv-t57-graph',
    text: '🎯T57 needs-owner: accept graph expansion API?',
    agents: agents,
  });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.repo, 'marcelocantos/bullseye');
  assert.strictEqual(model.speaker, 'jv-t57-graph', 'proven author is the speaker');
  const want = LT + 'jv-t57-graph' + GT;
  assert.strictEqual(model.label, want + ' marcelocantos/bullseye');
  // Composition order: speaker segment precedes context segment.
  const speakerAt = model.innerHTML.indexOf('ctx-speaker');
  const contextAt = model.innerHTML.indexOf('ctx-context');
  assert.ok(speakerAt >= 0 && contextAt >= 0, 'both segments present');
  assert.ok(speakerAt < contextAt, 'speaker precedes context');
  assert.ok(model.innerHTML.indexOf(want) >= 0, '〈jv-t57-graph〉 painted');
  assert.ok(model.innerHTML.indexOf('·') < 0, 'no · separator');
  assert.ok(model.innerHTML.indexOf('bullseye-po') < 0, 'parent PO is not a token');
});

test('T314 jevons-product bubble authored by the PO → PO speaks only when proven', function () {
  const base = {
    role: 'assistant',
    text: 'Decision packet for 🎯T314 — please confirm owner accept.',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    agents: [{
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    }],
  };
  // (a) Author unknown: ledger ownership must NOT promote jevons-po to speaker.
  const inferred = TCC.chromeModel(base);
  assert.strictEqual(inferred.show, true);
  assert.strictEqual(inferred.speaker, '');
  assert.strictEqual(inferred.label, 'marcelocantos/jevons');
  assert.ok(inferred.innerHTML.indexOf('jevons-po') < 0, 'no PO token without proof');
  assert.ok(inferred.innerHTML.indexOf('ctx-speaker') < 0);
  // (b) Author proven as jevons-po: it paints, as the speaker, first.
  const authored = TCC.chromeModel(Object.assign({ author: 'jevons-po' }, base));
  assert.strictEqual(authored.speaker, 'jevons-po');
  assert.strictEqual(
    authored.label,
    LT + 'jevons-po' + GT + ' marcelocantos/jevons'
  );
  assert.ok(authored.innerHTML.indexOf(LT + 'jevons-po' + GT) >= 0);
  assert.ok(authored.innerHTML.indexOf('ctx-po') < 0, 'PO never paints in the meta role');
  assert.ok(authored.innerHTML.indexOf('·') < 0);
  assert.ok(
    authored.innerHTML.indexOf('ctx-speaker') < authored.innerHTML.indexOf('ctx-context'),
    'speaker precedes context'
  );
});

test('T314 proven author paints even when no repo resolves', function () {
  const model = TCC.chromeModel({
    role: 'assistant',
    author: 'jv-t314-context-tab',
    force: true,
    targetId: 'T314',
  });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.label, LT + 'jv-t314-context-tab' + GT);
  assert.ok(model.innerHTML.indexOf('ctx-context') < 0, 'no empty context segment');
});

test('T314 root-overseer author with no repo paints nothing', function () {
  const model = TCC.chromeModel({
    role: 'jevons',
    author: 'jevons',
    force: true,
    targetId: 'T314',
  });
  assert.strictEqual(model.show, false);
  assert.strictEqual(model.innerHTML, '');
  assert.strictEqual(model.label, '');
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
  // Ledger ownership still resolves — as provenance metadata, not a token.
  assert.strictEqual(ctx.po, 'bullseye-po');
  assert.strictEqual(ctx.label, 'marcelocantos/bullseye');
});

test('ledger fallback when no engaged worker — repo context, no PO tail', function () {
  const opts = {
    text: 'Decision packet for 🎯T262.4 — please confirm owner accept.',
    ledger: '/Users/m/work/github.com/marcelocantos/jevons/bullseye.yaml',
    agents: [{
      name: 'jevons-po',
      purpose: 'work',
      workdir: '/Users/m/work/github.com/marcelocantos/jevons',
    }],
  };
  const ctx = TCC.resolveTargetContext(opts);
  assert.strictEqual(ctx.repo, 'marcelocantos/jevons');
  assert.strictEqual(ctx.show, true, 'context-paint gates on ask + resolved chrome');
  assert.strictEqual(ctx.label, 'marcelocantos/jevons');
  const model = TCC.chromeModel(opts);
  assert.strictEqual(model.show, true);
  assert.ok(model.innerHTML.indexOf('ctx-context') >= 0);
  assert.ok(model.innerHTML.indexOf('jevons-po') < 0);
  assert.ok(model.innerHTML.indexOf('ctx-speaker') < 0);
});

test('no chrome without repo or proven author', function () {
  const ctx = TCC.resolveTargetContext({
    text: '🎯T999 needs-owner: accept?',
    agents: [],
  });
  assert.strictEqual(ctx.show, false);
});

test('force + explicit repo/po paints repo context only', function () {
  const model = TCC.chromeModel({
    force: true,
    targetId: 'T57',
    repo: 'marcelocantos/bullseye',
    po: 'bullseye-po',
  });
  assert.strictEqual(model.show, true);
  assert.strictEqual(model.label, 'marcelocantos/bullseye');
  assert.ok(model.innerHTML.indexOf('·') < 0);
  assert.ok(model.innerHTML.indexOf('ctx-speaker') < 0, 'explicit PO is not an author');
  assert.ok(model.innerHTML.indexOf('bullseye-po') < 0);
});

test('index.html wires TargetContextChrome + speaker/context roles', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/target_context_chrome.js') >= 0, 'script tag');
  assert.ok(html.indexOf('msg-context-tab') >= 0, 'msg-context-tab class');
  assert.ok(html.indexOf('syncTargetContextChrome') >= 0, 'sync attach');
  assert.ok(html.indexOf('TargetContextChrome') >= 0, 'global use');
  assert.ok(html.indexOf('ctx-speaker') >= 0, 'speaker class in CSS');
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
  const asOwner = TCC.resolveTargetContext(Object.assign({ role: 'user' }, opts));
  assert.strictEqual(asOwner.show, false, 'owner bubble must never show chrome');
  const ownerModel = TCC.chromeModel(Object.assign({ role: 'user' }, opts));
  assert.strictEqual(ownerModel.show, false);
  assert.strictEqual(ownerModel.innerHTML, '', 'no painted chrome for owner');
  assert.strictEqual(ownerModel.label, '');
});

test('T306 owner role beats force, explicit repo/po, and a proven author', function () {
  const model = TCC.chromeModel({
    role: 'user',
    force: true,
    author: 'jevons-po',
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
    assert.strictEqual(model.label, 'marcelocantos/jevons');
    assert.ok(model.innerHTML.indexOf('ctx-context') >= 0, 'ctx-context for role ' + role);
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
