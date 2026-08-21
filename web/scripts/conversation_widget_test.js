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
  assert.strictEqual(CW.buildSendRequest('jevons', 'hi').ok, true);
  assert.strictEqual(CW.buildSendRequest('jevons', 'hi').url, '/api/agents/jevons/send');
  assert.strictEqual(CW.agentSendPath('po'), '/api/agents/po/send');
});

test('T309.1 sendBlockMessage is loud for every block reason', function () {
  assert.ok(CW.sendBlockMessage('no-selection').indexOf('selected') >= 0);

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
    appendChild: function (c) { this.children.push(c); if (c) c.parentNode = this; return c; },
    removeChild: function (c) { this.children = this.children.filter((x) => x !== c); return c; },
    remove: function () {
      if (this.parentNode && this.parentNode.removeChild) this.parentNode.removeChild(this);
    },
    after: function (n) {
      if (!this.parentNode) return;
      const kids = this.parentNode.children || [];
      const i = kids.indexOf(this);
      if (i < 0) this.parentNode.appendChild(n);
      else {
        kids.splice(i + 1, 0, n);
        if (n) n.parentNode = this.parentNode;
      }
    },
    setAttribute: function (k, v) { this['attr:' + k] = v; },
    querySelector: function (sel) {
      const kids = this.children || [];
      if (sel === '.working-indicator') {
        return kids.find(function (c) { return c.className && String(c.className).indexOf('working-indicator') >= 0; }) || null;
      }
      return null;
    },
    querySelectorAll: function (sel) {
      const kids = this.children || [];
      if (sel === '.msg.jevons') {
        return kids.filter(function (c) {
          return c.classList && c.classList.contains('jevons');
        });
      }
      if (sel === '.msg') {
        return kids.filter(function (c) {
          return c.classList && c.classList.contains('msg');
        });
      }
      return [];
    },
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

test('T371 index.html: inspect hydrate is applyWireEvent, not a history blob', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function handleNamedConversation') >= 0);
  assert.ok(html.indexOf('function handleConversationReset') >= 0);
  assert.ok(html.indexOf('handleNamedConversation') >= 0 && html.indexOf('applyWireEvent') >= 0,
    'named frames grow via the widget');
  assert.ok(html.indexOf('conversation_reset') >= 0);
  assert.ok(html.indexOf('ConversationWidget.stagePendingOwnerTurn') >= 0,
    'staging uses the shared widget helper, not a sidebar-local stack');
  assert.ok(html.indexOf('onStagePending') >= 0, 'mount wires onStagePending');
});

// ── 🎯T372: one widget, one grow, one send ──────────────────────────

function grokWordChunks() {
  return ['Plan', ' remaining', ' is', ' on', ' the', ' header', ' bar'];
}

function toolTape() {
  return [
    { type: 'user', message: { content: 'do the thing' }, timestamp: 1 },
    {
      type: 'assistant',
      stream_id: 'sid-tools',
      message: { content: [{ type: 'tool_use', name: 'Read', input: { path: 'x' } }] },
    },
    {
      type: 'assistant',
      stream_id: 'sid-tools',
      message: { content: [{ type: 'text', text: 'done' }], stop_reason: 'end_turn' },
    },
  ];
}

test('one apply: jevons and another agent emit the same row kinds including turn-slot', function () {
  const tape = toolTape();
  function run(agentId, density) {
    const stream = CW.createStreamJoin({});
    tape.forEach(function (ev) { stream.applyWireEvent(ev); });
    return stream.getLines();
  }
  const root = run('jevons', 'comfortable');
  const other = run('jevons-po', 'compact');
  function kinds(lines) {
    return lines.map(function (l) {
      return l.kind || l.role;
    });
  }
  assert.deepStrictEqual(kinds(root), kinds(other));
  const slots = root.filter(function (l) { return l.kind === 'turn-slot'; });
  assert.strictEqual(slots.length, 1, 'one turn-slot from tool_use');
  assert.ok(slots[0].items && slots[0].items.length >= 1, 'slot has tool items');
  assert.ok(/step/.test(slots[0].text || CW.turnSlotLabel(slots[0].items)), '⋯ n steps label');
  const users = root.filter(function (l) { return l.role === 'user'; });
  const asst = root.filter(function (l) { return l.role === 'assistant' || l.role === 'jevons'; });
  assert.strictEqual(users.length, 1);
  assert.strictEqual(asst.length, 1);
  assert.strictEqual(asst[0].text, 'done');
});

test('turn-slot coalesces tools across tools-only end_turn (not one strip per tool)', function () {
  const stream = CW.createStreamJoin({});
  const tape = [
    { type: 'user', message: { content: 'investigate' }, timestamp: 1 },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
    { type: 'assistant', message: { content: [], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash' }] } },
    { type: 'assistant', message: { content: [], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Grep' }] } },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'report' }], stop_reason: 'end_turn' },
    },
  ];
  tape.forEach(function (ev) { stream.applyWireEvent(ev); });
  const slots = stream.getLines().filter(function (l) { return l.kind === 'turn-slot'; });
  assert.strictEqual(slots.length, 1, 'one strip per owner turn, got ' + slots.length);
  assert.strictEqual(slots[0].items.length, 3, 'all three tools in that strip');
  assert.strictEqual(slots[0].text, '⋯ 3 steps');
  const kinds = stream.getLines().map(function (l) { return l.kind || l.role; });
  assert.deepStrictEqual(kinds, ['user', 'turn-slot', 'assistant']);
});

test('displayFromEvents ignores lossless recorded envelopes', function () {
  const lines = CW.displayFromEvents([
    { type: 'user', message: { content: 'hi' } },
    { type: 'progress', recorded: 'lossless', progress_type: 'tool_use', raw: { sessionUpdate: 'tool_call_update' } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'yo' }] } },
  ]);
  assert.deepStrictEqual(lines.map(function (l) { return l.kind || l.role; }), ['user', 'assistant']);
});

test('T119.5 incremental fold equals full displayFromEvents replay', function () {
  const tape = [
    { type: 'user', message: { content: 'go' }, timestamp: 1 },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
    { type: 'assistant', message: { content: [], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Grep' }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'later' }] } },
  ];
  const fold = CW.newDisplayFold();
  for (let i = 0; i < tape.length; i++) {
    CW.foldDisplayEvent(fold, tape[i]);
    const full = CW.displayFromEvents(tape.slice(0, i + 1));
    assert.deepStrictEqual(
      fold.out.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      full.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      'fold prefix ' + i
    );
  }
});

test('displayFromEvents is f(raw): 1 step is already ⋯ 1 step; consecutive tools coalesce', function () {
  const CE = require('./chat_events.js');
  const tape = [
    { type: 'user', message: { content: 'go' }, timestamp: 1 },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
    { type: 'assistant', message: { content: [], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash' }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'ok' }], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Grep' }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'later' }] } },
  ];
  const full = CW.displayFromEvents(tape);
  let acc = [];
  for (let i = 0; i < tape.length; i++) {
    acc.push(tape[i]);
    const folded = CW.displayFromEvents(acc);
    const prefix = CW.displayFromEvents(tape.slice(0, i + 1));
    assert.deepStrictEqual(
      folded.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      prefix.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      'fold prefix ' + i,
    );
  }
  const kinds = full.map(function (l) { return l.kind || l.role; });
  assert.deepStrictEqual(kinds, ['user', 'turn-slot', 'assistant', 'turn-slot', 'assistant']);
  assert.strictEqual(full[1].text, '⋯ 2 steps');
  assert.strictEqual(full[3].text, '⋯ 1 step');
  const one = CW.displayFromEvents([
    { type: 'user', message: { content: 'x' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
  ]);
  assert.strictEqual(one[1].text, '⋯ 1 step');
});

test('turn-slot after a response is a new strip (chronology)', function () {
  const stream = CW.createStreamJoin({});
  [
    { type: 'user', message: { content: 'go' }, timestamp: 1 },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'first' }], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash' }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'second' }], stop_reason: 'end_turn' } },
  ].forEach(function (ev) { stream.applyWireEvent(ev); });
  const kinds = stream.getLines().map(function (l) { return l.kind || l.role; });
  assert.deepStrictEqual(kinds, ['user', 'assistant', 'turn-slot', 'assistant']);
  const slot = stream.getLines().filter(function (l) { return l.kind === 'turn-slot'; })[0];
  assert.strictEqual(slot.items.length, 2);
  assert.strictEqual(slot.text, '⋯ 2 steps');
});

test('appendUser prefers addMsg over buildMsg+messagesEl (canvas vs leftover stack)', function () {
  const added = [];
  const stream = CW.createStreamJoin({
    addMsg: function (role, text) {
      added.push({ role: role, text: text, via: 'addMsg' });
      return { className: 'msg ' + role };
    },
    buildMsg: function (role, text) {
      added.push({ role: role, text: text, via: 'buildMsg' });
      return { className: 'msg ' + role };
    },
    messagesEl: { appendChild: function () { added.push({ via: 'append' }); } },
  });
  stream.appendUser('hello owner', 1, { origin: 'owner' });
  assert.strictEqual(added.length, 1);
  assert.strictEqual(added[0].via, 'addMsg');
  assert.strictEqual(added[0].role, 'user');
});

// 🎯T491: fold.out is mutated in place. applyWireEvent must snapshot the
// previous display before fold, or syncDisplay sees equal lengths and
// paints only the first user (J19: 16 seeded turns → 1 leftover bubble).
test('T491 applyWireEvent paints every distinct user+assistant, not only the first', function () {
  const painted = [];
  const stream = CW.createStreamJoin({
    addMsg: function (role, text) {
      painted.push({ role: role, text: String(text || '') });
      return { className: 'msg ' + role, _streamRaw: text };
    },
  });
  const n = 8;
  for (let i = 0; i < n; i++) {
    const tok = 'ROOThist-' + String(i).padStart(2, '0');
    stream.applyWireEvent({
      type: 'user',
      timestamp: i * 2,
      message: { role: 'user', content: tok + ' distinctive owner turn ' + i },
    });
    stream.applyWireEvent({
      type: 'assistant',
      timestamp: i * 2 + 1,
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: 'ack ' + tok }],
        stop_reason: 'end_turn',
      },
    });
  }
  const users = painted.filter(function (p) { return p.role === 'user'; });
  const asst = painted.filter(function (p) { return p.role === 'jevons' || p.role === 'assistant'; });
  assert.strictEqual(users.length, n, 'painted users=' + users.length + ' want ' + n +
    ' (aliasing prev=fold.out paints only the first)');
  assert.strictEqual(asst.length, n, 'painted assistants=' + asst.length + ' want ' + n);
  assert.strictEqual(users[0].text.indexOf('ROOThist-00'), 0);
  assert.ok(users[n - 1].text.indexOf('ROOThist-07') === 0, users[n - 1] && users[n - 1].text);
  const lines = stream.getLines();
  assert.strictEqual(lines.filter(function (l) { return l.role === 'user'; }).length, n);
});

// 🎯T496: a tool_use stop is not terminal, so the pre-tool assistant row
// keeps _stream and the post-tool final text grows THAT row — which is no
// longer the last display row (the turn-slot is). syncDisplay's equal-length
// branch used to inspect only the last row and returned without painting,
// so the overseer's final answer never reached the owner-visible bubble.
test('T496 final text after tool_use paints into the owner-visible bubble', function () {
  const bubbles = [];
  const stream = CW.createStreamJoin({
    addMsg: function (role, text) {
      const el = { className: 'msg ' + role, _streamRaw: String(text || '') };
      bubbles.push(el);
      return el;
    },
    requestAnimationFrame: function (fn) { fn(); return 0; },
  });
  const sid = 'sid-t496';
  stream.applyWireEvent({ type: 'user', message: { role: 'user', content: 'how is the fleet?' } });
  stream.applyWireEvent({
    type: 'assistant', stream_id: sid,
    message: { content: [{ type: 'text', text: 'Checking the fleet' }] },
  });
  stream.applyWireEvent({
    type: 'assistant', stream_id: sid,
    message: { content: [{ type: 'text', text: ' now.' }] },
  });
  stream.applyWireEvent({
    type: 'assistant', stream_id: sid,
    message: { content: [{ type: 'tool_use', name: 'jevons_agent_list', input: {} }], stop_reason: 'tool_use' },
  });
  stream.applyWireEvent({
    type: 'tool_result',
    message: { content: [{ type: 'tool_result', content: 'ok' }] },
  });
  stream.applyWireEvent({
    type: 'assistant', stream_id: sid,
    message: { content: [{ type: 'text', text: 'FINAL-T496 the fleet is healthy.' }] },
  });
  stream.applyWireEvent({
    type: 'assistant', stream_id: sid,
    message: { content: [], stop_reason: 'end_turn' },
  });
  // Display model has the final text (fold is correct)…
  const asst = stream.getLines().filter(function (l) {
    return l && (l.role === 'assistant' || l.role === 'jevons');
  });
  assert.strictEqual(asst.length, 1, 'one assistant line, got ' + asst.length);
  assert.ok(asst[0].text.indexOf('FINAL-T496') >= 0, 'fold has final text: ' + asst[0].text);
  // …and so does the painted bubble (the owner-visible half — the bug).
  const jb = bubbles.filter(function (b) { return b.className.indexOf('jevons') >= 0; });
  assert.strictEqual(jb.length, 1, 'one jevons bubble, got ' + jb.length);
  assert.ok(jb[0]._streamRaw.indexOf('FINAL-T496') >= 0,
    'bubble carries post-tool final text, got: ' + jb[0]._streamRaw);
});

// 🎯T504: owner user is a stream barrier. Same stream_id after the owner
// bubble must mint a NEW assistant below, never grow the pre-user card.
test('T504 owner user is a stream barrier: post-user text is a new bubble below', function () {
  const leftover = 'Expected leftover mail…';
  const owner = 'why did jevons PO start the workers as Fable.';
  const reply = 'Checking how those workers were minted…';
  const sid = 'sid-t504-fable';
  const tape = [
    { type: 'assistant', stream_id: sid, message: { content: [{ type: 'text', text: leftover }] } },
    { type: 'user', message: { content: owner } },
    { type: 'assistant', stream_id: sid, message: { content: [{ type: 'text', text: reply }] } },
  ];
  const bubbles = [];
  const stream = CW.createStreamJoin({
    addMsg: function (role, text) {
      const el = { className: 'msg ' + role, _streamRaw: String(text || '') };
      bubbles.push(el);
      return el;
    },
    requestAnimationFrame: function (fn) { fn(); return 0; },
  });
  tape.forEach(function (ev) { stream.applyWireEvent(ev); });
  const lines = stream.getLines();
  const kinds = lines.map(function (l) { return l.kind || l.role; });
  assert.deepStrictEqual(kinds, ['assistant', 'user', 'assistant']);
  assert.strictEqual(lines[0].text, leftover, 'first bubble stays the leftover');
  assert.ok(String(lines[0].text).indexOf(reply) < 0,
    'post-user sentence is not in the first bubble: ' + lines[0].text);
  assert.ok(!lines[0]._stream, 'owner user seals the pre-user stream');
  assert.strictEqual(lines[1].text, owner);
  assert.strictEqual(lines[2].text, reply);
  const jb = bubbles.filter(function (b) { return b.className.indexOf('jevons') >= 0; });
  const ub = bubbles.filter(function (b) { return b.className.indexOf('user') >= 0; });
  assert.strictEqual(ub.length, 1, 'one owner bubble');
  assert.strictEqual(jb.length, 2, 'two jevons bubbles (below-user is new), got ' + jb.length);
  assert.ok(String(jb[0]._streamRaw).indexOf(reply) < 0,
    'painted leftover must not include the reply: ' + jb[0]._streamRaw);
  assert.ok(String(jb[1]._streamRaw).indexOf(reply) >= 0,
    'reply paints in the second jevons bubble: ' + jb[1]._streamRaw);
  // Incremental fold equals displayFromEvents of the same tape (reload).
  const replayed = CW.displayFromEvents(tape);
  assert.deepStrictEqual(
    lines.map(function (l) { return { k: l.kind || l.role, t: l.text, s: !!l._stream }; }),
    replayed.map(function (l) { return { k: l.kind || l.role, t: l.text, s: !!l._stream }; }),
    'reload of the interleaved tape agrees with live',
  );
});

test('T504 control: no owner user, same stream_id + mid-stream tool is one bubble (T496)', function () {
  const sid = 'sid-t504-control';
  const tape = [
    { type: 'assistant', stream_id: sid, message: { content: [{ type: 'text', text: 'Checking the fleet' }] } },
    {
      type: 'assistant', stream_id: sid,
      message: { content: [{ type: 'tool_use', name: 'jevons_agent_list' }], stop_reason: 'tool_use' },
    },
    { type: 'tool_result', message: { content: [{ type: 'tool_result', content: 'ok' }] } },
    { type: 'assistant', stream_id: sid, message: { content: [{ type: 'text', text: 'FINAL-T504 the fleet is healthy.' }] } },
  ];
  const fold = CW.displayFromEvents(tape);
  const asst = fold.filter(function (l) { return l && (l.role === 'assistant' || l.role === 'jevons'); });
  assert.strictEqual(asst.length, 1, 'control is one assistant, got ' + asst.length);
  assert.ok(asst[0].text.indexOf('Checking the fleet') >= 0);
  assert.ok(asst[0].text.indexOf('FINAL-T504') >= 0, 'post-tool text grew the pre-tool bubble');
  const kinds = fold.map(function (l) { return l.kind || l.role; });
  assert.deepStrictEqual(kinds, ['assistant', 'turn-slot']);
  const live = CW.newDisplayFold();
  for (let i = 0; i < tape.length; i++) {
    CW.foldDisplayEvent(live, tape[i]);
    const full = CW.displayFromEvents(tape.slice(0, i + 1));
    assert.deepStrictEqual(
      live.out.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      full.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      'T504 control fold prefix ' + i,
    );
  }
});

test('T504 T329 inject is not a barrier and must not seal', function () {
  const sid = 'sid-t504-inject';
  const tape = [
    { type: 'assistant', stream_id: sid, message: { content: [{ type: 'text', text: 'I will read the file.' }] } },
    {
      type: 'user',
      message: {
        content: '<system-reminder>\nBackground task "call-1" completed (exit code: 0).\n</system-reminder>',
      },
    },
    { type: 'assistant', stream_id: sid, message: { content: [{ type: 'text', text: ' Then edit it.' }] } },
  ];
  const lines = CW.displayFromEvents(tape);
  const asst = lines.filter(function (l) { return l && (l.role === 'assistant' || l.role === 'jevons'); });
  assert.strictEqual(asst.length, 1, 'inject must not split, got ' + asst.length + ': ' +
    JSON.stringify(lines.map(function (l) { return l.kind || l.role; })));
  assert.ok(asst[0].text.indexOf('I will read the file.') >= 0);
  assert.ok(asst[0].text.indexOf('Then edit it.') >= 0);
  assert.ok(asst[0]._stream, 'T329 inject must not seal the open stream');
  const users = lines.filter(function (l) { return l && l.role === 'user'; });
  assert.strictEqual(users.length, 1);
  const live = CW.createStreamJoin({});
  tape.forEach(function (ev) { live.applyWireEvent(ev); });
  const liveAsst = live.getLines().filter(function (l) {
    return l && (l.role === 'assistant' || l.role === 'jevons');
  });
  assert.strictEqual(liveAsst.length, 1);
  assert.ok(liveAsst[0]._stream, 'live inject must not seal');
});

test('T504 sealed visible assistant is a barrier for an older open stream', function () {
  const tape = [
    { type: 'assistant', stream_id: 's1', message: { content: [{ type: 'text', text: 'older still streaming' }] } },
    {
      type: 'assistant', stream_id: 's2',
      message: { content: [{ type: 'text', text: 'later turn' }], stop_reason: 'end_turn' },
    },
    { type: 'assistant', stream_id: 's1', message: { content: [{ type: 'text', text: ' more older' }] } },
  ];
  const lines = CW.displayFromEvents(tape);
  const asst = lines.filter(function (l) { return l && (l.role === 'assistant' || l.role === 'jevons'); });
  assert.strictEqual(asst.length, 3, 'must not grow s1 past sealed s2, got ' + asst.length +
    ' ' + JSON.stringify(asst.map(function (l) { return l.text; })));
  assert.strictEqual(asst[0].text, 'older still streaming');
  assert.strictEqual(asst[1].text, 'later turn');
  assert.strictEqual(asst[2].text, ' more older');
});

test('T504 leftover appendUser seals so appendAssistant mints below', function () {
  const leftover = 'Expected leftover mail…';
  const owner = 'why did jevons PO start the workers as Fable.';
  const reply = 'Checking how those workers were minted…';
  const bubbles = [];
  const stream = CW.createStreamJoin({
    addMsg: function (role, text) {
      const el = { className: 'msg ' + role, _streamRaw: String(text || '') };
      bubbles.push(el);
      return el;
    },
    requestAnimationFrame: function (fn) { fn(); return 0; },
  });
  stream.appendAssistant(leftover, 1, { streamId: 'sid-t504-join' });
  stream.appendUser(owner, 2);
  stream.appendAssistant(reply, 3, { streamId: 'sid-t504-join' });
  const lines = stream.getLines();
  assert.deepStrictEqual(lines.map(function (l) { return l.kind || l.role; }),
    ['assistant', 'user', 'assistant']);
  assert.strictEqual(lines[0].text, leftover);
  assert.ok(String(lines[0].text).indexOf(reply) < 0);
  assert.strictEqual(lines[2].text, reply);
  const jb = bubbles.filter(function (b) { return b.className.indexOf('jevons') >= 0; });
  assert.strictEqual(jb.length, 2, 'leftover join mints a new bubble below the user, got ' + jb.length);
  assert.ok(String(jb[0]._streamRaw).indexOf(reply) < 0);
  assert.ok(String(jb[1]._streamRaw).indexOf(reply) >= 0);
});

test('T372 Grok word-chunks are one assistant bubble (both densities)', function () {
  ['compact', 'comfortable'].forEach(function (density) {
    const dom = fakeDom(density);
    const ctl = CW.mount(dom.host, {
      document: dom.doc,
      density: density,
      agentId: density === 'compact' ? 'jv-t390-plan-usage' : 'jevons',
    });
    grokWordChunks().forEach(function (word, i) {
      ctl.applyWireEvent({
        type: 'assistant',
        stream_id: 'sid-t372',
        message: { content: [{ type: 'text', text: word }] },
      });
    });
    ctl.applyWireEvent({
      type: 'assistant',
      stream_id: 'sid-t372',
      message: { content: [], stop_reason: 'end_turn' },
    });
    const asst = ctl.getLines().filter(function (l) {
      return l && (l.role === 'assistant' || l.role === 'jevons');
    });
    assert.strictEqual(asst.length, 1, density + ': one assistant line, got ' + asst.length);
    assert.strictEqual(asst[0].text, grokWordChunks().join(''));
    assert.ok(!asst[0]._stream, density + ': terminal seals');
    const bubbles = (dom.byId[ctl.ids.messages] && dom.byId[ctl.ids.messages].children) || [];
    const jevons = bubbles.filter(function (c) {
      return c.classList && c.classList.contains('jevons');
    });
    assert.strictEqual(jevons.length, 1, density + ': one DOM bubble');
  });
});

test('T372 createStreamJoin is the one grow implementation', function () {
  assert.strictEqual(typeof CW.createStreamJoin, 'function');
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  assert.ok(/function createStreamJoin\(/.test(src), 'join body lives in the widget');
  assert.ok(/function appendAssistant\(/.test(src), 'widget grows via appendAssistant');
});

test('T372 index.html: wireComposer:false is gone', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(!/wireComposer\s*:\s*false/.test(html),
    'main must not opt out of the widget composer');
  assert.ok(!/opts\.wireComposer/.test(fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8')),
    'widget must not honour a wireComposer escape hatch');
});

test('T372 index.html: no second grow-bubble implementation', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(!/function resolveOpenStreamEl\(/.test(html),
    'resolveOpenStreamEl join body must not remain in index.html');
  assert.ok(!/\blet openStreamEl\b/.test(html),
    'openStreamEl must live in the widget, not as a second handle in index.html');
  assert.ok(!/const openStreamById/.test(html),
    'openStreamById map must not be a second join in index.html');
  const append = html.match(/function appendOrAddJevons\([\s\S]*?\n\}/);
  assert.ok(append, 'appendOrAddJevons remains as a named delegate');
  assert.ok(append[0].indexOf('appendAssistant') >= 0,
    'appendOrAddJevons must delegate to the widget');
  assert.ok(append[0].indexOf('_streamRaw') < 0,
    'appendOrAddJevons must not merge _streamRaw itself');
  const live = html.match(/function handleNamedConversation\([\s\S]*?\n\}/);
  assert.ok(live, 'named conversation ingest present');
  assert.ok(live[0].indexOf('applyWireEvent') >= 0, 'live path calls widget.applyWireEvent');
  const mainLive = html.match(/if \(typ === 'assistant'\) \{[\s\S]*?\n    return;\n  \}/);
  assert.ok(html.indexOf('mainConversation.applyWireEvent') >= 0,
    'main live ingest uses the same apply, not a second tool branch');
  assert.ok(!/if \(c\.type==='tool_use'\) \{[\s\S]{0,200}addTurnItem\('tool-use'/.test(html),
    'main must not addTurnItem from a host tool_use walk');
  assert.ok(live[0].indexOf('copyInspectLines') < 0 && live[0].indexOf('inspectLinesCopy') < 0,
    'live path must not copy lines (drops _stream — T479)');
  assert.ok(live[0].indexOf('renderAgentInspect') < 0,
    'live path must not remount via renderAgentInspect');
});

test('applyWireEvent pins to end when scrollFollow is tracking', function () {
  let pinned = 0;
  const follow = {
    tracking: true,
    shouldPin: function () { return this.tracking; },
    applyAfterUpdate: function () { pinned++; },
  };
  const stream = CW.createStreamJoin({ scrollFollow: follow });
  stream.applyWireEvent({ type: 'user', message: { content: 'hello from the bottom' } });
  assert.ok(pinned >= 1, 'tracking ingest pins after each wire event');
  follow.tracking = false;
  const before = pinned;
  stream.applyWireEvent({ type: 'user', message: { content: 'should not jump' } });
  assert.strictEqual(pinned, before, 'free mode does not pin');
});

test('inspect hydrate reset then applyWireEvent is the one ingest', function () {
  const stream = CW.createStreamJoin({});
  stream.applyWireEvent({ type: 'user', message: { content: 'old' } });
  assert.ok(stream.getLines().length >= 1);
  stream.reset();
  assert.strictEqual(stream.getLines().length, 0);
  stream.applyWireEvent({ type: 'user', message: { content: 'new' } });
  const lines = stream.getLines();
  assert.strictEqual(lines.filter(function (l) { return l.role === 'user'; }).length, 1);
  assert.ok(String(lines[0].text || '').indexOf('new') >= 0);
});

test('T494.1 agent_note + system pairs coalesce to one labelled slot', function () {
  const stream = CW.createStreamJoin({});
  stream.applyWireEvent({ type: 'user', message: { content: 'go' } });
  stream.applyWireEvent({
    type: 'assistant',
    message: { content: [{ type: 'text', text: 'ack' }], stop_reason: 'end_turn' },
  });
  for (let i = 0; i < 20; i++) {
    stream.applyWireEvent({ type: 'agent_note', text: '[Agent pad responded] slot ' + i });
    stream.applyWireEvent({ type: 'system' });
  }
  const lines = stream.getLines();
  const slots = lines.filter(function (l) { return l.kind === 'turn-slot'; });
  assert.strictEqual(slots.length, 1, 'system must not open a new slot, got ' + slots.length);
  assert.strictEqual(slots[0].items.length, 20);
  assert.strictEqual(slots[0].text, '⋯ 20 steps');
});

test('T494.1 tool_use + notes between owner turns are one labelled slot', function () {
  // Host-shaped: virtualizeMessages is a no-op during replay, so open
  // must attach itself (openFoldTurnSlot → attachTranscriptRow). Each
  // note/tool that waited on virtualize minted an orphan empty row.
  const rows = [];
  function attach(slot) {
    slot.el = {
      isConnected: true,
      _vIndex: rows.length,
      _label: { textContent: '' },
      _items: {
        children: [],
        appendChild: function (d) { this.children.push(d); },
      },
    };
    rows.push({ kind: 'turn-slot', text: slot.text || '', el: slot.el });
  }
  const stream = CW.createStreamJoin({
    onTurnSlotOpen: attach,
    onTurnSlotItem: function (slot, cls, text) {
      slot.el._items.appendChild({ cls: cls, text: text });
      const n = slot.el._items.children.length;
      slot.el._label.textContent = '⋯ ' + n + (n === 1 ? ' step' : ' steps');
      rows[slot.el._vIndex].text = slot.el._label.textContent;
    },
  });
  stream.applyWireEvent({ type: 'user', message: { content: 'go' } });
  stream.applyWireEvent({
    type: 'assistant',
    message: { content: [{ type: 'text', text: 'ack' }], stop_reason: 'end_turn' },
  });
  for (let i = 0; i < 12; i++) {
    if (i % 3 === 0) {
      stream.applyWireEvent({
        type: 'assistant',
        message: { content: [{ type: 'tool_use', name: 'Read' }] },
      });
    }
    stream.applyWireEvent({ type: 'agent_note', text: '[Agent pad responded] mix ' + i });
    stream.applyWireEvent({ type: 'system' });
  }
  stream.applyWireEvent({ type: 'user', message: { content: 'next' } });
  const slots = rows.filter(function (r) { return r.kind === 'turn-slot'; });
  const empty = slots.filter(function (s) { return !String(s.text || '').trim(); });
  assert.strictEqual(slots.length, 1, 'mix must be one slot, got ' + slots.length);
  assert.strictEqual(empty.length, 0, 'no unlabelled desert rows');
  assert.ok(slots[0].text.indexOf('⋯') === 0, 'labelled ⋯ n steps, got ' + slots[0].text);
  assert.strictEqual(slots[0].el._items.children.length, 16, '4 tools + 12 notes');
});

test('T119.6 ensureTurnSlot twice leaves one canvas child', function () {
  const CW = require('./conversation_widget.js');
  const canvas = { children: [] };
  let slot = CW.ensureTurnSlot(canvas, null);
  slot = CW.ensureTurnSlot(canvas, slot);
  slot = CW.ensureTurnSlot(canvas, slot);
  assert.strictEqual(canvas.children.length, 1, 'second ensure must not append');
  assert.strictEqual(slot, canvas.children[0]);
  assert.strictEqual(CW.shouldMintTurnSlot(slot, true), false);
  assert.strictEqual(CW.shouldMintTurnSlot(null, false), true);
  // Mutation: always-create.
  function alwaysMint(canvas) {
    const el = { id: canvas.children.length };
    canvas.children.push(el);
    return el;
  }
  const mutant = { children: [] };
  alwaysMint(mutant);
  alwaysMint(mutant);
  assert.strictEqual(mutant.children.length, 2, 'mutant must be the failure mode the oracle detects');
});

// 🎯T119.8: host paints capsules from fold slot rows — no turnDetails/turnItems.
test('T119.8 host has no parallel open-slot state; paints by slot.el', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(!/\blet turnDetails\b/.test(html), 'no turnDetails open-slot state');
  assert.ok(!/\blet turnItems\b/.test(html), 'no turnItems open-slot state');
  assert.ok(!/function startTurn\(/.test(html), 'startTurn retired — openFoldTurnSlot owns mint');
  assert.ok(!/function closeTurn\(/.test(html), 'closeTurn retired — cancelFoldTurnSlot is per-slot');
  assert.ok(!/function addTurnItem\(/.test(html), 'addTurnItem retired — paintFoldTurnSlotItem is per-slot');
  const open = html.match(/function openFoldTurnSlot\(slot\) \{[\s\S]*?\nfunction cancelFoldTurnSlot/);
  assert.ok(open, 'openFoldTurnSlot exists');
  assert.ok(/attachTranscriptRow/.test(open[0]),
    'T494.1: open attaches itself — virtualize is a no-op during replay');
  assert.ok(!/virtualizeMessages\(\)/.test(open[0]),
    'T494.1: open must not depend on virtualizeMessages to set row.el');
  assert.ok(!/createElement/.test(open[0]),
    'T119.4: open must not mint DOM — apply/attachTranscriptRow is the only mint');
  const cancel = html.match(/function cancelFoldTurnSlot\(slot\) \{[\s\S]*?\nfunction formatAgentNote/);
  assert.ok(cancel, 'cancelFoldTurnSlot exists');
  assert.ok(!/\.remove\(\)/.test(cancel[0]),
    'T119.4: cancel must not destroy nodes — detach/apply does');
  assert.ok(/removeTranscriptRow/.test(cancel[0]),
    'empty cancel deletes the unused slot from the list');
  assert.ok(/onTurnSlotItem:\s*function\s*\(\s*slot/.test(html),
    'onTurnSlotItem receives the slot');
  assert.ok(/paintFoldTurnSlotItem\(\s*slot/.test(html),
    'onTurnSlotItem routes to THAT slot via paintFoldTurnSlotItem(slot, …)');
  const opt = html.match(/function paintOptimisticMainUser\([\s\S]*?\n\}/);
  assert.ok(opt, 'paintOptimisticMainUser exists');
  assert.ok(!/openFoldTurnSlot\(/.test(opt[0]) && !/startTurn\(/.test(opt[0]),
    'optimistic user paint must not pre-open a slot that onTurnSlotOpen will open');
});

test('T119.8 tape [user, tool, text, tool, tool] → two capsules (1 then 2)', function () {
  const capsules = [];
  function mintEl() {
    const kids = [];
    const el = {
      isConnected: true,
      _label: { textContent: '' },
      _items: {
        children: kids,
        appendChild: function (d) { kids.push(d); },
      },
    };
    capsules.push(el);
    return el;
  }
  // Host-shaped hooks: always mint a NEW capsule per fold open; paint into slot.el.
  const stream = CW.createStreamJoin({
    onTurnSlotOpen: function (slot) {
      slot.el = mintEl();
    },
    onTurnSlotItem: function (slot, cls, text) {
      assert.ok(slot && slot.el, 'item routes to an opened slot.el');
      slot.el._items.appendChild({ className: 'turn-item ' + cls, textContent: text });
      const n = slot.el._items.children.length;
      slot.el._label.textContent = '⋯ ' + n + (n === 1 ? ' step' : ' steps');
    },
  });
  const tape = [
    { type: 'user', message: { content: 'go' }, timestamp: 1 },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'mid' }], stop_reason: 'end_turn' },
    },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash' }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Grep' }] } },
  ];
  tape.forEach(function (ev) { stream.applyWireEvent(ev); });
  const lines = stream.getLines();
  const kinds = lines.map(function (l) { return l.kind || l.role; });
  assert.deepStrictEqual(kinds, ['user', 'turn-slot', 'assistant', 'turn-slot']);
  assert.strictEqual(capsules.length, 2, 'two fold slots → two capsules, got ' + capsules.length);
  assert.strictEqual(capsules[0]._items.children.length, 1, 'first capsule is 1 step');
  assert.strictEqual(capsules[1]._items.children.length, 2, 'second capsule is 2 steps');
  assert.strictEqual(capsules[0]._label.textContent, '⋯ 1 step');
  assert.strictEqual(capsules[1]._label.textContent, '⋯ 2 steps');
  // Live fold and full replay agree.
  const replayed = CW.displayFromEvents(tape);
  assert.deepStrictEqual(
    lines.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
    replayed.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
  );
  // Control: consecutive tools with no visible row still coalesce.
  const control = [];
  const cstream = CW.createStreamJoin({
    onTurnSlotOpen: function (slot) { slot.el = { id: control.length }; control.push(slot.el); },
    onTurnSlotItem: function () {},
  });
  [
    { type: 'user', message: { content: 'x' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'A' }] } },
    { type: 'assistant', message: { content: [], stop_reason: 'end_turn' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'B' }] } },
  ].forEach(function (ev) { cstream.applyWireEvent(ev); });
  assert.strictEqual(control.length, 1, 'consecutive tools coalesce into one capsule');
  assert.strictEqual(cstream.getLines().filter(function (l) { return l.kind === 'turn-slot'; })[0].items.length, 2);
});

function t1199Tool(name, sid) {
  const ev = { type: 'assistant', message: { content: [{ type: 'tool_use', name: name }] } };
  if (sid) ev.stream_id = sid;
  return ev;
}
function t1199Text(text, sid, end) {
  const msg = { content: [{ type: 'text', text: text }] };
  if (end) msg.stop_reason = 'end_turn';
  const ev = { type: 'assistant', message: msg };
  if (sid) ev.stream_id = sid;
  return ev;
}
function t1199Tools(n, prefix, sid) {
  const out = [];
  for (let i = 0; i < n; i++) out.push(t1199Tool(prefix + i, sid));
  return out;
}
// Owner 2026-08-21 (screenshot 8a9b04bd47b3e6e6): ⋯ 5 on ⋯ 5, then a
// user bubble, then ⋯ 3 on ⋯ 3. Pre-tool _stream text + more tools after
// a park is the closeOpen/growAssistant adjacency.
function t1199OwnerReproTape() {
  const a = 'sid-t119.9-a';
  const b = 'sid-t119.9-b';
  return [
    { type: 'user', message: { content: 'first' }, timestamp: 1 },
    t1199Text('checking the fleet', a),
  ].concat(t1199Tools(5, 'A', a)).concat([
    t1199Text('still working', a),
  ]).concat(t1199Tools(5, 'B', a)).concat([
    { type: 'user', message: { content: 'second' }, timestamp: 2 },
    t1199Text('on it', b),
  ]).concat(t1199Tools(3, 'C', b)).concat([
    t1199Text('more', b),
  ]).concat(t1199Tools(3, 'D', b));
}
function t1199Kinds(lines) {
  return lines.map(function (l) { return l.kind || l.role; });
}
function t1199AdjacentSlots(lines) {
  const hits = [];
  for (let i = 1; i < lines.length; i++) {
    const a = lines[i - 1].kind || lines[i - 1].role;
    const b = lines[i].kind || lines[i].role;
    if (a === 'turn-slot' && b === 'turn-slot') hits.push(i);
  }
  return hits;
}

test('T119.9 park-on-prior-stream then more tools is one slot, not two adjacent', function () {
  const sid = 'sid-t119.9-park';
  const tape = [
    { type: 'user', message: { content: 'go' }, timestamp: 1 },
    t1199Text('Checking.', sid),
    t1199Tool('Read', sid),
    t1199Tool('Grep', sid),
    t1199Text('still going', sid), // parks on prior _stream; must not close the slot
    t1199Tool('Bash', sid),
    t1199Tool('Glob', sid),
  ];
  const fold = CW.newDisplayFold();
  for (let i = 0; i < tape.length; i++) {
    CW.foldDisplayEvent(fold, tape[i]);
    const full = CW.displayFromEvents(tape.slice(0, i + 1));
    assert.deepStrictEqual(
      fold.out.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      full.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
      'live fold prefix ' + i + ' must equal reload',
    );
  }
  const kinds = t1199Kinds(fold.out);
  assert.deepStrictEqual(kinds, ['user', 'assistant', 'turn-slot']);
  assert.deepStrictEqual(t1199AdjacentSlots(fold.out), []);
  assert.strictEqual(fold.out[2].items.length, 4, 'all four tools in one capsule');
  assert.strictEqual(fold.out[2].text, '⋯ 4 steps');
  assert.ok(String(fold.out[1].text).indexOf('still going') >= 0, 'parked text grew the earlier stream');
});

test('T119.9 owner repro 8a9b04bd47b3e6e6: one capsule per unbroken tool burst', function () {
  const tape = t1199OwnerReproTape();
  const capsules = [];
  const stream = CW.createStreamJoin({
    onTurnSlotOpen: function (slot) {
      slot.el = { id: capsules.length, _label: { textContent: '' }, _items: { children: [] } };
      capsules.push(slot.el);
    },
    onTurnSlotItem: function (slot, cls, text) {
      slot.el._items.children.push({ cls: cls, text: text });
      const n = slot.el._items.children.length;
      slot.el._label.textContent = '⋯ ' + n + (n === 1 ? ' step' : ' steps');
    },
  });
  tape.forEach(function (ev) { stream.applyWireEvent(ev); });
  const lines = stream.getLines();
  assert.deepStrictEqual(
    t1199Kinds(lines),
    ['user', 'assistant', 'turn-slot', 'user', 'assistant', 'turn-slot'],
  );
  assert.deepStrictEqual(t1199AdjacentSlots(lines), [], 'no two turn-slots adjacent');
  const slots = lines.filter(function (l) { return l.kind === 'turn-slot'; });
  assert.strictEqual(slots.length, 2, 'one capsule per owner turn');
  assert.strictEqual(slots[0].items.length, 10, 'first burst coalesces 5+5');
  assert.strictEqual(slots[0].text, '⋯ 10 steps');
  assert.strictEqual(slots[1].items.length, 6, 'second burst coalesces 3+3');
  assert.strictEqual(slots[1].text, '⋯ 6 steps');
  assert.strictEqual(capsules.length, 2, 'live path minted two capsules, not four');
  assert.strictEqual(capsules[0]._label.textContent, '⋯ 10 steps');
  assert.strictEqual(capsules[1]._label.textContent, '⋯ 6 steps');
  const replayed = CW.displayFromEvents(tape);
  assert.deepStrictEqual(
    lines.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
    replayed.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
    'reload of the same tape agrees with live',
  );
});

// 🎯T119.10: owner tape 8f3d3c3cd0617018 / 4c81aaf12c8a338a — thinking + tools
// + rewind/fleet-health notes. Live applyWireEvent must equal displayFromEvents.
// Counting tool_use+tool_result as two live steps and one on hydrate is RED.
function t11910OwnerTape() {
  const sid = 'sid-t119.10';
  return [
    { type: 'agent_note', text: '[Fleet health] auto-spawned jv-t390.1.2-auto' },
    {
      type: 'user',
      message: { content: 'Which target represents the work we discussed to perform regular entropy audits?' },
      timestamp: 1,
    },
    {
      type: 'agent_note',
      text: '[Conversation rewound by the owner. The record below is the surviving context.]',
    },
    t1199Text("I'll look up the entropy-audit target in the ledger and report its current state.", sid),
    t1199Tool('search_tool', sid),
    t1199Tool('search_tool', sid),
    t1199Text("Jevons MCP didn't show up in search, so I'll query it directly.", sid),
    t1199Tool('search_tool', sid),
    t1199Tool('grep', sid),
    t1199Tool('grep', sid),
    t1199Tool('grep', sid),
    t1199Tool('search_tool', sid),
    t1199Tool('search_tool', sid),
  ];
}

test('T119.10 owner tape: live applyWireEvent equals displayFromEvents; no empty-tip 1-step', function () {
  const tape = t11910OwnerTape();
  const progress = [];
  const capsules = [];
  const stream = CW.createStreamJoin({
    onTurnSlotOpen: function (slot) {
      slot.el = {
        id: capsules.length,
        _label: { textContent: '' },
        _items: { children: [], innerHTML: '' },
      };
      capsules.push(slot.el);
    },
    onTurnSlotItem: function (slot) {
      // Host-shaped: N from fold items, not a children accumulator.
      const n = (slot.items || []).length;
      slot.el._label.textContent = n ? ('⋯ ' + n + (n === 1 ? ' step' : ' steps')) : '';
      slot.el._items.children = (slot.items || []).slice();
    },
    onWorkingProgress: function (slot) {
      progress.push(CW.workingProgressFromSlot(slot));
    },
  });
  tape.forEach(function (ev) { stream.applyWireEvent(ev); });
  const lines = stream.getLines();
  const replayed = CW.displayFromEvents(tape);
  assert.deepStrictEqual(
    lines.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
    replayed.map(function (l) { return { k: l.kind || l.role, n: (l.items || []).length, t: l.text }; }),
    'live applyWireEvent must agree with a hard-reload of the same tape',
  );
  assert.deepStrictEqual(
    t1199Kinds(lines),
    ['turn-slot', 'user', 'turn-slot', 'assistant', 'turn-slot'],
  );
  assert.deepStrictEqual(t1199AdjacentSlots(lines), [], 'tools after parked thinking text are one capsule');
  const slots = lines.filter(function (l) { return l.kind === 'turn-slot'; });
  assert.strictEqual(slots.length, 3, 'two note capsules + one tools capsule, got ' + slots.length);
  assert.strictEqual(slots[0].items.length, 1, 'fleet-health note is reconstructible');
  assert.ok(String(slots[0].items[0].text || '').trim(), 'fleet-health tip is not empty');
  assert.strictEqual(slots[1].items.length, 1, 'rewind note is reconstructible');
  assert.ok(String(slots[1].items[0].text || '').trim(), 'rewind tip is not empty');
  assert.strictEqual(slots[2].items.length, 8, 'one capsule for the unbroken tool burst, not 2+6');
  assert.strictEqual(slots[2].text, '⋯ 8 steps');
  assert.strictEqual(capsules.length, 3, 'host minted one marker per fold slot');
  assert.strictEqual(capsules[2]._label.textContent, '⋯ 8 steps');
  assert.ok(progress.length, 'WorkingProgress fired from the fold');
  assert.strictEqual(progress[progress.length - 1], CW.workingProgressFromSlot(slots[2]));
  assert.ok(progress[progress.length - 1].indexOf('8 steps') === 0, progress[progress.length - 1]);
});

test('T119.10 tool_use+tool_result is one step live and on hydrate', function () {
  const sid = 'sid-t119.10-result';
  const liveTape = [
    { type: 'user', message: { content: 'go' }, timestamp: 1 },
    {
      type: 'assistant', stream_id: sid,
      message: { content: [{ type: 'tool_use', name: 'Read' }] },
    },
    { type: 'tool_result', message: { content: [{ type: 'tool_result', content: 'ok' }] } },
    {
      type: 'assistant', stream_id: sid,
      message: { content: [{ type: 'tool_use', name: 'Grep' }] },
    },
    { type: 'tool_result', message: { content: [{ type: 'tool_result', content: 'hits' }] } },
  ];
  // Hydrate journal often drops tool_result — that must not change N.
  const hydrateTape = liveTape.filter(function (ev) { return ev.type !== 'tool_result'; });
  const live = CW.createStreamJoin({});
  liveTape.forEach(function (ev) { live.applyWireEvent(ev); });
  const liveSlots = live.getLines().filter(function (l) { return l.kind === 'turn-slot'; });
  const hydra = CW.displayFromEvents(hydrateTape);
  const hydraSlots = hydra.filter(function (l) { return l.kind === 'turn-slot'; });
  assert.strictEqual(liveSlots.length, 1);
  assert.strictEqual(hydraSlots.length, 1);
  assert.strictEqual(liveSlots[0].items.length, 2, 'two tool_uses, results are not extra steps');
  assert.strictEqual(hydraSlots[0].items.length, 2, 'hydrate of tool_use-only tape is the same N');
  assert.strictEqual(liveSlots[0].text, '⋯ 2 steps');
  assert.strictEqual(hydraSlots[0].text, '⋯ 2 steps');
  assert.strictEqual(
    CW.workingProgressFromSlot(liveSlots[0]),
    CW.workingProgressFromSlot(hydraSlots[0]),
  );
});

test('T119.10 host paints N from fold items, not DOM children', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const paint = html.match(/function paintFoldTurnSlotItem\(slot, cls, text\) \{[\s\S]*?\nfunction buildMsg/);
  assert.ok(paint, 'paintFoldTurnSlotItem exists');
  assert.ok(/slot\.items/.test(paint[0]), 'reconciles from fold items');
  assert.ok(!/el\._items\.children\.length/.test(paint[0]),
    'must not label from DOM children (live 2× hydrate N)');
  assert.ok(!/updateWorkingProgress\(n \+/.test(paint[0]),
    'WorkingProgress is not a children accumulator in paintFoldTurnSlotItem');
  assert.ok(/onWorkingProgress:\s*function\s*\(\s*slot/.test(html),
    'mount wires onWorkingProgress from the fold slot');
  assert.ok(/workingProgressFromSlot\(slot\)/.test(html),
    'strip N is ConversationWidget.workingProgressFromSlot');
});

test('T372 index.html: send click is the widget, not a second composer send', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(!/sendBtn\.addEventListener\(\s*['"]click['"]\s*,\s*send\s*\)/.test(html),
    'main send button must not bind a second send()');
  assert.ok(html.indexOf('onComposerAction') >= 0,
    'main send enters the widget, then the host transport');
});

// ── 🎯T480: size-only T106 clip (one implementation, both surfaces) ──

function fakeBubble(opts) {
  opts = opts || {};
  const h = opts.fullH != null ? opts.fullH : 20;
  const body = fakeEl('body');
  body.className = 'msg-body';
  body.offsetHeight = h;
  body.scrollHeight = h;
  body.clientWidth = 280;
  body.innerHTML = opts.html != null ? opts.html : '';
  body.textContent = opts.text || '';
  const d = fakeEl('msg');
  d.className = 'msg ' + (opts.role || 'user');
  d.classList.add('msg');
  d.classList.add(opts.role || 'user');
  d._body = body;
  d._layoutRole = opts.role || 'user';
  d._layoutText = opts.text || '';
  d.clientWidth = 300;
  d.isConnected = true;
  d.ownerDocument = {
    createElement: function (tag) { return fakeEl(tag); },
  };
  d.appendChild(body);
  return d;
}

test('T480 short fixture → no tab', function () {
  const d = fakeBubble({ role: 'user', text: 'hi', fullH: 40 });
  const m = CW.layoutSizeClip(d, { fullH: 40 });
  assert.strictEqual(m.tall, false);
  assert.ok(!d.classList.contains('msg-clipped'), 'short must not clip');
  assert.ok(!d._expandBtn, 'short must not grow a pocket tab');
  assert.strictEqual(d._fullText, null);
});

test('T480 tall user → clipped + tab', function () {
  const d = fakeBubble({ role: 'user', text: 'tall user request\n'.repeat(20) });
  const m = CW.layoutSizeClip(d, { fullH: 400 });
  assert.strictEqual(m.tall, true);
  assert.ok(d.classList.contains('msg-clipped'), 'tall user must clip');
  assert.ok(d._expandBtn, 'tall user must have pocket tab');
  assert.strictEqual(d._expandBtn.className, 'msg-expand-tab');
  assert.strictEqual(d._expandBtn.tabIndex, -1);
});

test('T480 tall assistant → clipped + tab', function () {
  const d = fakeBubble({ role: 'jevons', text: '### reply\n- item\n'.repeat(12) });
  const m = CW.layoutSizeClip(d, { fullH: 400 });
  assert.strictEqual(m.tall, true);
  assert.ok(d.classList.contains('msg-clipped'), 'tall assistant must clip');
  assert.ok(d._expandBtn, 'tall assistant must have pocket tab');
});

test('T480 <user_info>…</rules> wall is tall → clipped', function () {
  const wall = '<user_info>\n' + 'Agents.md line\n'.repeat(80) + '</rules>';
  const d = fakeBubble({ role: 'user', text: wall });
  const m = CW.layoutSizeClip(d, { fullH: 2400 });
  assert.strictEqual(m.tall, true, 'harness wall is just a tall bubble');
  assert.ok(d.classList.contains('msg-clipped'));
  assert.ok(d._expandBtn);
});

test('T480 size-only: same height same clip regardless of role', function () {
  const u = CW.measureCollapse(fakeBubble({ role: 'user' }), 'user', 'x', { fullH: 400 });
  const j = CW.measureCollapse(fakeBubble({ role: 'jevons' }), 'jevons', 'x', { fullH: 400 });
  const s = CW.measureCollapse(fakeBubble({ role: 'status' }), 'status', 'x', { fullH: 40 });
  assert.strictEqual(u.tall, true);
  assert.strictEqual(j.tall, u.tall);
  assert.strictEqual(u.fullH, j.fullH);
  assert.strictEqual(s.tall, false);
});

test('T480 clip source does not classify by role / inject / harness', function () {
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  const start = src.indexOf('// 🎯T480 / T106: size-only clip');
  const end = src.indexOf('function createStreamJoin');
  assert.ok(start >= 0 && end > start, 'T480 clip block present');
  const block = src.slice(start, end);
  assert.ok(!/user_info|git_status|classifyInspect|system-reminder|injectKind|standing-brief/.test(block),
    'size clip must not branch on harness / inject tags');
  assert.ok(!/if\s*\(\s*role\s*===/.test(block),
    'size clip must not fork the tall decision on role');
});

test('T480 renderModel applies size clip; nuggets stay nuggets', function () {
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  const rm = src.match(/function renderModel\(model\) \{[\s\S]*?\n    function invalidatePaint/);
  assert.ok(rm, 'renderModel present');
  assert.ok(rm[0].indexOf("kind === 'nugget'") >= 0, 'nuggets still short-circuit');
  assert.ok(rm[0].indexOf('layoutSizeClip') >= 0,
    'renderModel must run the widget size clip after attach');
  assert.ok(rm[0].indexOf('continue') >= 0 &&
    rm[0].indexOf("kind === 'nugget'") < rm[0].indexOf('layoutSizeClip'),
    'nuggets skip clip — T480 is about bubbles');
});

test('T480 index.html layoutMsg is not a second inspector-only skip', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ConversationWidget.layoutSizeClip') >= 0,
    'main renderBody must call the widget clip');
  assert.ok(html.indexOf('ConversationWidget.measureCollapse') >= 0,
    'window.measureCollapse is a widget alias, not a second probe');
  assert.ok(!/function measureCollapse\(d, role, text\) \{[\s\S]{0,400}parseAssistantMarkdown/.test(html),
    'index.html must not keep a second role-painting probe');
  assert.ok(/#agent-inspect-body\s*>\s*\.msg\.msg-clipped\s*>\s*\.msg-body/.test(html),
    'inspect CSS must let T106 clip win over the T167 unclip');
});

test('T480 renderModel clips a tall bubble and leaves a short one alone', function () {
  const dom = fakeDom('compact');
  const ctl = CW.mount(dom.host, {
    document: dom.doc,
    density: 'compact',
    agentId: 'jv-t480',
    buildMsg: function (role, text) {
      const tall = String(text || '').length > 80;
      return fakeBubble({ role: role, text: text, fullH: tall ? 400 : 24 });
    },
    lineSpec: function (line) {
      if (line && line.nugget) {
        return { kind: 'nugget', html: '<div class="turn-marker inject-nugget">⋯</div>' };
      }
      return {
        kind: 'bubble',
        role: line.role === 'assistant' ? 'jevons' : (line.role || 'user'),
        text: line.text || '',
        when: line.when,
      };
    },
  });
  // fakeDom createElement for nugget wrap: set innerHTML + firstChild.
  const origCreate = dom.doc.createElement;
  dom.doc.createElement = function (tag) {
    const el = origCreate(tag);
    Object.defineProperty(el, 'innerHTML', {
      configurable: true,
      set: function (html) {
        this._html = html;
        if (html && String(html).indexOf('inject-nugget') >= 0) {
          const n = fakeEl('nugget');
          n.className = 'turn-marker inject-nugget';
          this.firstChild = n;
        }
      },
      get: function () { return this._html || ''; },
    });
    return el;
  };
  const wall = '<user_info>\n' + 'line\n'.repeat(40) + '</rules>';
  ctl.renderModel({
    lines: [
      { role: 'user', text: 'hi' },
      { role: 'user', text: wall },
      { role: 'assistant', text: '### reply\n' + '- item\n'.repeat(20) },
      { role: 'user', text: 'ignored', nugget: true },
    ],
  });
  const kids = (dom.byId[ctl.ids.messages] && dom.byId[ctl.ids.messages].children) || [];
  const bubbles = kids.filter(function (c) { return c.classList && c.classList.contains('msg'); });
  const nuggets = kids.filter(function (c) {
    return c.className && String(c.className).indexOf('inject-nugget') >= 0;
  });
  assert.strictEqual(bubbles.length, 3, 'three bubbles (nugget is not a bubble)');
  assert.strictEqual(nuggets.length, 1, 'T233 nugget still a nugget');
  assert.ok(!bubbles[0].classList.contains('msg-clipped'), 'short user unclipped');
  assert.ok(!bubbles[0]._expandBtn, 'short user no tab');
  assert.ok(bubbles[1].classList.contains('msg-clipped'), 'user_info wall clipped');
  assert.ok(bubbles[1]._expandBtn, 'user_info wall has tab');
  assert.ok(bubbles[2].classList.contains('msg-clipped'), 'tall assistant clipped');
  assert.ok(bubbles[2]._expandBtn, 'tall assistant has tab');
});

Promise.all(asyncTests).then(function () {
  console.log('PASS conversation_widget_test (' + passed + ' tests)');
});
