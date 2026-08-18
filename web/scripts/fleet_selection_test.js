// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic unit tests for 🎯T383 — fleet tree selection is sticky.
// Run: node web/scripts/fleet_selection_test.js
//
// RED AGAINST THE PRE-FIX TREE, two ways, and both matter.
//   - The pure cases fail at require(): fleet_selection.js does not exist
//     before this target.
//   - The wiring cases fail on the shipped index.html: pre-fix it calls no
//     policy at all, so a poll's candidate is applied unconditionally.
//
// The first test replays the owner's report through the SAME composition
// index.html performs — 🎯T252's picker proposes, this module disposes — and
// asserts the pre-fix answer explicitly on the way past. If someone later
// "fixes" T252's poll branch by hand, that assertion tells them the fault
// they think they are removing is the one this module already contains, and
// where the real policy lives.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const FS_ = require('./fleet_selection.js');
const AT = require('./agent_transcript.js');

const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');

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

// The owner's tree: two asides plus the agents that are always there.
function tree() {
  return [
    { name: 'jevons-po', purpose: 'work', status: 'running' },
    { name: 'jv-t383-auto', purpose: 'work', status: 'running' },
    { name: 'att-aaa111', purpose: 'aside', status: 'running' },
    { name: 'att-bbb222', purpose: 'aside', status: 'running' },
  ];
}
const A = 'att-aaa111';   // the first aside — the one it kept jumping to
const B = 'att-bbb222';   // the second aside — the one the owner selected

// One background repaint, wired exactly as refreshAgents() wires it: 🎯T252
// proposes a candidate from the attention queue, 🎯T383 disposes.
function repaint(state, agents, attentionQueue) {
  const candidate = AT.pickAttentionAsideSelection({
    attentionNames: attentionQueue || [],
    currentSelection: state.selected,
    draftEmpty: true,
    reason: 'poll',
  });
  const v = FS_.resolveSelection({
    selected: state.selected,
    origin: state.origin,
    agents: agents,
    candidate: candidate,
  });
  return { selected: v.selected, origin: v.origin, changed: v.changed, reason: v.reason };
}

// ── The reported fault ──────────────────────────────────────────

test('the owner selects the second aside and it stays there (acceptance 1)', function () {
  // 🎯T252's picker, unaided, is the fault: the owner's selection is not in
  // the attention queue, so it answers with the head of a queue they never
  // asked about. Pinned here so the mechanism cannot be lost by accident.
  assert.strictEqual(
    AT.pickAttentionAsideSelection({
      attentionNames: [A], currentSelection: B, draftEmpty: true, reason: 'poll',
    }), A,
    'precondition: the T252 poll branch still proposes the queue head');

  // The owner clicked B, so B is owner-pinned.
  let state = { selected: B, origin: FS_.OWNER };
  // 5–15 seconds of the agents_changed burst is ~100 repaints at the 120ms
  // coalesce floor. Not one of them may move the selection.
  for (let i = 0; i < 100; i++) {
    state = repaint(state, tree(), [A]);
    assert.strictEqual(state.selected, B, 'repaint ' + i + ' moved the selection');
    assert.strictEqual(state.changed, false, 'repaint ' + i + ' reported a change');
  }
  assert.strictEqual(state.origin, FS_.OWNER, 'the pin survives the repaints');
});

test('a page-chosen selection is still advanceable (🎯T124 / 🎯T252 intact)', function () {
  const state = repaint({ selected: A, origin: FS_.AUTO }, tree(), [B]);
  assert.strictEqual(state.selected, B, 'auto selection follows the attention queue');
  assert.strictEqual(state.reason, 'auto-advance');
  assert.strictEqual(state.changed, true);
});

test('an empty selection is filled from the queue (the case T252 was written for)', function () {
  const state = repaint({ selected: null, origin: FS_.AUTO }, tree(), [A]);
  assert.strictEqual(state.selected, A);
  assert.strictEqual(state.reason, 'auto-fill');
});

// ── Acceptance 2: survives every kind of repaint ────────────────

test('selection survives add, remove, reorder and status change (acceptance 2)', function () {
  const pinned = { selected: B, origin: FS_.OWNER };

  const added = tree();
  added.unshift({ name: 'att-000new', purpose: 'aside', status: 'running' });
  assert.strictEqual(repaint(pinned, added, [A]).selected, B, 'a row added above');

  const removed = tree().filter(function (a) { return a.name !== 'jv-t383-auto'; });
  assert.strictEqual(repaint(pinned, removed, [A]).selected, B, 'an unrelated row reaped');

  const reordered = tree().reverse();
  assert.strictEqual(repaint(pinned, reordered, [A]).selected, B, 'rows reordered');

  const busy = tree().map(function (a) {
    return { name: a.name, purpose: a.purpose, status: 'stopped' };
  });
  assert.strictEqual(repaint(pinned, busy, [A]).selected, B, 'every status changed');

  // A fleet that changed so much only the selected row is recognisable.
  const churned = [{ name: B, purpose: 'aside', status: 'running' },
                   { name: 'brand-new-1', purpose: 'work', status: 'running' }];
  assert.strictEqual(repaint(pinned, churned, [A]).selected, B, 'wholesale churn');
});

// ── Acceptance 3: identity, not position ────────────────────────

test('the answer is identical under every permutation of the row list (acceptance 3)', function () {
  // If any index were consulted anywhere, some permutation would disagree.
  const rows = tree();
  const perms = [];
  (function permute(head, rest) {
    if (!rest.length) { perms.push(head); return; }
    for (let i = 0; i < rest.length; i++) {
      permute(head.concat([rest[i]]), rest.slice(0, i).concat(rest.slice(i + 1)));
    }
  }([], rows));
  assert.strictEqual(perms.length, 24, 'all 4! orderings');
  perms.forEach(function (p, i) {
    const owner = FS_.resolveSelection({
      selected: B, origin: FS_.OWNER, agents: p, candidate: A,
    });
    assert.deepStrictEqual(
      { s: owner.selected, c: owner.changed, r: owner.reason },
      { s: B, c: false, r: 'pinned' }, 'owner-pinned, permutation ' + i);
    const auto = FS_.resolveSelection({
      selected: B, origin: FS_.AUTO, agents: p, candidate: A,
    });
    assert.deepStrictEqual(
      { s: auto.selected, c: auto.changed, r: auto.reason },
      { s: A, c: true, r: 'auto-advance' }, 'auto-advance, permutation ' + i);
  });
});

test('isPresent is by name and reads plain strings or rows', function () {
  assert.ok(FS_.isPresent(tree(), B));
  assert.ok(FS_.isPresent(['x', 'y'], 'y'));
  assert.ok(!FS_.isPresent(tree(), 'att-bbb2'), 'no prefix match');
  assert.ok(!FS_.isPresent(tree(), ''), 'an empty name is never present');
  assert.ok(!FS_.isPresent(null, B), 'a missing list is not a crash');
});

// ── Acceptance 4: a vanished selection clears ───────────────────

test('the selected agent leaving the tree clears the selection (acceptance 4)', function () {
  const gone = tree().filter(function (a) { return a.name !== B; });
  const v = FS_.resolveSelection({
    selected: B, origin: FS_.OWNER, agents: gone, candidate: A,
  });
  assert.strictEqual(v.selected, null, 'cleared');
  assert.strictEqual(v.reason, 'gone');
  assert.strictEqual(v.changed, true, 'the caller must repaint');
  assert.notStrictEqual(v.selected, A,
    'a reaped agent must never be silently swapped for another one');
});

test('a reaped auto selection clears too — it is not advanced onto a stranger', function () {
  const gone = tree().filter(function (a) { return a.name !== B; });
  const v = FS_.resolveSelection({
    selected: B, origin: FS_.AUTO, agents: gone, candidate: A,
  });
  assert.strictEqual(v.selected, null);
  assert.strictEqual(v.reason, 'gone');
});

test('a clear un-pins, so the next auto pass may fill the empty selection', function () {
  const gone = tree().filter(function (a) { return a.name !== B; });
  const cleared = FS_.resolveSelection({
    selected: B, origin: FS_.OWNER, agents: gone, candidate: null,
  });
  assert.strictEqual(cleared.origin, FS_.AUTO);
  const next = FS_.resolveSelection({
    selected: cleared.selected, origin: cleared.origin, agents: gone, candidate: A,
  });
  assert.strictEqual(next.selected, A);
  assert.strictEqual(next.reason, 'auto-fill');
});

// ── Candidate hygiene ───────────────────────────────────────────

test('a candidate that is not in the incoming tree is never applied', function () {
  const v = FS_.resolveSelection({
    selected: A, origin: FS_.AUTO, agents: tree(), candidate: 'att-ghost',
  });
  assert.strictEqual(v.selected, A);
  assert.strictEqual(v.reason, 'sticky');

  const empty = FS_.resolveSelection({
    selected: null, origin: FS_.AUTO, agents: tree(), candidate: 'att-ghost',
  });
  assert.strictEqual(empty.selected, null);
  assert.strictEqual(empty.reason, 'idle');
});

test('no context at all is idle, not a throw', function () {
  const v = FS_.resolveSelection();
  assert.deepStrictEqual(
    { s: v.selected, c: v.changed, r: v.reason },
    { s: null, c: false, r: 'idle' });
});

test('an unknown origin string is treated as auto, never as a pin', function () {
  const v = FS_.resolveSelection({
    selected: B, origin: 'somebody-elses-idea', agents: tree(), candidate: A,
  });
  assert.strictEqual(v.selected, A, 'only the literal OWNER pins');
});

// ── backgroundMayMove: the guard for direct-select call sites ───

test('backgroundMayMove refuses to move an owner selection and nothing else', function () {
  assert.strictEqual(FS_.backgroundMayMove({ selected: B, origin: FS_.OWNER }), false);
  assert.strictEqual(FS_.backgroundMayMove({ selected: B, origin: FS_.AUTO }), true);
  assert.strictEqual(FS_.backgroundMayMove({ selected: null, origin: FS_.OWNER }), true,
    'an empty selection is always fillable');
  assert.strictEqual(FS_.backgroundMayMove({}), true);
  assert.strictEqual(FS_.backgroundMayMove(), true);
});

// ── index.html wiring (red against the pre-fix tree) ────────────

test('index.html loads fleet_selection.js before the fleet refresh tail', function () {
  const tag = html.indexOf('scripts/fleet_selection.js');
  assert.ok(tag >= 0, 'index.html must load web/scripts/fleet_selection.js');
  assert.ok(tag < html.indexOf('function refreshAgents'),
    'the module tag must precede the inline script that uses it');
});

test('the fleet refresh runs its candidate through resolveSelection', function () {
  const at = html.indexOf('FleetSelection.resolveSelection');
  assert.ok(at >= 0, 'refreshAgents must consult the policy');
  const near = html.slice(at, at + 700);
  assert.ok(/selected:\s*selectedAgent/.test(near), 'current selection passed');
  assert.ok(/origin:\s*selectionOrigin/.test(near), 'origin passed');
  assert.ok(/agents:\s*agents/.test(near), 'the incoming registry rows passed');
  assert.ok(/candidate:\s*wantSelect/.test(near), 'the auto-select proposal passed');
  // The verdict must actually gate the apply — not be computed and dropped.
  assert.ok(/autoSelected\s*=/.test(near) && /wantSelect\s*=/.test(near),
    'the verdict must overwrite the apply decision');
});

test('a vanished selection is cleared on the paint tail, not substituted', function () {
  const at = html.indexOf('clearSelection && selectedAgent');
  assert.ok(at >= 0, 'the paint tail must handle the gone case');
  const near = html.slice(at, at + 500);
  assert.ok(/selectAgent\(selectedAgent\)/.test(near),
    'clearing goes through selectAgent so the pane closes and the draft is stashed');
  assert.ok(near.indexOf('clearSelection') < near.indexOf('autoSelected'),
    'the gone case must be decided before any auto-select apply');
});

test('every background select marks itself auto; owner paths do not', function () {
  // The two background call sites pass {auto:true} so they stay advanceable.
  const bg = html.match(/selectAgent\([^)]*\{\s*auto:\s*true\s*\}\)/g) || [];
  assert.ok(bg.length >= 2,
    'both the refresh tail and attention auto-activate must select as auto');
  // Owner paths must not: bindFleetRow's name-click is the canonical one
  // (🎯T285.2 wraps the handler so the icon can open the migrate menu, but
  // the select itself stays a bare selectAgent(d.key) — never {auto:true}).
  const bindAt = html.indexOf('function bindFleetRow');
  assert.ok(bindAt >= 0, 'bindFleetRow missing');
  const bind = html.slice(bindAt, bindAt + 1600);
  assert.ok(/selectAgent\(d\.key\)/.test(bind),
    'a row click stays a bare selectAgent — owner input, hence pinned');
  assert.ok(!/selectAgent\(d\.key\s*,/.test(bind),
    'a row click must not pass {auto:true}');
});

test('selectAgent records who chose the selection', function () {
  const at = html.indexOf('selectionOrigin = (!next');
  assert.ok(at >= 0, 'selectAgent must set the origin');
  const near = html.slice(at, at + 200);
  assert.ok(/opts\s*&&\s*opts\.auto/.test(near), 'opts.auto marks a page choice');
  assert.ok(/SEL_OWNER/.test(near), 'everything else is owner input');
  assert.ok(/selectionOrigin = SEL_AUTO/.test(html),
    'an emptied selection must be fillable again');
});

test('attention auto-activate is gated by backgroundMayMove', function () {
  const at = html.indexOf('FleetSelection.backgroundMayMove');
  assert.ok(at >= 0, 'maybeAutoActivateAttention must consult the guard');
  const fn = html.indexOf('function maybeAutoActivateAttention');
  assert.ok(fn >= 0 && at > fn && at - fn < 900,
    'the guard belongs inside maybeAutoActivateAttention');
  const near = html.slice(at, at + 200);
  assert.ok(/return false/.test(near), 'a refused move must not fall through');
  assert.ok(at < html.indexOf('const draftEmpty = isSidebarComposerDraftEmpty()'),
    'the guard runs before the draft check — an owner pin outranks a cleared draft');
});

test('the 🎯T370 cycle chord still selects as owner input', function () {
  const at = html.indexOf('plan.select');
  assert.ok(at >= 0, 'the cycle apply path is still here');
  const near = html.slice(at, at + 200);
  assert.ok(/selectAgent\(plan\.target\)/.test(near),
    'the chord must not pass {auto:true} — a chord is the owner choosing');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nAll fleet_selection tests passed.');
