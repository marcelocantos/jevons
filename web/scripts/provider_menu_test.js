// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for the fleet-tree provider icon menu + "!" band
// mark (🎯T285.2).
//
//   node web/scripts/provider_menu_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const PM = require('./provider_menu.js');

function indexHtml() {
  return fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
}

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

console.log('provider_menu_test (🎯T285.2)');

const OPTIONS = {
  providers: [
    {
      provider: 'grok', band: 'hot', eligible: false,
      reason: 'Grok weekly hot — burn 2.5×, 15% remaining',
      models: ['grok-4.5', 'grok-4'],
    },
    {
      provider: 'claude', band: 'ok', eligible: true, reason: 'Claude weekly on pace',
      models: ['claude-fable-5', 'claude-opus-5', 'claude-sonnet-5'],
    },
    { provider: 'codex', band: 'unpublished', eligible: false,
      reason: 'Codex — no weekly window published', models: [] },
  ],
};

// --- band → "!" mapping (server band in, mark colour out) ---------------

test('band→mark: hot and exhausted are red (off)', function () {
  assert.strictEqual(PM.bandMark('hot'), 'off');
  assert.strictEqual(PM.bandMark('exhausted'), 'off');
});

test('band→mark: ahead is orange', function () {
  assert.strictEqual(PM.bandMark('ahead'), 'ahead');
});

test('band→mark: ok/under/locked/unpublished carry no mark', function () {
  ['ok', 'under', 'locked', 'unpublished', ''].forEach(function (b) {
    assert.strictEqual(PM.bandMark(b), '');
  });
});

test('markHtml: hot provider paints red ! with the server reason as title', function () {
  const html = PM.markHtml({ name: 'jv-x', provider: 'grok' }, OPTIONS);
  assert.ok(/prov-mark-off/.test(html), html);
  assert.ok(html.indexOf('Grok weekly hot') !== -1, html);
  assert.ok(html.indexOf('>!<') !== -1, html);
});

test('markHtml: ok provider paints nothing', function () {
  assert.strictEqual(PM.markHtml({ provider: 'claude' }, OPTIONS), '');
});

test('markHtml: unknown provider paints nothing (no invented band)', function () {
  assert.strictEqual(PM.markHtml({ provider: 'bedrock' }, OPTIONS), '');
});

// --- menu shape: two columns, borderless -------------------------------

test('menu is a table with exactly two cells per provider row', function () {
  const rows = PM.menuModel(OPTIONS, { name: 'jv-x', provider: 'grok', purpose: 'work' });
  const html = PM.menuHtml(rows);
  const trs = html.match(/<tr/g) || [];
  assert.strictEqual(trs.length, 3, html);
  const marks = html.match(/prov-cell-mark/g) || [];
  const models = html.match(/prov-cell-models/g) || [];
  assert.strictEqual(marks.length, 3);
  assert.strictEqual(models.length, 3);
});

test('menu carries no border class or attribute', function () {
  const html = PM.menuHtml(PM.menuModel(OPTIONS, { provider: 'grok' }));
  assert.ok(!/border/i.test(html), html);
});

test('models render left-to-right best first (server order)', function () {
  const rows = PM.menuModel(OPTIONS, { name: 'jv-x', provider: 'grok', purpose: 'work' });
  const claude = rows.filter(function (r) { return r.provider === 'claude'; })[0];
  assert.deepStrictEqual(
    claude.models.map(function (m) { return m.id; }),
    ['claude-fable-5', 'claude-opus-5', 'claude-sonnet-5']);
  const html = PM.menuHtml(rows);
  assert.ok(html.indexOf('claude-fable-5') < html.indexOf('claude-opus-5'), 'order lost');
});

test('ineligible provider is visible, disabled, and wears the ! reason', function () {
  const rows = PM.menuModel(OPTIONS, { name: 'jv-x', provider: 'claude', purpose: 'work' });
  const grok = rows.filter(function (r) { return r.provider === 'grok'; })[0];
  assert.ok(grok, 'hot provider must stay visible');
  assert.strictEqual(grok.disabled, true);
  assert.strictEqual(grok.reason, 'Grok weekly hot — burn 2.5×, 15% remaining');
  grok.models.forEach(function (m) { assert.strictEqual(m.disabled, true); });
  const html = PM.menuHtml(rows);
  assert.ok(html.indexOf('Grok weekly hot') !== -1, 'reason must reach the row title');
});

test('provider with no models offers its default', function () {
  const rows = PM.menuModel(OPTIONS, { name: 'jv-x', provider: 'grok', purpose: 'work' });
  const codex = rows.filter(function (r) { return r.provider === 'codex'; })[0];
  assert.strictEqual(codex.models.length, 1);
  assert.strictEqual(codex.models[0].id, '');
  assert.strictEqual(codex.models[0].label, 'default');
});

test('current provider+model is marked current and inert', function () {
  const rows = PM.menuModel(OPTIONS,
    { name: 'jv-x', provider: 'claude', model: 'claude-fable-5', purpose: 'work' });
  const claude = rows.filter(function (r) { return r.provider === 'claude'; })[0];
  assert.strictEqual(claude.models[0].current, true);
  assert.strictEqual(claude.models[0].disabled, true);
  assert.strictEqual(claude.models[1].current, false);
  assert.strictEqual(claude.models[1].disabled, false, 'same-provider pin stays choosable for fleet seats');
});

test('overseer same-provider entries are visible but disabled (no re-pin without rotation)', function () {
  const rows = PM.menuModel(OPTIONS,
    { name: 'jevons', provider: 'claude', model: 'claude-fable-5', purpose: 'overseer' });
  const claude = rows.filter(function (r) { return r.provider === 'claude'; })[0];
  assert.strictEqual(claude.models[1].disabled, true);
  assert.ok(/rotation/.test(claude.models[1].reason), claude.models[1].reason);
});

// --- click-icon vs click-name ------------------------------------------

test('click on the provider icon opens the menu', function () {
  assert.strictEqual(PM.clickOpensMenu(['model-icon', 'model-badge', 'agent-node']), true);
});

test('click on the ! mark opens the menu too', function () {
  assert.strictEqual(PM.clickOpensMenu(['prov-mark prov-mark-off', 'agent-node']), true);
});

test('click on the agent name selects, never menus', function () {
  assert.strictEqual(PM.clickOpensMenu(['agent-name', 'agent-node']), false);
});

test('click elsewhere on the row selects', function () {
  assert.strictEqual(PM.clickOpensMenu(['agent-dir', 'agent-node']), false);
});

// --- migrate routing ----------------------------------------------------

test('overseer choice hits POST /api/overseer/migrate', function () {
  const req = PM.migrateRequest(
    { name: 'jevons', provider: 'grok', purpose: 'overseer' }, 'claude', 'claude-opus-5');
  assert.strictEqual(req.path, '/api/overseer/migrate');
  assert.deepStrictEqual(req.body, { provider: 'claude', model: 'claude-opus-5' });
});

test('worker choice hits POST /api/agents/migrate with its name', function () {
  const req = PM.migrateRequest(
    { name: 'jv-t1-x', provider: 'grok', purpose: 'work' }, 'claude', 'claude-sonnet-5');
  assert.strictEqual(req.path, '/api/agents/migrate');
  assert.deepStrictEqual(req.body,
    { name: 'jv-t1-x', provider: 'claude', model: 'claude-sonnet-5' });
});

test('PO (non-overseer, non-work purpose) still routes to the fleet wrapper', function () {
  const req = PM.migrateRequest(
    { name: 'jevons-po', provider: 'grok', purpose: 'po' }, 'claude', '');
  assert.strictEqual(req.path, '/api/agents/migrate');
});

// Product wiring: the module is a helper nobody calls unless the tree
// paints the mark, opens the menu from the icon, and POSTs the two routes.
test('index.html paints the ! between the badge and the name', function () {
  const html = indexHtml();
  assert.ok(html.indexOf('scripts/provider_menu.js') !== -1, 'script tag missing');
  assert.ok(html.indexOf('ProviderMenu.markHtml(') !== -1, 'markHtml never called');
  const line = /^.*'<span class="agent-name">.*$/m.exec(html);
  assert.ok(line, 'agent-name paint site missing');
  const before = line[0].slice(0, line[0].indexOf('\'<span class="agent-name">'));
  assert.ok(/markHtml/.test(before), 'mark is not painted before the bare name: ' + line[0].trim());
  assert.ok(/modelHtml|modelPrefixHtml/.test(before), 'badge is not before the mark+name');
});

test('index.html maps server bands onto orange/red ! CSS and a borderless menu', function () {
  const html = indexHtml();
  assert.ok(/\.prov-mark-ahead\s*\{[^}]*--amber/.test(html), 'ahead ! is not amber');
  assert.ok(/\.prov-mark-off\s*\{[^}]*--red/.test(html), 'hot/exhausted ! is not red');
  const menu = /#prov-menu\s*\{([^}]*)\}/.exec(html);
  assert.ok(menu, '#prov-menu rule missing');
  assert.ok(/border:\s*none/.test(menu[1]), '#prov-menu must be borderless');
  assert.ok(html.indexOf('ProviderMenu.clickOpensMenu') !== -1, 'icon click not wired');
  assert.ok(html.indexOf('ProviderMenu.migrateRequest') !== -1, 'migrate POST not wired');
  assert.ok(html.indexOf('/api/migrate/options') !== -1, 'options fetch missing');
});

process.exit(process.exitCode || 0);
