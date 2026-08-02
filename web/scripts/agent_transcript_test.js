// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const AT = require('./agent_transcript.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 5).join('\n     ') : e);
  }
}

test('nextSelection toggles off', function () {
  assert.strictEqual(AT.nextSelection(null, 'po'), 'po');
  assert.strictEqual(AT.nextSelection('po', 'po'), null);
  assert.strictEqual(AT.nextSelection('po', 'worker'), 'worker');
});

test('overseer root never opens transcript pane (T124 residual)', function () {
  assert.strictEqual(AT.isOverseer('jevons'), true);
  assert.strictEqual(AT.isOverseer('Jevons'), true);
  assert.strictEqual(AT.isOverseer('po', 'work'), false);
  assert.strictEqual(AT.isOverseer('ceo', 'overseer'), true);
  assert.strictEqual(AT.shouldOpenTranscript('jevons'), false);
  assert.strictEqual(AT.shouldOpenTranscript('worker', 'work'), true);
  // Click overseer: clear / ignore inspect — never select for pane.
  assert.strictEqual(AT.nextSelection(null, 'jevons'), null);
  assert.strictEqual(AT.nextSelection('po', 'jevons'), null);
  assert.strictEqual(AT.nextSelection(null, 'ceo', { purpose: 'overseer' }), null);
  // Auto-select must not pick overseer even if mis-tagged as new aside name.
  const prev = [];
  const next = [{ name: 'jevons', purpose: 'overseer' }, { name: 'att-x', purpose: 'aside' }];
  assert.strictEqual(AT.pickAutoSelect(prev, next, null), 'att-x');
  assert.strictEqual(AT.pickAutoSelect([], [{ name: 'jevons', purpose: 'overseer' }], 'jevons'), null);
});

test('detectNewAsides finds purpose=aside only', function () {
  const prev = [{ name: 'jevons', purpose: 'overseer' }, { name: 'po', purpose: 'work' }];
  const next = prev.concat([
    { name: 'worker-1', purpose: 'work' },
    { name: 'att-side', purpose: 'aside' },
  ]);
  assert.deepStrictEqual(AT.detectNewAsides(prev, next), ['att-side']);
});

test('pickAutoSelect prefers new aside; keeps selection otherwise', function () {
  const prev = [{ name: 'jevons' }];
  const next = [
    { name: 'jevons', purpose: 'overseer' },
    { name: 'filing', purpose: 'aside' },
  ];
  assert.strictEqual(AT.pickAutoSelect(prev, next, null), 'filing');
  assert.strictEqual(AT.pickAutoSelect(next, next, 'filing'), 'filing');
  assert.strictEqual(AT.pickAutoSelect(next, [{ name: 'jevons' }], 'filing'), null);
});

test('paneModel maps turns; empty and error', function () {
  const m = AT.paneModel('po', {
    name: 'po',
    session_id: 's1',
    turns: [
      { role: 'user', text: 'hello' },
      { role: 'assistant', text: 'hi' },
    ],
  });
  assert.strictEqual(m.title, 'po');
  assert.strictEqual(m.lines.length, 2);
  assert.strictEqual(m.empty, false);
  const empty = AT.paneModel('x', { turns: [] });
  assert.strictEqual(empty.empty, true);
  const err = AT.paneModel('x', null, new Error('no transcript'));
  assert.ok(err.error.indexOf('no transcript') >= 0);
});

test('main chat rule constant is owner-overseer only', function () {
  assert.strictEqual(AT.MAIN_CHAT_IS_OWNER_OVERSEER_ONLY, true);
});

test('index.html wires agent inspect pane + selectAgent transcript', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('agent-inspect') >= 0 || html.indexOf('id="agent-transcript"') >= 0,
    'transcript pane host');
  assert.ok(html.indexOf('scripts/agent_transcript.js') >= 0, 'script tag');
  assert.ok(html.indexOf('/api/agents/') >= 0 && html.indexOf('transcript') >= 0,
    'fetches agent transcript');
  assert.ok(html.indexOf('pickAutoSelect') >= 0 || html.indexOf('AgentTranscript.pickAutoSelect') >= 0,
    'auto-select on new aside');
  // Must not dump fleet monologue into #messages as the select path.
  assert.ok(!/function selectAgent[\s\S]{0,800}msgs\.appendChild/.test(html),
    'selectAgent must not append to main messages');
});

test('T136 create-aside dual-write + no attention chip wall', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ensureFleetAside') >= 0, 'registers fleet aside on create');
  assert.ok(html.indexOf('/api/asides') >= 0, 'POST /api/asides path');
  assert.ok(html.indexOf("attentionBar.classList.remove('visible')") >= 0 ||
    html.indexOf('classList.remove("visible")') >= 0,
    'attention bar not shown for aside stack');
  assert.ok(html.indexOf('appendThreadChip') === -1, 'no chip loop for asides');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall agent_transcript tests passed');
