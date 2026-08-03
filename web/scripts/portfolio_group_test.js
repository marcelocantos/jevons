// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for domain portfolio chrome (🎯T200).
//
//   node web/scripts/portfolio_group_test.js

'use strict';

const assert = require('assert');
const PG = require('./portfolio_group.js');

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

console.log('portfolio_group_test (🎯T200)');

test('empty/missing is calm (no chrome)', function () {
  assert.strictEqual(PG.renderPortfoliosHtml(null), '');
  assert.strictEqual(PG.renderPortfoliosHtml(undefined), '');
  assert.strictEqual(PG.renderPortfoliosHtml({}), '');
  assert.strictEqual(PG.renderPortfoliosHtml({ portfolios: [] }), '');
  assert.deepStrictEqual(PG.portfolioModels(null), []);
  assert.deepStrictEqual(PG.portfolioModels({ portfolios: [] }), []);
});

test('fixture portfolio ≥2 members appears as one group', function () {
  const payload = {
    portfolios: [{
      id: 'personal',
      name: 'Personal',
      members: [
        { path: 'github.com/marcelocantos/jevons', label: 'jevons', agents: ['jevons', 'jevons-po'] },
        { path: 'github.com/marcelocantos/pigeon', label: 'pigeon', agents: ['pigeon-w'] },
      ],
    }],
  };
  const models = PG.portfolioModels(payload);
  assert.strictEqual(models.length, 1, 'one portfolio group');
  assert.strictEqual(models[0].id, 'personal');
  assert.strictEqual(models[0].name, 'Personal');
  assert.strictEqual(models[0].memberCount, 2);
  assert.strictEqual(models[0].agentCount, 3);

  const html = PG.renderPortfoliosHtml(payload);
  assert.ok(html.indexOf('portfolio-group') >= 0);
  assert.ok(html.indexOf('data-portfolio="personal"') >= 0);
  assert.ok(html.indexOf('Personal') >= 0);
  assert.ok(html.indexOf('jevons') >= 0);
  assert.ok(html.indexOf('pigeon') >= 0);
  // Single panel wrapping the group (one owner-visible surface).
  assert.ok(html.indexOf('portfolio-panel') >= 0);
  assert.strictEqual((html.match(/portfolio-group/g) || []).length, 1);
});

test('member meta and escaping', function () {
  const html = PG.renderPortfolioGroupHtml({
    id: 'schools',
    name: 'Schools <script>',
    members: [{ path: 'a/b', label: 'b&c', agents: [], agentCount: 0 }],
  });
  assert.ok(html.indexOf('Schools &lt;script&gt;') >= 0);
  assert.ok(html.indexOf('b&amp;c') >= 0);
  assert.ok(html.indexOf('0 agents') >= 0);
  assert.ok(html.indexOf('<script>') === -1);
});

test('empty members calm row', function () {
  const html = PG.renderPortfolioGroupHtml({ id: 'x', name: 'X', members: [] });
  assert.ok(html.indexOf('no members') >= 0);
});

if (process.exitCode) {
  console.error('portfolio_group_test FAILED');
  process.exit(1);
}
console.log('portfolio_group_test PASS');
