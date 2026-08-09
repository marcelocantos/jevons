// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for ConversationWidget (🎯T309.1).
// Run: node web/scripts/conversation_widget_test.js

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const CW = require('./conversation_widget.js');
const CK = require('./composer_keys.js');

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed++;
  } catch (e) {
    console.error('FAIL', name);
    console.error(e && e.stack || e);
    process.exit(1);
  }
}

// ── density + ids ───────────────────────────────────────────────────

test('T309.1 normalizeDensity compact|comfortable', function () {
  assert.strictEqual(CW.normalizeDensity('compact'), CW.DENSITY_COMPACT);
  assert.strictEqual(CW.normalizeDensity('COMPACT'), CW.DENSITY_COMPACT);
  assert.strictEqual(CW.normalizeDensity('comfortable'), CW.DENSITY_COMFORTABLE);
  assert.strictEqual(CW.normalizeDensity(''), CW.DENSITY_COMFORTABLE);
  assert.strictEqual(CW.normalizeDensity(null), CW.DENSITY_COMFORTABLE);
});

test('T309.1 defaultIds differ by density (main vs RHS)', function () {
  const main = CW.defaultIds({ density: 'comfortable' });
  assert.strictEqual(main.messages, 'messages');
  assert.strictEqual(main.input, 'input');
  assert.strictEqual(main.send, 'send');
  assert.strictEqual(main.composer, 'input-bar');

  const side = CW.defaultIds({ density: 'compact' });
  assert.strictEqual(side.messages, 'agent-inspect-body');
  assert.strictEqual(side.input, 'agent-inspect-input');
  assert.strictEqual(side.send, 'agent-inspect-send');
  assert.strictEqual(side.composer, 'agent-inspect-composer');
});

test('T309.1 rootClassName encodes density as CSS param only', function () {
  assert.strictEqual(CW.rootClassName('compact'), 'conversation-widget density-compact');
  assert.strictEqual(CW.rootClassName('comfortable'), 'conversation-widget density-comfortable');
});

// ── draft store ─────────────────────────────────────────────────────

test('T309.1 createDraftStore per-agent stash', function () {
  const d = CW.createDraftStore();
  assert.strictEqual(d.get('a'), '');
  d.set('a', 'hello');
  d.set('b', 'world');
  assert.strictEqual(d.get('a'), 'hello');
  assert.strictEqual(d.get('b'), 'world');
  d.clear('a');
  assert.strictEqual(d.get('a'), '');
  assert.strictEqual(d.get('b'), 'world');
});

test('T309.1 isDraftEmpty', function () {
  assert.strictEqual(CW.isDraftEmpty(''), true);
  assert.strictEqual(CW.isDraftEmpty('  \n'), true);
  assert.strictEqual(CW.isDraftEmpty('x'), false);
});

// ── key classification (one path, density param) ────────────────────

test('T309.1 classifyComposerKey compact: Enter send, Shift+Enter newline', function () {
  assert.strictEqual(CW.classifyComposerKey({ key: 'Enter' }, { density: 'compact' }), 'send');
  assert.strictEqual(
    CW.classifyComposerKey({ key: 'Enter', shiftKey: true }, { density: 'compact' }),
    'newline',
  );
  assert.strictEqual(CW.classifyComposerKey({ key: 'a' }, { density: 'compact' }), null);
  assert.strictEqual(
    CW.classifyComposerKey({ key: 'Enter', isComposing: true }, { density: 'compact' }),
    null,
  );
});

test('T309.1 classifyComposerKey comfortable uses ComposerKeys when provided', function () {
  const act = CW.classifyComposerKey(
    { key: 'Enter', ctrlKey: true },
    { density: 'comfortable', ComposerKeys: CK, composerEmpty: false },
  );
  assert.strictEqual(act, 'interrupt', 'Ctrl+Enter interject on comfortable');
  const plain = CW.classifyComposerKey(
    { key: 'Enter' },
    { density: 'comfortable', ComposerKeys: CK, composerEmpty: false },
  );
  assert.strictEqual(plain, 'send');
  const shift = CW.classifyComposerKey(
    { key: 'Enter', shiftKey: true },
    { density: 'comfortable', ComposerKeys: CK },
  );
  assert.strictEqual(shift, 'newline');
});

// ── send request + optimistic ───────────────────────────────────────

test('T309.1 buildSendRequest targets agent send API', function () {
  const ok = CW.buildSendRequest('att-msf-1', '  ship it  ');
  assert.strictEqual(ok.ok, true);
  assert.strictEqual(ok.name, 'att-msf-1');
  assert.strictEqual(ok.method, 'POST');
  assert.strictEqual(ok.url, '/api/agents/att-msf-1/send');
  assert.deepStrictEqual(ok.body, { text: 'ship it' });

  const enc = CW.buildSendRequest('jv-t27.2-config', 'go');
  assert.strictEqual(enc.ok, true);
  assert.strictEqual(enc.url, '/api/agents/' + encodeURIComponent('jv-t27.2-config') + '/send');

  assert.strictEqual(CW.buildSendRequest(null, 'x').reason, 'no-selection');
  assert.strictEqual(CW.buildSendRequest('att-x', '   ').reason, 'empty');
  assert.strictEqual(CW.buildSendRequest('jevons', 'hi').reason, 'overseer-main-only');
  assert.strictEqual(CW.agentSendPath('po'), '/api/agents/po/send');
});

test('T309.1 sendBlockMessage is loud for every block reason', function () {
  assert.ok(CW.sendBlockMessage('no-selection').indexOf('selected') >= 0);
  assert.ok(CW.sendBlockMessage('overseer-main-only').indexOf('main chat') >= 0);
  assert.ok(CW.sendBlockMessage('empty').indexOf('empty') >= 0);
  assert.ok(CW.sendBlockMessage('').indexOf('silent') >= 0 ||
    CW.sendBlockMessage('').indexOf('unknown') >= 0);
});

test('T309.1 afterSendOptimistic appends user + opens working', function () {
  const r = CW.afterSendOptimistic([], 'do it', { title: 'po' });
  assert.strictEqual(r.lines.length, 1);
  assert.strictEqual(r.lines[0].role, 'user');
  assert.strictEqual(r.lines[0].text, 'do it');
  assert.ok(r.lines[0].when);
  assert.strictEqual(r.model.working, true);
  assert.strictEqual(r.model.title, 'po');
});

test('T309.1 composerVisible transcript+selection only', function () {
  assert.strictEqual(CW.composerVisible({
    tab: 'transcript', selectedAgent: 'worker',
  }), true);
  assert.strictEqual(CW.composerVisible({
    tab: 'frontier', selectedAgent: 'worker',
  }), false);
  assert.strictEqual(CW.composerVisible({
    tab: 'transcript', selectedAgent: null,
  }), false);
  assert.strictEqual(CW.composerVisible({
    tab: 'transcript', selectedAgent: 'jevons',
  }), false);
});

// ── both mounts structural (index.html) ─────────────────────────────

test('T309.1 index.html loads conversation_widget and mounts both densities', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/conversation_widget.js') >= 0,
    'must load conversation_widget.js');
  assert.ok(html.indexOf('ConversationWidget') >= 0,
    'must reference ConversationWidget');
  assert.ok(html.indexOf('density-compact') >= 0 || html.indexOf("density: 'compact'") >= 0
    || html.indexOf('density: "compact"') >= 0 || html.indexOf("density:'compact'") >= 0,
    'compact density for RHS mount');
  assert.ok(html.indexOf('density-comfortable') >= 0 || html.indexOf("density: 'comfortable'") >= 0
    || html.indexOf('density: "comfortable"') >= 0 || html.indexOf("density:'comfortable'") >= 0,
    'comfortable density for main mount');
  // One widget owns both surfaces — main + RHS hosts marked conversation-widget.
  assert.ok(html.indexOf('conversation-widget') >= 0, 'widget class present');
  // Legacy dual-path markers must route through widget.
  assert.ok(
    html.indexOf('ConversationWidget.mount') >= 0 ||
    html.indexOf('ConversationWidget && ConversationWidget.mount') >= 0,
    'mount() used for at least one surface',
  );
});

test('T309.1 index.html: sidebar composer uses widget classify/send (not a second path)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Product send path still exists for RHS, but classification comes from widget.
  assert.ok(
    html.indexOf('ConversationWidget.classifyComposerKey') >= 0 ||
    html.indexOf('classifyComposerKey') >= 0,
    'composer key path goes through widget',
  );
  // Compact composer ids still present (widget adopts them).
  assert.ok(html.indexOf('id="agent-inspect-composer"') >= 0);
  assert.ok(html.indexOf('id="agent-inspect-input"') >= 0);
  assert.ok(html.indexOf('id="agent-inspect-body"') >= 0);
  assert.ok(html.indexOf('id="messages"') >= 0 && html.indexOf('id="input"') >= 0,
    'main host nodes present for comfortable adopt');
});

test('T309.1 index.html: renderAgentInspect is widget mount host', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const renderFn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(renderFn, 'renderAgentInspect present before loadAgentTranscript');
  // Host delegates paint loop to widget when controller exists.
  assert.ok(
    renderFn[0].indexOf('renderModel') >= 0 ||
    renderFn[0].indexOf('_inspectWidget') >= 0 ||
    renderFn[0].indexOf('inspectConversation') >= 0 ||
    renderFn[0].indexOf('ConversationWidget') >= 0,
    'renderAgentInspect delegates to widget renderModel / controller',
  );
});

console.log('PASS conversation_widget_test (' + passed + ' tests)');
