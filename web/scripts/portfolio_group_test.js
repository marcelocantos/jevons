// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for domain portfolio fleet-tree weave (🎯T200).
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
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 8).join('\n    ') : e);
    process.exitCode = 1;
  }
}

console.log('portfolio_group_test (🎯T200 tree weave)');

test('empty/missing portfolios: identity tree (calm)', function () {
  const agents = [
    { name: 'jevons', parent: '', workdir: '/Users/x/.jevons/jevons', status: 'running' },
    { name: 'jevons-po', parent: 'jevons', workdir: '/Users/x/work/github.com/org/repo', status: 'running' },
  ];
  const w = PG.weaveFleetTree(agents, null);
  assert.strictEqual(w.overseerName, 'jevons');
  assert.strictEqual(w.nodes.length, 2);
  assert.ok(w.byParent['jevons']);
  assert.strictEqual(w.byParent['jevons'].length, 1);
  assert.strictEqual(w.byParent['jevons'][0].name, 'jevons-po');
  assert.deepStrictEqual(PG.weaveFleetTree(agents, { portfolios: [] }).nodes.map(n => n.name),
    ['jevons', 'jevons-po']);
});

test('hermetic: portfolio A with two POs + one unassigned under jevons', function () {
  // Fixture: jevons → A → {po1,po2} and jevons → unassigned-po
  const agents = [
    { name: 'jevons', parent: '', workdir: '/Users/x/.jevons/jevons', status: 'running' },
    { name: 'po1', parent: 'jevons', workdir: '/Users/x/work/github.com/org/repo-a', status: 'running' },
    { name: 'po2', parent: 'jevons', workdir: '/Users/x/work/github.com/org/repo-b', status: 'stopped' },
    { name: 'unassigned-po', parent: 'jevons', workdir: '/Users/x/work/github.com/other/solo', status: 'running' },
  ];
  const portfolios = {
    portfolios: [{
      id: 'A',
      name: 'Domain A',
      members: [
        { path: 'github.com/org/repo-a' },
        { path: 'github.com/org/repo-b' },
      ],
    }],
  };
  const w = PG.weaveFleetTree(agents, portfolios);
  const pf = PG.portfolioNodeName('A');
  assert.strictEqual(pf, 'portfolio:A');

  // Virtual portfolio under jevons
  const underJevons = (w.byParent['jevons'] || []).map(n => n.name).sort();
  assert.ok(underJevons.indexOf(pf) >= 0, 'portfolio A under jevons: ' + underJevons);
  assert.ok(underJevons.indexOf('unassigned-po') >= 0, 'unassigned under jevons');
  assert.ok(underJevons.indexOf('po1') < 0, 'po1 not direct under jevons');
  assert.ok(underJevons.indexOf('po2') < 0, 'po2 not direct under jevons');

  // Both POs under portfolio A
  const underA = (w.byParent[pf] || []).map(n => n.name).sort();
  assert.deepStrictEqual(underA, ['po1', 'po2']);

  // Lineage preserved (registry parent stays for kill safety)
  const po1 = w.nodes.find(n => n.name === 'po1');
  assert.strictEqual(po1.lineage_parent, 'jevons');
  assert.strictEqual(po1.parent, pf);

  // Portfolio node chrome
  const pNode = w.nodes.find(n => n.name === pf);
  assert.ok(PG.isPortfolioNode(pNode));
  assert.strictEqual(pNode.purpose, 'portfolio');
  assert.strictEqual(pNode.description, 'Domain A');
});

test('portfolio row uses folder chrome, not agent-dot status', function () {
  const chrome = PG.portfolioRowChrome({
    name: 'portfolio:A',
    description: 'Domain A',
    purpose: 'portfolio',
    is_portfolio: true,
  });
  assert.strictEqual(chrome.leadKind, 'folder');
  assert.ok(chrome.leadHtml.indexOf('agent-folder') >= 0);
  assert.ok(chrome.leadHtml.indexOf(PG.FOLDER_ICON) >= 0 || chrome.leadHtml.indexOf('📁') >= 0);
  assert.ok(chrome.leadHtml.indexOf('agent-dot') < 0, 'no status dot on portfolio');
  assert.ok(chrome.leadHtml.indexOf('running') < 0);
  assert.ok(chrome.leadHtml.indexOf('stopped') < 0);

  const agentLead = PG.rowLeadHtml({ name: 'po1', status: 'running' });
  assert.ok(agentLead.indexOf('agent-dot') >= 0);
  assert.ok(agentLead.indexOf('running') >= 0);
  assert.ok(agentLead.indexOf('agent-folder') < 0);

  const pfLead = PG.rowLeadHtml({ name: 'portfolio:A', purpose: 'portfolio', description: 'A' });
  assert.ok(pfLead.indexOf('agent-folder') >= 0);
  assert.ok(pfLead.indexOf('agent-dot') < 0);
});

test('membership is path-based, not name parse', function () {
  // Agent named like the product but wrong workdir must not join.
  assert.strictEqual(
    PG.matchPortfolioId('/tmp/unrelated', [{ id: 'A', members: ['github.com/org/repo'] }]),
    ''
  );
  assert.ok(PG.workdirMatchesMember(
    '/Users/x/work/github.com/org/repo',
    'github.com/org/repo'
  ));
  // Only reparent overseer children — worker under PO stays under PO.
  const agents = [
    { name: 'jevons', parent: '', workdir: '/Users/x/.jevons/jevons' },
    { name: 'po1', parent: 'jevons', workdir: '/Users/x/work/github.com/org/repo' },
    { name: 'worker', parent: 'po1', workdir: '/Users/x/work/github.com/org/repo' },
  ];
  const w = PG.weaveFleetTree(agents, {
    portfolios: [{ id: 'A', members: ['github.com/org/repo'] }],
  });
  const worker = w.nodes.find(n => n.name === 'worker');
  assert.strictEqual(worker.parent, 'po1', 'worker stays under PO (lineage)');
  const po1 = w.nodes.find(n => n.name === 'po1');
  assert.strictEqual(po1.parent, 'portfolio:A');
});

test('workers keep status dots; portfolio never uses running/stopped-only lead', function () {
  assert.ok(/agent-dot running/.test(PG.rowLeadHtml({ status: 'running' })));
  assert.ok(/agent-dot stopped/.test(PG.rowLeadHtml({ status: 'stopped' })));
  const pf = PG.rowLeadHtml({ purpose: 'portfolio', name: 'portfolio:x' });
  assert.ok(pf.indexOf('agent-dot') < 0);
  assert.ok(pf.indexOf('agent-folder') >= 0);
});

if (process.exitCode) {
  console.error('portfolio_group_test FAILED');
  process.exit(1);
}
console.log('portfolio_group_test PASS');
