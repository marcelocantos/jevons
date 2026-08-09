// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for the 🎯T369 decision-matrix model: which markdown tables
// become a selectable choice card, which are left alone, and whether a
// selection survives a re-render.
//
//   node web/scripts/decision_matrix_test.js

'use strict';

const assert = require('assert');
const DM = require('./decision_matrix.js');

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed++;
    console.log('  ✓', name);
  } catch (e) {
    console.error('  ✗', name);
    console.error('   ', e && e.message);
    process.exitCode = 1;
  }
}

// The shape the owner screenshotted: 🎯T364 A/B/C with a recommended column.
const T364 = {
  title: 'T364 — how does the owner reach the control plane?',
  headers: ['Option', 'Approach', 'Cost', 'Risk', 'Recommended'],
  rows: [
    ['A', 'CLI-first entry', 'low', 'stale binary', ''],
    ['B', 'control-plane allowlist', 'medium', 'scope creep', '✅'],
    ['C', 'force-pause main session', 'low', 'owner surprise', ''],
  ],
};

console.log('decision_matrix: option cells');

test('bare key, parenthesised key, emphasised key', () => {
  assert.deepStrictEqual(DM.parseOptionCell('A'), { key: 'A', label: '' });
  assert.deepStrictEqual(DM.parseOptionCell('(b)'), { key: 'B', label: '' });
  assert.deepStrictEqual(DM.parseOptionCell('**C**'), { key: 'C', label: '' });
  assert.deepStrictEqual(DM.parseOptionCell('Option A'), { key: 'A', label: '' });
  assert.deepStrictEqual(DM.parseOptionCell('2.'), { key: '2', label: '' });
});

test('key plus label across separators', () => {
  assert.deepStrictEqual(DM.parseOptionCell('A — CLI-first entry'),
    { key: 'A', label: 'CLI-first entry' });
  assert.deepStrictEqual(DM.parseOptionCell('B: allowlist'),
    { key: 'B', label: 'allowlist' });
  assert.deepStrictEqual(DM.parseOptionCell('3. force-pause'),
    { key: '3', label: 'force-pause' });
  assert.deepStrictEqual(DM.parseOptionCell('Option C force-pause'),
    { key: 'C', label: 'force-pause' });
});

test('a bare word is not option "A" with a chopped label', () => {
  assert.strictEqual(DM.parseOptionCell('Approach'), null);
  assert.strictEqual(DM.parseOptionCell('Yes please'), null);
  assert.strictEqual(DM.parseOptionCell(''), null);
});

console.log('decision_matrix: key sequence gate');

test('consecutive letters and digits pass; gaps and dupes fail', () => {
  assert.strictEqual(DM.isConsecutiveKeys(['A', 'B', 'C']), true);
  assert.strictEqual(DM.isConsecutiveKeys(['1', '2']), true);
  assert.strictEqual(DM.isConsecutiveKeys(['A', 'C']), false);
  assert.strictEqual(DM.isConsecutiveKeys(['B', 'C']), false);
  assert.strictEqual(DM.isConsecutiveKeys(['A', 'A']), false);
  assert.strictEqual(DM.isConsecutiveKeys(['A']), false);
  assert.strictEqual(DM.isConsecutiveKeys(['A', '2']), false);
});

console.log('decision_matrix: table detection');

test('row-oriented A/B/C matrix parses with labels, details, recommendation', () => {
  const m = DM.parseTable(T364);
  assert.ok(m, 'expected a model');
  assert.strictEqual(m.orientation, 'rows');
  assert.strictEqual(m.options.length, 3);
  assert.deepStrictEqual(m.options.map((o) => o.key), ['A', 'B', 'C']);
  assert.strictEqual(m.options[0].label, 'CLI-first entry');
  assert.strictEqual(m.recommendedKey, 'B');
  assert.strictEqual(m.options[1].recommended, true);
  assert.strictEqual(m.options[0].recommended, false);
  // Option/label/recommended columns are chrome; the rest stay as detail.
  assert.deepStrictEqual(m.options[0].cells,
    [{ name: 'Cost', value: 'low' }, { name: 'Risk', value: 'stale binary' }]);
  assert.ok(m.id, 'model must carry an id');
});

test('label carried in the option cell itself', () => {
  const m = DM.parseTable({
    headers: ['Choice', 'Notes'],
    rows: [['A — keep polling', 'cheap'], ['B — push events', 'more plumbing']],
  });
  assert.ok(m);
  assert.deepStrictEqual(m.options.map((o) => o.label), ['keep polling', 'push events']);
});

test('inline ✅ marks the recommendation with no recommended column', () => {
  const m = DM.parseTable({
    headers: ['Option', 'Approach'],
    rows: [['A', 'poll'], ['B', 'push ✅']],
  });
  assert.ok(m);
  assert.strictEqual(m.recommendedKey, 'B');
});

test('column-oriented matrix (options as headers) parses', () => {
  const m = DM.parseTable({
    title: 'Transport',
    headers: ['', 'A — poll', 'B — push', 'C — hybrid'],
    rows: [
      ['Cost', 'low', 'medium', 'high'],
      ['Recommended', '', '✅', ''],
    ],
  });
  assert.ok(m);
  assert.strictEqual(m.orientation, 'columns');
  assert.deepStrictEqual(m.options.map((o) => o.key), ['A', 'B', 'C']);
  assert.strictEqual(m.options[1].label, 'push');
  assert.strictEqual(m.recommendedKey, 'B');
  // The Recommended row is chrome, not a detail line.
  assert.deepStrictEqual(m.options[0].cells, [{ name: 'Cost', value: 'low' }]);
});

test('ordinary comparison tables are left alone', () => {
  // A false positive is worse than a missed matrix: these must all be null.
  assert.strictEqual(DM.parseTable({
    headers: ['Approach', 'Cost'],
    rows: [['Polling', 'low'], ['Push', 'high']],
  }), null, 'plain word rows');
  assert.strictEqual(DM.parseTable({
    headers: ['Target', 'Status'],
    rows: [['T364', 'active'], ['T369', 'identified']],
  }), null, 'target listing');
  assert.strictEqual(DM.parseTable({
    headers: ['Option', 'Approach'],
    rows: [['A', 'only one']],
  }), null, 'a single option is not a choice');
  assert.strictEqual(DM.parseTable({
    headers: ['Option', 'Approach'],
    rows: [['A', 'poll'], ['C', 'push']],
  }), null, 'non-consecutive keys');
  assert.strictEqual(DM.parseTable({ headers: [], rows: [] }), null, 'empty');
});

console.log('decision_matrix: identity');

test('same matrix re-rendered keeps its id; edited matrix gets a new one', () => {
  const a = DM.parseTable(T364);
  const b = DM.parseTable(JSON.parse(JSON.stringify(T364)));
  assert.strictEqual(a.id, b.id);
  const changed = JSON.parse(JSON.stringify(T364));
  changed.rows[2][1] = 'force-pause everything';
  assert.notStrictEqual(DM.parseTable(changed).id, a.id);
});

test('id ignores detail-cell churn but tracks the question', () => {
  const a = DM.parseTable(T364);
  const costChanged = JSON.parse(JSON.stringify(T364));
  costChanged.rows[0][2] = 'very low';
  assert.strictEqual(DM.parseTable(costChanged).id, a.id);
  const retitled = JSON.parse(JSON.stringify(T364));
  retitled.title = 'Something else entirely';
  assert.notStrictEqual(DM.parseTable(retitled).id, a.id);
});

console.log('decision_matrix: durable selection');

test('record and read back a choice', () => {
  const m = DM.parseTable(T364);
  let store = DM.parseStore('');
  assert.strictEqual(DM.choiceFor(store, m.id), null);
  store = DM.recordChoice(store, m, 'C', 1700);
  const c = DM.choiceFor(store, m.id);
  assert.strictEqual(c.key, 'C');
  assert.strictEqual(c.label, 'force-pause main session');
  assert.strictEqual(c.sent, false);
  assert.strictEqual(c.at, 1700);
  // Round-trips through storage unchanged.
  assert.deepStrictEqual(DM.choiceFor(DM.parseStore(DM.serializeStore(store)), m.id), c);
});

test('recordChoice does not mutate the input store', () => {
  const m = DM.parseTable(T364);
  const store = DM.parseStore('');
  const next = DM.recordChoice(store, m, 'A', 1);
  assert.strictEqual(DM.choiceFor(store, m.id), null);
  assert.strictEqual(DM.choiceFor(next, m.id).key, 'A');
});

test('sent flag and re-selection overwrite in place', () => {
  const m = DM.parseTable(T364);
  let store = DM.recordChoice(DM.parseStore(''), m, 'A', 1);
  store = DM.recordChoice(store, m, 'B', 2, { sent: true });
  assert.strictEqual(Object.keys(store.choices).length, 1);
  assert.strictEqual(DM.choiceFor(store, m.id).key, 'B');
  assert.strictEqual(DM.choiceFor(store, m.id).sent, true);
});

test('unknown option key is not recorded', () => {
  const m = DM.parseTable(T364);
  const store = DM.recordChoice(DM.parseStore(''), m, 'Z', 1);
  assert.strictEqual(DM.choiceFor(store, m.id), null);
});

test('malformed storage reads as empty, never throws', () => {
  ['', '   ', 'not json', '[]', '{"choices":3}', 'null'].forEach((raw) => {
    const s = DM.parseStore(raw);
    assert.deepStrictEqual(s.choices, {}, 'raw: ' + JSON.stringify(raw));
  });
  // Entries without a key are dropped rather than poisoning the store.
  assert.deepStrictEqual(DM.parseStore('{"choices":{"x":{"label":"no key"}}}').choices, {});
});

test('store is capped, evicting oldest first', () => {
  let store = DM.parseStore('');
  for (let i = 0; i < DM.MAX_CHOICES + 5; i++) {
    const m = DM.parseTable({
      title: 'matrix ' + i,
      headers: ['Option', 'Approach'],
      rows: [['A', 'poll'], ['B', 'push']],
    });
    store = DM.recordChoice(store, m, 'A', i + 1);
  }
  const ids = Object.keys(store.choices);
  assert.strictEqual(ids.length, DM.MAX_CHOICES);
  const times = ids.map((id) => store.choices[id].at).sort((a, b) => a - b);
  assert.strictEqual(times[0], 6, 'the five oldest entries should be gone');
});

console.log('decision_matrix: owner-facing text');

test('reply names key, label, and how it relates to the recommendation', () => {
  const m = DM.parseTable(T364);
  assert.strictEqual(DM.replyText(m, 'B'),
    'Decision — T364 — how does the owner reach the control plane?: '
    + '**B** — control-plane allowlist (your recommended option).');
  assert.strictEqual(DM.replyText(m, 'A'),
    'Decision — T364 — how does the owner reach the control plane?: '
    + '**A** — CLI-first entry (not the recommended B — control-plane allowlist).');
  assert.strictEqual(DM.replyText(m, 'Z'), '');
});

test('reply omits the recommendation clause when nothing was recommended', () => {
  const m = DM.parseTable({
    title: 'Transport',
    headers: ['Option', 'Approach'],
    rows: [['A', 'poll'], ['B', 'push']],
  });
  assert.strictEqual(DM.replyText(m, 'A'), 'Decision — Transport: **A** — poll.');
});

test('status text distinguishes unselected, selected, and sent', () => {
  const m = DM.parseTable(T364);
  assert.strictEqual(DM.statusText(m, null), 'Pick one of 3 options');
  assert.strictEqual(DM.statusText(m, { key: 'A' }),
    'Selected A — CLI-first entry · not sent yet');
  assert.strictEqual(DM.statusText(m, { key: 'A', sent: true }),
    'Selected A — CLI-first entry · sent');
});

test('title falls back rather than rendering an anonymous card', () => {
  assert.strictEqual(DM.titleFrom('', ''), 'Design choice');
  assert.strictEqual(DM.titleFrom('**Which entry point?**:'), 'Which entry point?');
  assert.ok(DM.titleFrom('x'.repeat(200)).length <= 91);
  const m = DM.parseTable({ headers: ['Option', 'Approach'], rows: [['A', 'poll'], ['B', 'push']] });
  assert.strictEqual(m.title, 'Option');
});

console.log('\ndecision_matrix: ' + passed + ' passed');
