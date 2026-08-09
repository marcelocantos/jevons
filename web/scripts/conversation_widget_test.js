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
// 🎯T371 needs to assert on the state BETWEEN send-accept and server-answer, so
// a few tests are async. A returned thenable is awaited before the run reports
// PASS — otherwise an async assertion failure would be an unhandled rejection
// and the suite would report green, which is exactly the class of fake oracle
// this target exists to stop.
const asyncTests = [];
function test(name, fn) {
  try {
    const r = fn();
    if (r && typeof r.then === 'function') {
      asyncTests.push(r.then(
        function () { passed++; },
        function (e) {
          console.error('FAIL', name);
          console.error(e && e.stack || e);
          process.exit(1);
        },
      ));
      return;
    }
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

// ── 🎯T371: owner turns never vanish (main ↔ sidebar parity) ─────────
//
// These are the regression net for the Discuss-T364 / att-msln9k27 repro:
// type into a fleet aside composer, press Enter, watch the message disappear.
// Three independent mechanisms could delete an owner bubble; each gets an
// oracle here, and each oracle is written so it fails if the sidebar and main
// contracts fork again (🎯T372 doctrine).

// Minimal injectable DOM — enough for mount() to bind and paint. Deliberately
// tiny: the point is to exercise the REAL send ordering, not to emulate a browser.
function fakeEl(id) {
  const el = {
    id: id,
    value: '',
    disabled: false,
    hidden: false,
    innerHTML: '',
    textContent: '',
    scrollHeight: 20,
    style: {},
    dataset: {},
    children: [],
    _listeners: {},
    classList: {
      _s: {},
      add: function () { for (const a of arguments) this._s[a] = true; },
      remove: function () { for (const a of arguments) delete this._s[a]; },
      toggle: function (c, on) { if (on) this._s[c] = true; else delete this._s[c]; },
      contains: function (c) { return !!this._s[c]; },
    },
    addEventListener: function (t, fn) { (this._listeners[t] = this._listeners[t] || []).push(fn); },
    appendChild: function (c) { this.children.push(c); return c; },
    removeChild: function (c) { this.children = this.children.filter((x) => x !== c); return c; },
    querySelector: function () { return null; },
    querySelectorAll: function () { return []; },
    focus: function () {},
    scrollTo: function () {},
  };
  return el;
}

function fakeDom(density) {
  const ids = CW.defaultIds({ density: density });
  const byId = {};
  [ids.root, ids.messages, ids.composer, ids.input, ids.send].forEach(function (i) {
    if (i) byId[i] = fakeEl(i);
  });
  const host = byId[ids.root] || fakeEl(ids.root || 'host');
  byId[ids.root || 'host'] = host;
  host.querySelector = function (sel) {
    const want = String(sel).replace(/^#/, '').replace(/\\/g, '');
    return byId[want] || null;
  };
  const doc = {
    getElementById: function (i) { return byId[i] || null; },
    createElement: function (tag) { return fakeEl('<' + tag + '>'); },
  };
  return { ids: ids, byId: byId, host: host, doc: doc };
}

// A promise whose settlement this test controls, so we can inspect the DOM
// state in the window between "owner pressed Enter" and "server answered".
function deferred() {
  let resolve, reject;
  const promise = new Promise(function (res, rej) { resolve = res; reject = rej; });
  return { promise: promise, resolve: resolve, reject: reject };
}

test('T371 pending: stage is per-agent, idempotent, and ack consumes one', function () {
  let st = CW.emptyPending();
  st = CW.stagePendingOwnerTurn(st, 'att-a', 'hello', { now: 1000 });
  st = CW.stagePendingOwnerTurn(st, 'att-a', 'hello', { now: 1001 });
  assert.strictEqual(st.items.length, 1, 'same body staged twice while unacked = one item');

  st = CW.stagePendingOwnerTurn(st, 'att-b', 'hello', { now: 1002 });
  assert.strictEqual(st.items.length, 2, 'same body for a different agent is a different turn');
  assert.strictEqual(CW.pendingOwnerTurnsFor(st, 'att-a').length, 1);
  assert.strictEqual(CW.pendingOwnerTurnsFor(st, 'att-b').length, 1);

  // Blank / unnamed never stages.
  assert.strictEqual(CW.stagePendingOwnerTurn(st, 'att-a', '   ').items.length, 2);
  assert.strictEqual(CW.stagePendingOwnerTurn(st, '', 'x').items.length, 2);

  // Acking att-a leaves att-b's identical body alone (no cross-pane bleed).
  const acked = CW.ackPendingOwnerTurns(st, 'att-a', [
    { role: 'user', text: 'hello' },
    { role: 'assistant', text: 'hi' },
  ]);
  assert.strictEqual(acked.acked.length, 1);
  assert.deepStrictEqual(acked.state.items.map((i) => i.agent), ['att-b']);
});

test('T371 pending: a history frame that omits the owner turn cannot delete it', function () {
  let st = CW.stagePendingOwnerTurn(CW.emptyPending(), 'att-a', 'does this send?', { now: 5 });

  // Server replies with sealed history that has not caught up to the turn.
  const serverLines = [
    { role: 'user', text: 'earlier question' },
    { role: 'assistant', text: 'earlier answer' },
  ];
  const kept = CW.applyPendingOwnerTurns(serverLines, st, 'att-a');
  assert.strictEqual(kept.length, 3, 'unsealed owner turn is re-applied, not dropped');
  assert.strictEqual(kept[2].role, 'user');
  assert.strictEqual(kept[2].text, 'does this send?');
  assert.strictEqual(kept[2]._pending, true, 'still marked unsealed');

  // Once the server seals it, ack removes it and it is NOT duplicated.
  const sealed = serverLines.concat([{ role: 'user', text: 'does this send?', when: 9 }]);
  const r = CW.ackPendingOwnerTurns(st, 'att-a', sealed);
  st = r.state;
  assert.strictEqual(r.acked.length, 1);
  const after = CW.applyPendingOwnerTurns(sealed, st, 'att-a');
  assert.strictEqual(after.length, 3, 'no double bubble after the server seals the turn');
  assert.strictEqual(after.filter((l) => l.text === 'does this send?').length, 1);
});

test('T371 pending: a repeated body seals one at a time (no premature ack)', function () {
  let st = CW.emptyPending();
  st = CW.stagePendingOwnerTurn(st, 'att-a', 'ping', { now: 1 });
  const r = CW.ackPendingOwnerTurns(st, 'att-a', [{ role: 'user', text: 'ping' }]);
  assert.strictEqual(r.state.items.length, 0);

  // Two distinct sends of the same body: one sealed line acks exactly one.
  let st2 = CW.stagePendingOwnerTurn(CW.emptyPending(), 'att-a', 'ping', { now: 1, id: 'p1' });
  st2 = { items: st2.items.concat([{ id: 'p2', agent: 'att-a', text: 'ping', when: 2, failed: false }]) };
  const r2 = CW.ackPendingOwnerTurns(st2, 'att-a', [{ role: 'user', text: 'ping' }]);
  assert.strictEqual(r2.acked.length, 1, 'one sealed line consumes one pending item');
  assert.strictEqual(r2.state.items.length, 1);
});

test('T371 parity: main and sidebar agent ids obey the identical contract', function () {
  // 🎯T372 doctrine: this must be ONE contract, so the same helpers are driven
  // with the overseer's agent id and an aside's and must behave identically.
  ['jevons', 'att-msln9k27-nf4y87'].forEach(function (agent) {
    let st = CW.stagePendingOwnerTurn(CW.emptyPending(), agent, 'owner turn', { now: 7 });

    // 1. visible immediately, before any server frame
    let lines = CW.applyPendingOwnerTurns([], st, agent);
    assert.strictEqual(lines.length, 1, agent + ': owner bubble present before the server answers');

    // 2. survives a hydrate/replay that omits it
    lines = CW.applyPendingOwnerTurns([{ role: 'assistant', text: 'unrelated replay' }], st, agent);
    assert.strictEqual(lines.filter((l) => l.role === 'user' && l.text === 'owner turn').length, 1,
      agent + ': owner bubble survives a history frame that omits it');

    // 3. survives selection churn — another agent's frame never consumes it
    const other = CW.ackPendingOwnerTurns(st, 'someone-else', [{ role: 'user', text: 'owner turn' }]);
    assert.strictEqual(CW.pendingOwnerTurnsFor(other.state, agent).length, 1,
      agent + ': another pane cannot ack this pane pending turn');

    // 4. a failed send keeps the bubble (marked failed), never a silent vanish
    const failed = CW.markPendingOwnerTurnFailed(st, st.items[0].id);
    assert.strictEqual(failed.items[0].failed, true);
    assert.strictEqual(CW.applyPendingOwnerTurns([], failed, agent).length, 1,
      agent + ': failed send still shows the owner bubble');
  });
});

test('T371 send paints the owner bubble on accept, not on HTTP 200', function () {
  const dom = fakeDom('compact');
  const d = deferred();
  const events = [];
  let pending = CW.emptyPending();

  const ctl = CW.mount(dom.host, {
    document: dom.doc,
    density: 'compact',
    agentId: 'att-a',
    wireComposer: false,
    onStagePending: function (name, text) {
      pending = CW.stagePendingOwnerTurn(pending, name, text);
      events.push('stage');
      return CW.pendingOwnerTurnsFor(pending, name).slice(-1)[0];
    },
    onAfterOptimistic: function (opt) {
      events.push('optimistic:' + opt.lines.map((l) => l.role + '=' + l.text).join(','));
    },
    onSend: function () { events.push('send'); return d.promise; },
    onSendAccepted: function () { events.push('accepted'); },
  });
  assert.ok(ctl, 'widget mounts against the injected document');

  ctl.setDraft('does this send?');
  const p = ctl.send();

  // The server has NOT answered yet — this is exactly the window in which the
  // owner's message used to be invisible (and stealable by an attention switch).
  assert.deepStrictEqual(
    events,
    ['stage', 'optimistic:user=does this send?', 'send'],
    'stage + optimistic paint happen before the transport is even called',
  );
  assert.strictEqual(CW.pendingOwnerTurnsFor(pending, 'att-a').length, 1);
  assert.strictEqual(ctl.getDraft(), '', 'composer cleared on accept');
  assert.strictEqual(ctl.getLines().length, 1, 'owner line already in the model');

  d.resolve({ status: 'sent' });
  return p.then(function () {
    assert.ok(events.indexOf('accepted') >= 0, 'acceptance still reported after the 200');
    assert.strictEqual(ctl.getLines().length, 1, 'no duplicate bubble after the server answers');
  });
});

test('T371 a failed send keeps the bubble and reports loudly (no vanish)', function () {
  const dom = fakeDom('compact');
  const d = deferred();
  let pending = CW.emptyPending();
  let failedWith = null;
  let errored = null;

  const ctl = CW.mount(dom.host, {
    document: dom.doc,
    density: 'compact',
    agentId: 'att-a',
    wireComposer: false,
    onStagePending: function (name, text) {
      pending = CW.stagePendingOwnerTurn(pending, name, text);
      return CW.pendingOwnerTurnsFor(pending, name).slice(-1)[0];
    },
    onAfterOptimistic: function () {},
    onSend: function () { return d.promise; },
    onSendFailed: function (name, staged, err) {
      failedWith = { name: name, staged: staged, err: err };
      pending = CW.markPendingOwnerTurnFailed(pending, staged && staged.id);
    },
    onSendError: function (err) { errored = err; },
  });

  ctl.setDraft('this will fail');
  const p = ctl.send();
  d.reject(new Error('agent "att-a" is not registered'));

  return p.then(function () {
    assert.ok(errored, 'send failure is surfaced (T275 loud)');
    assert.ok(failedWith && failedWith.staged, 'failed send names the staged turn');
    assert.strictEqual(failedWith.name, 'att-a');
    const items = CW.pendingOwnerTurnsFor(pending, 'att-a');
    assert.strictEqual(items.length, 1, 'the owner turn is still staged after a failed send');
    assert.strictEqual(items[0].failed, true);
    assert.strictEqual(ctl.getLines().length, 1, 'and its bubble is still on screen');
  });
});

test('T371 index.html: every inspect line replace reconciles pending turns', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function reconcileInspectPending') >= 0,
    'host defines the pending reconcile seam');

  // The wire handler is where the wholesale replace lives — it must reconcile.
  const wire = html.match(/function handleAgentTranscriptWire\([\s\S]*?\n}/);
  assert.ok(wire, 'handleAgentTranscriptWire present');
  const reconciles = (wire[0].match(/reconcileInspectPending\(/g) || []).length;
  assert.ok(reconciles >= 2,
    'both the live and history branches reconcile pending owner turns (found ' + reconciles + ')');

  // Staging is wired into the mount, not reimplemented host-side (🎯T372).
  assert.ok(html.indexOf('ConversationWidget.stagePendingOwnerTurn') >= 0,
    'staging uses the shared widget helper, not a sidebar-local stack');
  assert.ok(html.indexOf('onStagePending') >= 0, 'mount wires onStagePending');
});

Promise.all(asyncTests).then(function () {
  console.log('PASS conversation_widget_test (' + passed + ' tests)');
});
