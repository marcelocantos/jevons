// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for 🎯T65 attention-thread model (prefix-first).
// Run: node web/scripts/attention_threads_test.js

'use strict';

const assert = require('assert');
const AT = require('./attention_threads.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n     ') : e);
  }
}

test('parsePrefix is case-insensitive and strips prefix', function () {
  const a = AT.parsePrefix('aside: hello world');
  assert.strictEqual(a.command, 'aside');
  assert.strictEqual(a.body, 'hello world');
  const b = AT.parsePrefix('  CAPTURE : side thought');
  assert.strictEqual(b.command, 'capture');
  assert.strictEqual(b.body, 'side thought');
  const c = AT.parsePrefix('no prefix here');
  assert.strictEqual(c.command, null);
  assert.strictEqual(c.body, 'no prefix here');
  const d = AT.parsePrefix('target: virtualise history');
  assert.strictEqual(d.command, 'target');
  assert.strictEqual(d.body, 'virtualise history');
});

test('target: opens file-target aside, wire, main focus (T93/T95)', function () {
  const r = AT.handleComposer(AT.emptyState(), 'target: Chat paste images work');
  assert.strictEqual(r.kind, 'send');
  assert.strictEqual(r.purpose, 'file-target');
  assert.ok(r.text.indexOf('[target-aside:') === 0);
  assert.ok(r.text.indexOf('jevons_target_file') > 0);
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads[0].purpose, 'file-target');
});

// 🎯T247: explicit open prefixes spawn immediately — no affordance-gated intermediate.
test('T247 target:/aside:/capture: open path has threadId + no create-gate state', function () {
  const RS = require('./route_suggest.js');
  const TR = require('./thread_route.js');

  const target = AT.handleComposer(AT.emptyState(), 'target: Explicit open no chip');
  assert.ok(target.threadId && target.threadId.indexOf('att-') === 0, 'target: mints aside');
  assert.strictEqual(target.kind, 'send');
  assert.strictEqual(target.routed, true);
  assert.strictEqual(target.purpose, 'file-target');
  assert.strictEqual(target.state.threads[0].status, 'open');
  // No intermediate create state in the pure model — open is the only state.
  assert.ok(RS.shouldSkipRouteSuggest(target), 'no route/create affordance after target:');
  // Routing the produced wire must not invent a match chip either.
  const candidates = AT.routeCandidates(target.state);
  const hit = TR.route(target.text, candidates);
  assert.strictEqual(hit.reason, 'explicit-prefix');
  assert.strictEqual(hit.threadId, null);
  const plan = RS.planAutoRouteAction(hit, {
    threads: candidates,
    body: target.text,
    composerResult: target,
  });
  assert.strictEqual(plan.steal, false);
  assert.strictEqual(plan.suggestion, null);

  const aside = AT.handleComposer(AT.emptyState(), 'aside: ship checklist now');
  assert.ok(aside.threadId);
  assert.strictEqual(aside.routed, true);
  assert.ok(RS.shouldSkipRouteSuggest(aside));

  const cap = AT.handleComposer(AT.emptyState(), 'capture: later idea');
  assert.ok(cap.threadId);
  assert.strictEqual(cap.kind, 'local');
  assert.ok(RS.shouldSkipRouteSuggest(cap));
});

test('detectTargetFiled + closeTargetAside dismisses filing aside (done, not parked)', function () {
  const open = AT.handleComposer(AT.emptyState(), 'target: Foo bar');
  const id = open.threadId;
  assert.ok(id);
  const filed = AT.detectTargetFiled('Filed 🎯T120 — Foo\n__TARGET_FILED__:T120\n');
  assert.strictEqual(filed, 'T120');
  const closed = AT.closeTargetAside(open.state, id);
  assert.strictEqual(closed.threads[0].status, 'done');
  assert.strictEqual(closed.focusId, AT.MAIN_ID);
  // Default chrome excludes completed filing asides (🎯T95.1).
  assert.strictEqual(AT.stack(closed).length, 0);
  // Archive still lists them for discoverability.
  const arch = AT.archive(closed);
  assert.strictEqual(arch.length, 1);
  assert.strictEqual(arch[0].id, id);
  assert.strictEqual(arch[0].status, 'done');
});

test('closeTargetAside without id closes most recent open file-target', function () {
  let s = AT.handleComposer(AT.emptyState(), 'target: First filing').state;
  s = AT.handleComposer(s, 'target: Second filing').state;
  const closed = AT.closeTargetAside(s); // most recent open file-target
  const second = closed.threads.find(function (t) { return t.body === 'Second filing'; });
  const first = closed.threads.find(function (t) { return t.body === 'First filing'; });
  assert.strictEqual(second.status, 'done');
  assert.strictEqual(first.status, 'open');
  assert.strictEqual(AT.stack(closed).length, 1);
  assert.strictEqual(AT.archive(closed).length, 1);
});

test('general dismiss: done leaves chrome, stays in archive', function () {
  const cap = AT.handleComposer(AT.emptyState(), 'aside: billing nit');
  const id = cap.threadId;
  const done = AT.dismiss(cap.state, id);
  assert.strictEqual(done.threads[0].status, 'done');
  assert.strictEqual(done.focusId, AT.MAIN_ID);
  assert.strictEqual(AT.stack(done).length, 0);
  assert.strictEqual(AT.archive(done).length, 1);
  assert.strictEqual(AT.archivedStack(done)[0].id, id);
});

test('manual park still appears in default stack (not done)', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Workstream').state;
  const id = s.threads[0].id;
  s = AT.park(s, id);
  assert.strictEqual(s.threads[0].status, 'parked');
  const st = AT.stack(s);
  assert.strictEqual(st.length, 1);
  assert.strictEqual(st[0].status, 'parked');
  assert.strictEqual(AT.archive(s).length, 0);
});

test('serialize/deserialize preserves done status', function () {
  let s = AT.handleComposer(AT.emptyState(), 'target: Persist done').state;
  s = AT.closeTargetAside(s, s.threads[0].id);
  const again = AT.deserialize(AT.serialize(s));
  assert.strictEqual(again.threads[0].status, 'done');
  assert.strictEqual(AT.stack(again).length, 0);
  assert.strictEqual(AT.archive(again).length, 1);
  // Legacy "archived" alias normalizes to done.
  const legacy = AT.deserialize(JSON.stringify({
    focusId: 'main',
    threads: [{ id: 'att-x', title: 'X', body: 'X', status: 'archived', purpose: '', createdAt: 1, updatedAt: 1 }],
  }));
  assert.strictEqual(legacy.threads[0].status, 'done');
});

test('load migrates legacy parked file-target → done (not manual park)', function () {
  // Pre-T95.1 localStorage: auto-close parked the filing aside.
  const raw = JSON.stringify({
    focusId: AT.MAIN_ID,
    threads: [
      {
        id: 'att-file-old',
        title: 'T113 filing',
        body: 'target: something',
        status: 'parked',
        purpose: 'file-target',
        createdAt: 1,
        updatedAt: 2,
      },
      {
        id: 'att-work',
        title: 'real workstream',
        body: 'still parked on purpose',
        status: 'parked',
        purpose: '',
        createdAt: 3,
        updatedAt: 4,
      },
    ],
  });
  const store = {};
  store[AT.STORAGE_KEY || 'jevons-attention-threads-v1'] = raw;
  const storage = {
    getItem: function (k) { return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null; },
    setItem: function (k, v) { store[k] = String(v); },
  };
  const loaded = AT.load(storage);
  const filing = loaded.threads.find(function (t) { return t.id === 'att-file-old'; });
  const work = loaded.threads.find(function (t) { return t.id === 'att-work'; });
  assert.ok(filing);
  assert.ok(work);
  assert.strictEqual(filing.status, 'done');
  assert.strictEqual(filing.purpose, 'file-target');
  assert.strictEqual(work.status, 'parked');
  // Default chrome: only intentional park remains.
  const st = AT.stack(loaded);
  assert.strictEqual(st.length, 1);
  assert.strictEqual(st[0].id, 'att-work');
  assert.strictEqual(AT.archive(loaded).length, 1);
  assert.strictEqual(AT.archive(loaded)[0].id, 'att-file-old');
  // deserialize alone (same migration path)
  const d = AT.deserialize(raw);
  assert.strictEqual(d.threads.find(function (t) { return t.id === 'att-file-old'; }).status, 'done');
});

test('aside: sends routed wire text and keeps main focus', function () {
  const r = AT.handleComposer(AT.emptyState(), 'aside: billing nit');
  assert.strictEqual(r.kind, 'send');
  assert.ok(r.text.indexOf('[attention:') === 0);
  assert.ok(r.text.indexOf('billing nit') > 0);
  assert.ok(r.text.indexOf('aside:') === -1);
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads.length, 1);
  assert.strictEqual(r.state.threads[0].body, 'billing nit');
});

test('capture: is local — no send, main focus, thread tracked', function () {
  const r = AT.handleComposer(AT.emptyState(), 'capture: later idea');
  assert.strictEqual(r.kind, 'local');
  assert.strictEqual(r.text, '');
  assert.strictEqual(r.state.focusId, AT.MAIN_ID);
  assert.strictEqual(r.state.threads.length, 1);
  assert.strictEqual(r.state.threads[0].body, 'later idea');
  assert.strictEqual(r.clearComposer, true);
});

test('park: and pursue: keep both threads tracked', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: First').state;
  s = AT.handleComposer(s, 'capture: Second').state;
  assert.strictEqual(s.threads.length, 2);
  const firstId = s.threads.find(function (t) { return t.body === 'First'; }).id;
  s = AT.pursue(s, firstId);
  assert.strictEqual(s.focusId, firstId);
  s = AT.handleComposer(s, 'park:').state;
  assert.strictEqual(s.focusId, AT.MAIN_ID);
  const first = s.threads.find(function (t) { return t.id === firstId; });
  assert.strictEqual(first.status, 'parked');
  s = AT.handleComposer(s, 'pursue: First').state;
  assert.strictEqual(s.focusId, firstId);
  assert.strictEqual(AT.findThread(s, firstId).status, 'open');
  assert.strictEqual(s.threads.length, 2);
});

test('main: returns focus; main: body sends on main', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Side').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const local = AT.handleComposer(s, 'main:');
  assert.strictEqual(local.kind, 'local');
  assert.strictEqual(local.state.focusId, AT.MAIN_ID);
  s = AT.pursue(local.state, id);
  const send = AT.handleComposer(s, 'main: back to work');
  assert.strictEqual(send.kind, 'send');
  assert.strictEqual(send.text, 'back to work');
  assert.strictEqual(send.state.focusId, AT.MAIN_ID);
  assert.ok(!send.routed);
});

test('pursued focus without prefix routes as aside wire', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Draft').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const r = AT.handleComposer(s, 'Pursued body');
  assert.strictEqual(r.kind, 'send');
  assert.ok(r.routed);
  assert.ok(r.text.indexOf('[attention:' + id + '|') === 0);
  assert.ok(r.text.endsWith('Pursued body'));
});

test('plain main send is unchanged', function () {
  const r = AT.handleComposer(AT.emptyState(), 'Hello main');
  assert.strictEqual(r.kind, 'send');
  assert.strictEqual(r.text, 'Hello main');
  assert.ok(!r.routed);
  assert.strictEqual(r.state.threads.length, 0);
});

test('serialize round-trip drops legacy asideNext', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: Persist me').state;
  const again = AT.deserialize(AT.serialize(s));
  assert.strictEqual(again.threads.length, 1);
  assert.strictEqual(again.threads[0].body, 'Persist me');
  assert.strictEqual(again.asideNext, undefined);
});

test('load/save via mock storage', function () {
  const store = {};
  const storage = {
    getItem: function (k) { return Object.prototype.hasOwnProperty.call(store, k) ? store[k] : null; },
    setItem: function (k, v) { store[k] = String(v); },
  };
  let s = AT.handleComposer(AT.emptyState(), 'capture: Stored').state;
  AT.save(storage, s);
  const loaded = AT.load(storage);
  assert.strictEqual(loaded.threads[0].body, 'Stored');
});

test('composerPlaceholder: main is clean; side uses [aside: title] hint', function () {
  const mainPh = 'Message...';
  assert.strictEqual(AT.composerPlaceholder(AT.emptyState(), mainPh), mainPh);
  assert.ok(AT.composerPlaceholder(AT.emptyState(), mainPh).indexOf('[main') === -1);

  let s = AT.handleComposer(AT.emptyState(), 'capture: billing nit later').state;
  const id = s.threads[0].id;
  s = AT.pursue(s, id);
  const ph = AT.composerPlaceholder(s, mainPh);
  assert.ok(ph.indexOf('[aside: ') === 0);
  assert.ok(ph.indexOf('] Message...') > 0);
  assert.ok(ph.indexOf('billing') > 0);
  assert.ok(ph.indexOf('[main') === -1);
});

// ── 🎯T134 chrome hygiene + routing honesty ──────────────────────────

test('T134 routeCandidates: open only — never done or parked', function () {
  let s = AT.emptyState();
  s = AT.handleComposer(s, 'capture: restic backup work').state;
  const openId = s.threads[0].id;
  s = AT.handleComposer(s, 'capture: parked later').state;
  const parkId = s.threads[0].id;
  s = AT.park(s, parkId);
  s = AT.handleComposer(s, 'aside: done ghost title').state;
  const doneId = s.threads[0].id;
  s = AT.dismiss(s, doneId);

  const cands = AT.routeCandidates(s);
  assert.strictEqual(cands.length, 1);
  assert.strictEqual(cands[0].id, openId);
  assert.strictEqual(cands[0].status, 'open');
  assert.ok(cands.every(function (t) { return t.status === 'open'; }));
  // Done/parked never appear even if titles would match auto-route.
  assert.ok(!cands.find(function (t) { return t.id === doneId; }));
  assert.ok(!cands.find(function (t) { return t.id === parkId; }));
});

test('T134 stack excludes done; filing close still leaves bar clean', function () {
  let s = AT.handleComposer(AT.emptyState(), 'target: File me please').state;
  assert.strictEqual(AT.stack(s).length, 1);
  s = AT.closeTargetAside(s);
  assert.strictEqual(AT.stack(s).length, 0);
  assert.strictEqual(AT.routeCandidates(s).length, 0);
  assert.strictEqual(AT.archive(s).length, 1);
});

test('T134 model stack still accumulates; T136 chrome/visible empty', function () {
  assert.ok(AT.MAX_VISIBLE_CHIPS >= 1);
  let s = AT.emptyState();
  for (let i = 0; i < AT.MAX_VISIBLE_CHIPS + 3; i++) {
    s = AT.handleComposer(s, 'capture: Workstream item ' + i + ' unique').state;
  }
  const full = AT.stack(s);
  assert.strictEqual(full.length, AT.MAX_VISIBLE_CHIPS + 3);
  // 🎯T136: top chrome never shows aside chips (RHS fleet tree owns them).
  assert.strictEqual(AT.chromeStack(s).length, 0);
  const vs = AT.visibleStack(s);
  assert.strictEqual(vs.shown.length, 0);
  assert.strictEqual(vs.overflowCount, 0);
});

// ── 🎯T136 asides live only in RHS fleet tree ─────────────────────────

test('T136 chromeStack always empty even with open and parked asides', function () {
  let s = AT.handleComposer(AT.emptyState(), 'aside: billing nit').state;
  s = AT.handleComposer(s, 'capture: parked later').state;
  const parkId = s.threads[0].id;
  s = AT.park(s, parkId);
  assert.ok(AT.stack(s).length >= 2, 'model stack still tracks asides');
  assert.strictEqual(AT.chromeStack(s).length, 0);
  assert.strictEqual(AT.visibleStack(s).shown.length, 0);
  // Route candidates still open-only for T99/T135 (no silent steal elsewhere).
  assert.ok(AT.routeCandidates(s).length >= 1);
});

// ── 🎯T250: asides not on main transcript; sidebar path shows them ────

test('T250 parseAsideWireUserText attention + target-aside', function () {
  const att = AT.parseAsideWireUserText('[attention:att-billing|billing nit]\nbilling body');
  assert.ok(att);
  assert.strictEqual(att.kind, 'attention');
  assert.strictEqual(att.id, 'att-billing');
  assert.strictEqual(att.title, 'billing nit');
  assert.strictEqual(att.displayText, 'billing body');
  assert.ok(AT.isAsideWireUserText(att ? '[attention:att-billing|t]\nx' : ''));
  assert.ok(!AT.shouldPaintMainUserText('[attention:att-billing|t]\nx'));
  assert.ok(AT.shouldPaintMainUserText('plain main message'));
  assert.ok(!AT.isAsideWireUserText('plain main message'));

  const wire = AT.formatTargetWire('att-file', 'Chat paste images', 'Chat paste images work');
  const tgt = AT.parseAsideWireUserText(wire);
  assert.ok(tgt);
  assert.strictEqual(tgt.kind, 'target-aside');
  assert.strictEqual(tgt.id, 'att-file');
  assert.ok(tgt.displayText.indexOf('Chat paste images work') === 0);
  assert.ok(tgt.displayText.indexOf('Ceremony') < 0, 'ceremony stripped from display');
  assert.ok(!AT.shouldPaintMainUserText(wire));
});

test('T250 extractAsideWireTurnsFromFrames + merge for sidebar path', function () {
  const frames = [
    { type: 'user', message: { content: 'main hello' } },
    {
      type: 'user',
      message: { content: '[attention:att-side|billing]\nbilling nit body' },
    },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'main reply' }] },
    },
    {
      type: 'user',
      message: {
        content: '[target-aside: att-file | Target title]\nTarget body\n\n(Ceremony: short-lived…)',
      },
    },
  ];
  const cache = AT.extractAsideWireTurnsFromFrames(frames);
  assert.ok(cache['att-side'], 'attention id in cache');
  assert.strictEqual(cache['att-side'].length, 1);
  assert.strictEqual(cache['att-side'][0].role, 'user');
  assert.strictEqual(cache['att-side'][0].text, 'billing nit body');
  assert.ok(cache['att-file'], 'target-aside id in cache');
  assert.strictEqual(cache['att-file'][0].text, 'Target body');

  // Sidebar path: empty process transcript still shows wire turns.
  const emptyProc = AT.mergeInspectLinesWithAsideWire([], cache['att-side']);
  assert.strictEqual(emptyProc.length, 1);
  assert.strictEqual(emptyProc[0].text, 'billing nit body');

  // Process turns merge without losing wire.
  const merged = AT.mergeInspectLinesWithAsideWire(
    [{ role: 'assistant', text: 'aside agent reply' }],
    cache['att-side'],
  );
  assert.strictEqual(merged.length, 2);
  assert.strictEqual(merged[0].role, 'user');
  assert.strictEqual(merged[1].role, 'assistant');

  // Dedupe consecutive record
  const c2 = Object.create(null);
  AT.recordAsideWireUserTurn(c2, '[attention:att-side|billing]\nbilling nit body');
  AT.recordAsideWireUserTurn(c2, '[attention:att-side|billing]\nbilling nit body');
  assert.strictEqual(c2['att-side'].length, 1, 'dedupe identical consecutive');
});

test('T250 index.html: main paint filters aside wires; sidebar merge wired', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('shouldPaintMainUserText') >= 0, 'main paint gate');
  assert.ok(html.indexOf('noteAsideWireFromMain') >= 0, 'records aside wires');
  assert.ok(html.indexOf('asideWireTurnsCache') >= 0, 'sidebar cache');
  assert.ok(html.indexOf('mergeModelWithAsideWire') >= 0 ||
    html.indexOf('mergeInspectLinesWithAsideWire') >= 0,
    'sidebar merge path');
  assert.ok(html.indexOf('ingestAsideWiresFromHistoryLines') >= 0,
    'history hydrate harvests aside wires');
  // User handle must short-circuit paint for aside wires.
  assert.ok(/shouldPaintMainUserText/.test(html) &&
    /noteAsideWireFromMain/.test(html),
    'handle user path uses T250 helpers');
});

// ── 🎯T264: flash-class never-paint (self-clear after paint still fails) ─

test('T264 looksLikeAsideWireMarker + never paint incident / image-prefix flash', function () {
  // Owner incident fixture (att-msftck4l freeform aside).
  const incident =
    '[attention:att-msftck4l-9sguxj|how does bullseye compare to beads?]\n' +
    'how does bullseye compare to beads?';
  assert.ok(AT.looksLikeAsideWireMarker(incident), 'incident marker class');
  assert.ok(AT.isAsideWireUserText(incident));
  assert.ok(!AT.shouldPaintMainUserText(incident), 'never paint main');

  // Header-only flash (truncated bubble title class).
  const headerOnly = '[attention:att-msftck4l-9sguxj|how does…]';
  assert.ok(!AT.shouldPaintMainUserText(headerOnly));

  // Image prepend before wire must not open a paint path (flash class).
  const withImage =
    '[image: d592b0380b1a9e9b]\n' +
    '[attention:att-x|billing nit]\nbilling body';
  assert.ok(AT.looksLikeAsideWireMarker(withImage), 'image-prefix still wire');
  assert.ok(AT.isAsideWireUserText(withImage));
  assert.ok(!AT.shouldPaintMainUserText(withImage), 'image+attention never paints');

  // target-aside full wire
  const tgt = AT.formatTargetWire('att-file', 'title', 'body');
  assert.ok(!AT.shouldPaintMainUserText(tgt));

  // Plain owner text still paints.
  assert.ok(AT.shouldPaintMainUserText('how does bullseye compare to beads?'));
  assert.ok(AT.shouldPaintMainUserText('[image: abc]\nplain body'));
});

test('T264 index.html: isMainAsideWireUserText + addMsg flash gate', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function isMainAsideWireUserText') >= 0,
    'local never-paint helper');
  assert.ok(html.indexOf('isMainAsideWireUserText') >= 0);
  // Live handle uses the flash-safe gate (not only soft AttentionThreads &&).
  assert.ok(/isMainAsideWireUserText\(content\)/.test(html),
    'handle user path calls isMainAsideWireUserText');
  // addMsg last-line defense (optimistic/live flash).
  assert.ok(/function addMsg\([\s\S]*?isMainAsideWireUserText/.test(html),
    'addMsg gates user role before insert');
  assert.ok(/role === ['"]user['"][\s\S]{0,200}?isMainAsideWireUserText/.test(html),
    'addMsg user branch never-paints aside wire');
  // renderFrames history path
  assert.ok(/isMainAsideWireUserText\(c\.text\)/.test(html) ||
    /isMainAsideWireUserText\(c\)/.test(html),
    'history renderFrames uses never-paint gate');
});

test('T136 index.html: attention-bar not used for aside wall; fleet register path', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ensureFleetAside') >= 0, 'create-aside dual-writes fleet');
  assert.ok(html.indexOf('/api/asides') >= 0, 'POST /api/asides');
  // renderAttention must not show chip wall for asides.
  assert.ok(/function renderAttention\(\)\s*\{[\s\S]{0,600}?classList\.remove\(['"]visible['"]\)/.test(html) ||
    html.indexOf("attentionBar.classList.remove('visible')") >= 0,
    'renderAttention never shows attention-bar');
  assert.ok(html.indexOf('aria-label="Attention asides"') >= 0 ||
    html.indexOf("aria-label='Attention asides'") >= 0,
    'owner-visible vocabulary: asides not threads');
  // Must not leave a chip loop as the default chrome path for open stack.
  assert.ok(html.indexOf('appendThreadChip') === -1,
    'no appendThreadChip wall in index');
});

// 🎯T152: target-file close must dismiss dual-written fleet aside (RHS 💡),
// not only local file-target attention state.
// 🎯T164: live-only path + resolveTargetAsideIdsToDismiss for stopped zombies.
test('T152/T164 maybeCloseTargetAside dismisses fleet aside on __TARGET_FILED__', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function dismissFleetAside') >= 0, 'dismissFleetAside reverse of ensure');
  assert.ok(html.indexOf("method: 'DELETE'") >= 0 || html.indexOf('method: "DELETE"') >= 0,
    'DELETE /api/asides/{id}');
  assert.ok(/\/api\/asides\/['"]\s*\+\s*encodeURIComponent/.test(html) ||
    html.indexOf("/api/asides/' + encodeURIComponent") >= 0 ||
    html.indexOf('/api/asides/" + encodeURIComponent') >= 0,
    'DELETE path encodes aside id');
  // maybeCloseTargetAside must call dismiss after closeTargetAside.
  const closeFn = html.match(/function maybeCloseTargetAside\([\s\S]*?\n\}/);
  assert.ok(closeFn, 'maybeCloseTargetAside present');
  assert.ok(closeFn[0].indexOf('closeTargetAside') >= 0, 'closes local file-target');
  assert.ok(closeFn[0].indexOf('dismissFleetAside') >= 0, 'also dismisses fleet aside');
  assert.ok(closeFn[0].indexOf('detectTargetFiled') >= 0, 'keeps T95 marker detection');
  assert.ok(closeFn[0].indexOf('historyReplayActive') >= 0,
    'T164: skip auto-close during history replay');
  assert.ok(closeFn[0].indexOf('resolveTargetAsideIdsToDismiss') >= 0,
    'T164: resolve dual-write ids including fleet zombies');
  // Live stream/seal must call maybeCloseTargetAside; paintBody must not.
  assert.ok(html.indexOf('maybeCloseTargetAside(el._streamRaw)') >= 0 ||
    /maybeCloseTargetAside\(\s*el\._streamRaw\s*\)/.test(html),
    'live stream scheduleJevonsRender calls maybeCloseTargetAside');
  assert.ok(html.indexOf('maybeCloseTargetAside(raw)') >= 0 ||
    /maybeCloseTargetAside\(\s*raw\s*\)/.test(html),
    'seal paint calls maybeCloseTargetAside');
  // paintBody must not *call* maybeCloseTargetAside (history/lazy-safe).
  // Comment may still mention the name; only an invocation is banned.
  const paintFn = html.match(/function paintBody\([\s\S]*?\nfunction maybeCloseTargetAside/);
  assert.ok(paintFn, 'paintBody present before maybeCloseTargetAside');
  assert.ok(!/maybeCloseTargetAside\s*\(/.test(paintFn[0]),
    'paintBody must not invoke maybeCloseTargetAside (history/lazy paint path)');

  // Model path (hermetic, no fetch): open target: → close on marker → no open file-target.
  const open = AT.handleComposer(AT.emptyState(), 'target: links always new tab');
  const id = open.threadId;
  assert.ok(id && id.indexOf('att-') === 0);
  assert.strictEqual(open.state.threads[0].purpose, 'file-target');
  assert.strictEqual(open.state.threads[0].status, 'open');
  const filed = AT.detectTargetFiled('Filed 🎯T151 — links\n__TARGET_FILED__:T151\n');
  assert.strictEqual(filed, 'T151');
  const closed = AT.closeTargetAside(open.state, id);
  assert.strictEqual(AT.stack(closed).filter(function (t) {
    return t.purpose === 'file-target' && t.status === 'open';
  }).length, 0, 'no open file-target after close');
  assert.strictEqual(closed.focusId, AT.MAIN_ID);
  // Wire contract: index pairs ensure on create with dismiss on filed.
  assert.ok(html.indexOf('ensureFleetAside') >= 0 && html.indexOf('dismissFleetAside') >= 0);

  // 🎯T164 pure resolve: open file-target → id; after local close, fleet zombie still listed.
  const idsOpen = AT.resolveTargetAsideIdsToDismiss(open.state, [
    { name: 'jevons', purpose: 'overseer' },
    { name: id, purpose: 'aside', status: 'stopped' },
  ], id);
  assert.ok(idsOpen.indexOf(id) >= 0, 'open filing resolves dual-write id');
  const idsZombie = AT.resolveTargetAsideIdsToDismiss(closed, [
    { name: 'jevons', purpose: 'overseer' },
    { name: id, purpose: 'aside', status: 'stopped' },
  ], id);
  assert.ok(idsZombie.indexOf(id) >= 0,
    'done file-target still in fleet → still resolve DELETE id (zombie residual)');
  const idsGone = AT.resolveTargetAsideIdsToDismiss(closed, [
    { name: 'jevons', purpose: 'overseer' },
  ], null);
  assert.strictEqual(idsGone.length, 0, 'no fleet dual-write → no invent ids');
});

test('T134 clearDone / dismissAllParked / clearChromeNoise', function () {
  let s = AT.emptyState();
  s = AT.handleComposer(s, 'capture: Keep open').state;
  const openId = s.threads[0].id;
  s = AT.handleComposer(s, 'capture: Park me').state;
  const parkId = s.threads[0].id;
  s = AT.park(s, parkId);
  s = AT.handleComposer(s, 'capture: Done me').state;
  const doneId = s.threads[0].id;
  s = AT.dismiss(s, doneId);

  assert.strictEqual(AT.stack(s).length, 2); // open + parked
  assert.strictEqual(AT.archive(s).length, 1);

  // clearDone purges archive only.
  let s2 = AT.clearDone(s);
  assert.strictEqual(AT.archive(s2).length, 0);
  assert.ok(AT.findThread(s2, openId));
  assert.ok(AT.findThread(s2, parkId));
  assert.ok(!AT.findThread(s2, doneId));

  // dismissAllParked leaves open.
  s2 = AT.dismissAllParked(s2);
  assert.strictEqual(AT.findThread(s2, parkId).status, 'done');
  assert.strictEqual(AT.findThread(s2, openId).status, 'open');
  assert.strictEqual(AT.stack(s2).length, 1);

  // clearChromeNoise: full bar reset (open+parked dismissed + archive purged).
  s = AT.clearChromeNoise(s);
  assert.strictEqual(AT.stack(s).length, 0);
  assert.strictEqual(AT.archive(s).length, 0);
  assert.strictEqual(s.focusId, AT.MAIN_ID);
  assert.ok(!AT.findThread(s, openId));
  assert.ok(!AT.findThread(s, parkId));
  assert.ok(!AT.findThread(s, doneId));
});

test('T134 capture dedupes same fingerprint open thread', function () {
  let s = AT.handleComposer(AT.emptyState(), 'capture: restic backup status').state;
  assert.strictEqual(s.threads.length, 1);
  const id = s.threads[0].id;
  s = AT.handleComposer(s, 'capture: restic backup status').state;
  assert.strictEqual(s.threads.length, 1);
  assert.strictEqual(s.threads[0].id, id);
  // Near-dup first line (whitespace/case) merges.
  s = AT.handleComposer(s, 'capture:   RESTIC backup status  ').state;
  assert.strictEqual(s.threads.length, 1);
  assert.strictEqual(s.threads[0].id, id);
  // Different workstream stacks separately.
  s = AT.handleComposer(s, 'capture: billing nit later').state;
  assert.strictEqual(s.threads.length, 2);
  // Done ghost does not block a new open capture with same title.
  s = AT.dismiss(s, id);
  s = AT.handleComposer(s, 'capture: restic backup status').state;
  assert.strictEqual(AT.stack(s).filter(function (t) {
    return (t.body || '').toLowerCase().indexOf('restic') !== -1;
  }).length, 1);
});

// ── 🎯T308: the aside-wire path must not strip turn timestamps ──

test('T308 aside wire cache + merge carry `when` to the sidebar renderer', function () {
  // Live/optimistic record: caller supplies arrival time.
  const cache = Object.create(null);
  AT.recordAsideWireUserTurn(cache, '[attention:att-side|billing]\nbody', 1754620000000);
  assert.strictEqual(cache['att-side'][0].when, 1754620000000, 'recorded turn keeps its time');
  // No time supplied (or junk) → no fabricated timestamp.
  const bare = Object.create(null);
  AT.recordAsideWireUserTurn(bare, '[attention:att-x|t]\nbody');
  assert.ok(!('when' in bare['att-x'][0]), 'no when when the caller knows none');
  AT.recordAsideWireUserTurn(bare, '[attention:att-y|t]\nbody', 'garbage');
  assert.ok(!('when' in bare['att-y'][0]), 'junk time is not recorded');

  // History replay: each frame's own timestamp rides through, not "now".
  const replayed = AT.extractAsideWireTurnsFromFrames([
    { type: 'user', ts: 1754620001, message: { content: '[attention:att-a|t]\nfrom seconds' } },
    {
      type: 'user',
      created_at: '2026-08-08T01:02:03Z',
      message: { content: '[attention:att-b|t]\nfrom ISO' },
    },
    { type: 'user', message: { content: '[attention:att-c|t]\nno time' } },
  ]);
  assert.strictEqual(replayed['att-a'][0].when, 1754620001000, 'seconds scale to ms');
  assert.strictEqual(replayed['att-b'][0].when, Date.parse('2026-08-08T01:02:03Z'));
  assert.ok(!('when' in replayed['att-c'][0]), 'frame with no time yields no when');

  // Merge is where the sidebar used to lose it: {role, text} rebuild dropped when.
  const merged = AT.mergeInspectLinesWithAsideWire(
    [{ role: 'assistant', text: 'reply', when: 1754620009000 }],
    [{ role: 'user', text: 'body', when: 1754620000000 }],
  );
  assert.strictEqual(merged.length, 2);
  assert.strictEqual(merged[0].when, 1754620000000, 'wire user turn keeps its time');
  assert.strictEqual(merged[1].when, 1754620009000, 'process turn keeps its time');

  // Dedupe hit: same turn seen twice → earliest known time wins.
  const dedup = AT.mergeInspectLinesWithAsideWire(
    [{ role: 'user', text: 'body', when: 1754620000000 }],
    [{ role: 'user', text: 'body', when: 1754620500000 }],
  );
  assert.strictEqual(dedup.length, 1, 'still one turn');
  assert.strictEqual(dedup[0].when, 1754620000000, 'earliest reading wins on dedupe');
  // A timeless copy must not erase a known time.
  const rescued = AT.mergeInspectLinesWithAsideWire(
    [{ role: 'user', text: 'body', when: 1754620000000 }],
    [{ role: 'user', text: 'body' }],
  );
  assert.strictEqual(rescued[0].when, 1754620000000, 'timeless duplicate does not clear when');
});

test('T308 index.html: aside wire paths stamp/preserve time, never strip it', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const note = html.match(/\nfunction noteAsideWireFromMain\([\s\S]*?\n\}\n/);
  assert.ok(note, 'noteAsideWireFromMain present');
  assert.ok(/recordAsideWireUserTurn\(asideWireTurnsCache, content, at\)/.test(note[0]),
    'live aside wire records an arrival timestamp (T308)');
  const ingest = html.match(/\nfunction ingestAsideWiresFromHistoryLines\([\s\S]*?\n\}\n/);
  assert.ok(ingest, 'ingestAsideWiresFromHistoryLines present');
  assert.ok(ingest[0].indexOf('turn.when = t.when') >= 0,
    'history replay keeps each frame time instead of dropping it');
  assert.ok(!/list\.push\(\{ role: 'user', text: text \}\)/.test(ingest[0]),
    'no bare {role, text} push that strips when');
});

// 🎯T368: the composer prepends "[image: id]" markers, which used to push the
// command off the start of the draft so target:/aside:/capture:/idea: fell
// through to main. Markers must be skipped for the match and kept in the body.
test('T368 parsePrefix skips leading [image: …] markers', function () {
  const a = AT.parsePrefix('[image: d592b0380b1a9e9b]\ntarget: virtualise history');
  assert.strictEqual(a.command, 'target');
  assert.strictEqual(a.body, 'virtualise history');
  assert.strictEqual(a.images, '[image: d592b0380b1a9e9b]');
  // Multiple markers, single-line prepend, odd spacing.
  const b = AT.parsePrefix('[image: aaa111] [image: bbb222]  ASIDE : two shots');
  assert.strictEqual(b.command, 'aside');
  assert.strictEqual(b.body, 'two shots');
  assert.strictEqual(b.images, '[image: aaa111] [image: bbb222]');
  // No images: unchanged, and no command still returns the draft verbatim.
  const c = AT.parsePrefix('capture: side thought');
  assert.strictEqual(c.command, 'capture');
  assert.strictEqual(c.body, 'side thought');
  assert.strictEqual(c.images, '');
  const d = AT.parsePrefix('[image: aaa111]\nplain body');
  assert.strictEqual(d.command, null);
  assert.strictEqual(d.body, '[image: aaa111]\nplain body', 'no command keeps markers in body');
  assert.strictEqual(d.images, '');
});

test('T368 image + target: opens filing aside, not main', function () {
  const withImg = AT.handleComposer(AT.emptyState(),
    '[image: d592b0380b1a9e9b]\ntarget: Chat paste images work');
  const textOnly = AT.handleComposer(AT.emptyState(), 'target: Chat paste images work');
  assert.strictEqual(withImg.kind, 'send');
  assert.strictEqual(withImg.routed, true, 'routed to the aside wire, not main');
  assert.strictEqual(withImg.purpose, 'file-target');
  assert.ok(withImg.threadId && withImg.threadId.indexOf('att-') === 0);
  assert.ok(withImg.text.indexOf('[target-aside:') === 0);
  assert.strictEqual(withImg.state.threads[0].purpose, textOnly.state.threads[0].purpose);
  // Title comes from the owner's words, and the image ref survives in the body.
  assert.strictEqual(withImg.state.threads[0].title, textOnly.state.threads[0].title);
  assert.ok(withImg.state.threads[0].body.indexOf('[image: d592b0380b1a9e9b]') >= 0,
    'attached image stays with the filing body');
  assert.ok(withImg.text.indexOf('[image: d592b0380b1a9e9b]') > 0, 'wire carries the marker');
});

test('T368 image + aside:/capture:/idea: route the same as text-only', function () {
  const marker = '[image: aaa111bbb222ccc3]';
  const aside = AT.handleComposer(AT.emptyState(), marker + '\naside: look at this');
  assert.strictEqual(aside.kind, 'send');
  assert.strictEqual(aside.routed, true);
  assert.ok(aside.text.indexOf('[attention:') === 0, 'aside wire, not a main send');
  assert.ok(aside.state.threads[0].body.indexOf(marker) >= 0);

  const cap = AT.handleComposer(AT.emptyState(), marker + '\ncapture: park this thought');
  assert.strictEqual(cap.kind, 'local');
  assert.strictEqual(cap.ideaCapture, true);
  assert.strictEqual(cap.ideaSource, 'capture');
  assert.ok(cap.threadId && cap.threadId.indexOf('att-') === 0);
  assert.ok(cap.ideaText.indexOf('park this thought') === 0);
  assert.ok(cap.ideaText.indexOf(marker) > 0);

  const idea = AT.handleComposer(AT.emptyState(), marker + '\nIDEA: spark');
  assert.strictEqual(idea.kind, 'local');
  assert.strictEqual(idea.ideaCapture, true);
  assert.strictEqual(idea.ideaSource, 'idea');
  assert.ok(idea.ideaText.indexOf('spark') === 0);
  assert.ok(idea.ideaText.indexOf(marker) > 0);

  // Image-only command still opens the aside rather than falling through.
  const imgOnly = AT.handleComposer(AT.emptyState(), marker + '\ntarget:');
  assert.strictEqual(imgOnly.kind, 'send');
  assert.strictEqual(imgOnly.purpose, 'file-target');

  // No marker at all: main send, unchanged.
  const plain = AT.handleComposer(AT.emptyState(), 'just talking to Jevons');
  assert.strictEqual(plain.routed, false);
  assert.strictEqual(plain.text, 'just talking to Jevons');
  // Marker without a command: still a plain main send with the marker intact.
  const imgPlain = AT.handleComposer(AT.emptyState(), marker + '\njust talking');
  assert.strictEqual(imgPlain.routed, false);
  assert.strictEqual(imgPlain.text, marker + '\njust talking');
});

test('T368 index.html: local command clears attached image chips', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const i = html.indexOf("if (r.kind === 'local' || r.kind === 'empty') {");
  assert.ok(i > 0, 'local/empty composer branch present');
  const branch = html.slice(i, i + 900);
  assert.ok(/pendingImages\.length = 0;/.test(branch),
    'capture:/idea: with attachments drops the chips (markers already consumed)');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall passed');
