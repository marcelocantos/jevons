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

test('T371 index.html: every inspect line replace reconciles pending turns', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function reconcileInspectPending') >= 0,
    'host defines the pending reconcile seam');

  // History replace still reconciles. Live frames grow on the widget and
  // only ack pending on user events (🎯T372 — remount is not grow).
  const wire = html.match(/function handleAgentTranscriptWire\([\s\S]*?\nfunction inspectSpecFor/);
  assert.ok(wire, 'handleAgentTranscriptWire present');
  assert.ok(wire[0].indexOf('applyWireEvent') >= 0,
    'live frames grow via the widget, not a second painter');
  assert.ok((wire[0].match(/reconcileInspectPending\(/g) || []).length >= 1,
    'history replace still reconciles pending owner turns');

  // Staging is wired into the mount, not reimplemented host-side (🎯T372).
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
  const live = html.match(/if \(m\.kind === 'live' && m\.event\) \{[\s\S]*?\n    return;\n  \}/);
  assert.ok(live, 'live branch present');
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

test('T119.6 startTurn twice leaves one canvas child', function () {
  const CW = require('./conversation_widget.js');
  const canvas = { children: [] };
  let slot = CW.ensureTurnSlot(canvas, null);
  slot = CW.ensureTurnSlot(canvas, slot);
  slot = CW.ensureTurnSlot(canvas, slot);
  assert.strictEqual(canvas.children.length, 1, 'second startTurn must not append');
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
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const start = html.match(/function startTurn\(\) \{[\s\S]*?\nfunction closeTurn/);
  assert.ok(start, 'startTurn exists');
  assert.ok(/isConnected/.test(start[0]), 'startTurn is idempotent while the slot is connected');
  assert.ok(/attachTranscriptRow/.test(start[0]),
    'T494.1: startTurn attaches itself — virtualize is a no-op during replay');
  assert.ok(!/virtualizeMessages\(\)/.test(start[0]),
    'T494.1: startTurn must not depend on virtualizeMessages to set row.el');
  assert.ok(!/createElement/.test(start[0]),
    'T119.4: startTurn must not mint DOM — apply/attachTranscriptRow is the only mint');
  const close = html.match(/function closeTurn\(\) \{[\s\S]*?\nfunction formatAgentNote/);
  assert.ok(close, 'closeTurn exists');
  assert.ok(!/\.remove\(\)/.test(close[0]),
    'T119.4: closeTurn must not destroy nodes — detach/apply does');
  const opt = html.match(/function paintOptimisticMainUser\([\s\S]*?\n\}/);
  assert.ok(opt, 'paintOptimisticMainUser exists');
  assert.ok(!/startTurn\(\)/.test(opt[0]),
    'optimistic user paint must not pre-open a slot that onTurnSlotOpen will open');
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
