// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T372 — the pending-owner-turn contract, and the PARITY ORACLE.
//
// Section 3 is the load-bearing part. Prose in a design doc cannot stop main
// and the agent panes from drifting apart again; a test that drives the same
// scenario through BOTH public surfaces and deep-equals the outcome can. If
// anyone re-forks the contract — a main-only optimisation, a sidebar-only
// special case — the parity table goes red naming the scenario.
//
// Section 4 asserts the stronger 🎯T372 invariant: not merely that the two
// surfaces AGREE, but that they are the SAME code. Two implementations that
// happen to agree today are exactly the state the owner rejected.

const assert = require('assert');
const fs = require('fs');
const path = require('path');

const PT = require('./pending_turns.js');
const CP = require('./composer_persist.js');
const CW = require('./conversation_widget.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  - ' + name);
  } catch (e) {
    failed++;
    console.error('FAIL- ' + name);
    console.error('      ' + (e && e.message ? e.message : String(e)));
  }
}

const AGENT = 'jv-t371-aside-send-parity';

// ── 1. Core contract ────────────────────────────────────────────────

test('stage keys the turn by agent and trims the body', function () {
  const s = PT.stage(PT.empty(), AGENT, '  hello  ', { now: 100, id: 'x1' });
  assert.deepStrictEqual(s.items, [
    { id: 'x1', agent: AGENT, text: 'hello', when: 100, failed: false },
  ]);
});

test('stage is idempotent per (agent, body) while unacked', function () {
  let s = PT.stage(PT.empty(), AGENT, 'twice', { now: 1, id: 'a' });
  s = PT.stage(s, AGENT, 'twice', { now: 2, id: 'b' });
  assert.strictEqual(s.items.length, 1);
});

test('same body to two agents stages twice — panes are independent', function () {
  let s = PT.stage(PT.empty(), 'agent-a', 'ping', { now: 1, id: 'a' });
  s = PT.stage(s, 'agent-b', 'ping', { now: 2, id: 'b' });
  assert.strictEqual(s.items.length, 2);
});

test('stage ignores empty agent or empty body', function () {
  assert.strictEqual(PT.stage(PT.empty(), '', 'x').items.length, 0);
  assert.strictEqual(PT.stage(PT.empty(), AGENT, '   ').items.length, 0);
});

test('ack drops matched turns and leaves unsealed ones', function () {
  let s = PT.stage(PT.empty(), AGENT, 'sealed', { now: 1, id: 'a' });
  s = PT.stage(s, AGENT, 'unsealed', { now: 2, id: 'b' });
  const r = PT.ack(s, AGENT, [
    { role: 'user', text: 'sealed' },
    { role: 'assistant', text: 'unsealed' },
  ]);
  assert.deepStrictEqual(r.acked.map(function (i) { return i.text; }), ['sealed']);
  assert.deepStrictEqual(r.state.items.map(function (i) { return i.text; }), ['unsealed']);
});

test('ack only consumes an assistant-free user line once', function () {
  let s = PT.stage(PT.empty(), AGENT, 'dup', { now: 1, id: 'a' });
  s = { items: s.items.concat([{ id: 'b', agent: AGENT, text: 'dup', when: 2, failed: false }]) };
  const r = PT.ack(s, AGENT, [{ role: 'user', text: 'dup' }]);
  assert.strictEqual(r.acked.length, 1);
  assert.strictEqual(r.state.items.length, 1);
});

test("ack never touches another agent's pending set", function () {
  let s = PT.stage(PT.empty(), 'agent-a', 'ping', { now: 1, id: 'a' });
  s = PT.stage(s, 'agent-b', 'ping', { now: 2, id: 'b' });
  const r = PT.ack(s, 'agent-a', [{ role: 'user', text: 'ping' }]);
  assert.deepStrictEqual(r.state.items.map(function (i) { return i.agent; }), ['agent-b']);
});

test('apply re-appends an unsealed turn a history frame dropped', function () {
  const s = PT.stage(PT.empty(), AGENT, 'my words', { now: 7, id: 'a' });
  const out = PT.apply([{ role: 'assistant', text: 'server said' }], s, AGENT);
  assert.strictEqual(out.length, 2);
  assert.deepStrictEqual(out[1], { role: 'user', text: 'my words', when: 7, _pending: true });
});

test('apply does not duplicate a turn the frame already sealed', function () {
  const s = PT.stage(PT.empty(), AGENT, 'my words', { now: 7, id: 'a' });
  const out = PT.apply([{ role: 'user', text: 'my words', when: 5 }], s, AGENT);
  assert.strictEqual(out.length, 1);
  assert.strictEqual(out[0].when, 5, 'sealed server turn wins');
});

test('apply preserves every field on existing lines (🎯T308)', function () {
  const out = PT.apply(
    [{ role: 'user', text: 'x', when: 3, turn: 12, tool: 'bash' }],
    PT.empty(),
    AGENT,
  );
  assert.deepStrictEqual(out[0], { role: 'user', text: 'x', when: 3, turn: 12, tool: 'bash' });
});

test('apply does not mutate the caller line array', function () {
  const lines = [{ role: 'assistant', text: 'a' }];
  PT.apply(lines, PT.stage(PT.empty(), AGENT, 'p', { now: 1, id: 'a' }), AGENT);
  assert.strictEqual(lines.length, 1);
});

test('a failed send keeps its bubble, marked', function () {
  const s = PT.markFailed(PT.stage(PT.empty(), AGENT, 'lost', { now: 1, id: 'a' }), 'a');
  const out = PT.apply([], s, AGENT);
  assert.strictEqual(out.length, 1, 'failed send must never vanish');
  assert.strictEqual(out[0]._failed, true);
});

// ── 2. Serialization / migration ────────────────────────────────────

test('serialize → deserialize round-trips the agent key', function () {
  const s = PT.stage(PT.empty(), AGENT, 'body', { now: 42, id: 'a' });
  const r = PT.deserialize(PT.serialize(s));
  assert.ok(r.ok);
  assert.deepStrictEqual(r.state.items, s.items);
});

test('legacy main pending ({id,text,stagedAt}) migrates to agent jevons', function () {
  const raw = JSON.stringify({ items: [{ id: 'p1', text: 'old turn', stagedAt: 99 }] });
  const r = PT.deserialize(raw);
  assert.ok(r.ok);
  assert.deepStrictEqual(r.state.items, [
    { id: 'p1', agent: PT.MAIN_AGENT, text: 'old turn', when: 99, failed: false },
  ]);
});

test('deserialize fails loud, never silently resets', function () {
  const bad = PT.deserialize('{not json');
  assert.strictEqual(bad.ok, false);
  assert.ok(bad.present);
  assert.ok(/parse failed/.test(bad.error));
  const notObj = PT.deserialize('"a string"');
  assert.strictEqual(notObj.ok, false);
});

// ── 3. PARITY ORACLE: main vs agent must behave identically ─────────
//
// Each surface is expressed through its OWN public API — main through
// ComposerPersist (agent-free signatures, as index.html calls it), the agent
// pane through ConversationWidget. Identical scenarios, identical outcomes.
// Ids and timestamps legitimately differ, so outcomes are compared on the
// owner-visible shape: which bubbles exist, in what order, in what state.

const MAIN_SURFACE = {
  label: 'main (ComposerPersist)',
  agent: PT.MAIN_AGENT,
  empty: function () { return CP.emptyPending(); },
  stage: function (st, text) { return CP.stagePending(st, text); },
  ack: function (st, lines) {
    const texts = (lines || [])
      .filter(function (l) { return l && l.role === 'user'; })
      .map(function (l) { return l.text; });
    return CP.ackPendingAgainstHistory(st, texts);
  },
  apply: function (lines, st) { return PT.apply(lines, st, PT.MAIN_AGENT); },
  unacked: function (st) { return CP.unackedTexts(st); },
};

const AGENT_SURFACE = {
  label: 'agent pane (ConversationWidget)',
  agent: AGENT,
  empty: function () { return CW.emptyPending(); },
  stage: function (st, text) { return CW.stagePendingOwnerTurn(st, AGENT, text); },
  ack: function (st, lines) { return CW.ackPendingOwnerTurns(st, AGENT, lines); },
  apply: function (lines, st) { return CW.applyPendingOwnerTurns(lines, st, AGENT); },
  unacked: function (st) {
    return CW.pendingOwnerTurnsFor(st, AGENT).map(function (i) { return i.text; });
  },
};

const SURFACES = [MAIN_SURFACE, AGENT_SURFACE];

// Owner-visible projection of a line set: role, body, and pending marker.
// Deliberately drops id/when — those may differ without a product difference.
function visible(lines) {
  return (lines || []).map(function (l) {
    return {
      role: l.role,
      text: String(l.text == null ? '' : l.text).trim(),
      pending: !!l[PT.PENDING_FLAG],
    };
  });
}

// Scenario: (surface) -> { bubbles, unacked }. Each must be surface-agnostic.
const SCENARIOS = [
  {
    name: 'send then a history frame WITHOUT the turn — bubble survives',
    run: function (S) {
      const st = S.stage(S.empty(), 'owner words');
      // The server has not sealed the turn; the frame replaces the model.
      const acked = S.ack(st, [{ role: 'assistant', text: 'earlier reply' }]);
      const lines = S.apply([{ role: 'assistant', text: 'earlier reply' }], acked.state);
      return { bubbles: visible(lines), unacked: S.unacked(acked.state) };
    },
  },
  {
    name: 'send then a history frame WITH the turn — acked, not duplicated',
    run: function (S) {
      const st = S.stage(S.empty(), 'owner words');
      const frame = [
        { role: 'user', text: 'owner words' },
        { role: 'assistant', text: 'reply' },
      ];
      const acked = S.ack(st, frame);
      const lines = S.apply(frame, acked.state);
      return { bubbles: visible(lines), unacked: S.unacked(acked.state) };
    },
  },
  {
    name: 'double-send of one body stages once',
    run: function (S) {
      let st = S.stage(S.empty(), 'again');
      st = S.stage(st, 'again');
      const lines = S.apply([], st);
      return { bubbles: visible(lines), unacked: S.unacked(st) };
    },
  },
  {
    name: 'empty history frame cannot erase an unsealed turn',
    run: function (S) {
      const st = S.stage(S.empty(), 'do not delete me');
      const acked = S.ack(st, []);
      const lines = S.apply([], acked.state);
      return { bubbles: visible(lines), unacked: S.unacked(acked.state) };
    },
  },
  {
    name: 'two unsealed turns replay in send order',
    run: function (S) {
      let st = S.stage(S.empty(), 'first');
      st = S.stage(st, 'second');
      const lines = S.apply([{ role: 'assistant', text: 'ctx' }], st);
      return { bubbles: visible(lines), unacked: S.unacked(st) };
    },
  },
  {
    name: 'whitespace-only send is not staged',
    run: function (S) {
      const st = S.stage(S.empty(), '   ');
      return { bubbles: visible(S.apply([], st)), unacked: S.unacked(st) };
    },
  },
];

SCENARIOS.forEach(function (sc) {
  test('parity: ' + sc.name, function () {
    const results = SURFACES.map(function (S) {
      return { label: S.label, out: sc.run(S) };
    });
    const base = results[0];
    for (let i = 1; i < results.length; i++) {
      assert.deepStrictEqual(
        results[i].out,
        base.out,
        base.label + ' and ' + results[i].label + ' diverged on "' + sc.name +
        '".\n  ' + base.label + ': ' + JSON.stringify(base.out) +
        '\n  ' + results[i].label + ': ' + JSON.stringify(results[i].out) +
        '\n  🎯T372: main and every agent share ONE send/display/rehydrate' +
        ' contract. A difference here is a fork, not a feature — it needs an' +
        ' owner-signed exception in docs/design/one-chat-widget-fork-inventory.md.',
      );
    }
  });
});

// ── 4. One implementation, not two that agree ───────────────────────

test('ConversationWidget pending helpers ARE the shared contract', function () {
  const bindings = [
    ['emptyPending', CW.emptyPending, PT.empty],
    ['stagePendingOwnerTurn', CW.stagePendingOwnerTurn, PT.stage],
    ['ackPendingOwnerTurns', CW.ackPendingOwnerTurns, PT.ack],
    ['applyPendingOwnerTurns', CW.applyPendingOwnerTurns, PT.apply],
    ['markPendingOwnerTurnFailed', CW.markPendingOwnerTurnFailed, PT.markFailed],
    ['pendingOwnerTurnsFor', CW.pendingOwnerTurnsFor, PT.forAgent],
  ];
  bindings.forEach(function (b) {
    assert.strictEqual(
      b[1], b[2],
      'ConversationWidget.' + b[0] + ' must BE the PendingTurns function, not a ' +
      'second implementation of it (🎯T372 one contract).',
    );
  });
});

test('ComposerPersist delegates main staging to the shared contract', function () {
  const s = CP.stagePending(CP.emptyPending(), 'main body');
  assert.strictEqual(s.items.length, 1);
  assert.strictEqual(
    s.items[0].agent, PT.MAIN_AGENT,
    'main must stage through the agent-keyed contract — root jevons is an ' +
    'ordinary participant (🎯T372 principle 3), not a special class.',
  );
});

test('neither adopter re-implements stage/ack/apply', function () {
  const widget = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  const persist = fs.readFileSync(path.join(__dirname, 'composer_persist.js'), 'utf8');
  // A local `function stagePending…`/`function applyPendingOwnerTurns…` body is
  // how the fork grew back last time.
  assert.ok(
    !/function\s+applyPendingOwnerTurns\s*\(/.test(widget),
    'conversation_widget.js must delegate applyPendingOwnerTurns, not define it',
  );
  assert.ok(
    !/function\s+stagePendingOwnerTurn\s*\(/.test(widget),
    'conversation_widget.js must delegate stagePendingOwnerTurn, not define it',
  );
  assert.ok(
    persist.includes('PendingTurns.stage('),
    'composer_persist.js must stage through PendingTurns',
  );
  assert.ok(
    persist.includes('PendingTurns.ackTexts('),
    'composer_persist.js must ack through PendingTurns',
  );
});

test('pending_turns.js is embedded, loaded, and ordered before its adopters', function () {
  const embed = fs.readFileSync(path.join(__dirname, '..', 'embed.go'), 'utf8');
  assert.ok(embed.includes('scripts/pending_turns.js'), 'embed.go must embed pending_turns.js');

  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const at = function (f) { return html.indexOf('scripts/' + f + '.js"'); };
  assert.ok(at('pending_turns') > 0, 'index.html must load pending_turns.js');
  assert.ok(
    at('pending_turns') < at('conversation_widget'),
    'pending_turns.js must load before conversation_widget.js',
  );
  assert.ok(
    at('pending_turns') < at('composer_persist'),
    'pending_turns.js must load before composer_persist.js',
  );
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('PASS pending_turns_test');
