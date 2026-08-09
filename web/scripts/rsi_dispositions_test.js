// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic tests for the 🎯T354 RHS coach list model.
// Run: node web/scripts/rsi_dispositions_test.js
//
// Oracle for the honesty criteria: an empty store renders a calm empty
// state (not an error), a fetch failure renders an error (not "nothing
// found"), and pending / ignored / filed rows each carry the fields the
// owner needs to see what the coach judged and what was done with it.

'use strict';

const assert = require('assert');
const RD = require('./rsi_dispositions.js');
const FT = require('./frontier_table.js');

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

// Mirrors the shape GET /api/rsi/dispositions returns (server test covers
// that the Go side actually produces it).
const PAYLOAD = {
  total: 3,
  count: 3,
  pending: 1,
  path: '/tmp/state/rsi/dispositions.json',
  judgments: [
    {
      fingerprint: 'fp-pending',
      name: 'chat gap',
      observation: 'owner asked twice before a reply landed',
      severity: 'medium',
      delivered_at: '2026-08-09T03:00:00Z',
      disposition: 'pending',
      evidence: 'owner_chat:chatlog-2026-08-09 (chat_gap)',
    },
    {
      fingerprint: 'fp-ignored',
      name: 'phrase friction',
      observation: 'repeat phrasing in overseer replies',
      severity: 'low',
      delivered_at: '2026-08-09T02:00:00Z',
      disposition: 'ignore_with_reason',
      disposition_at: '2026-08-09T03:01:00Z',
      reason: 'one-off, no standing pattern',
    },
    {
      fingerprint: 'fp-filed',
      name: 'repair churn',
      observation: 'three follow-up commits on the same file',
      severity: 'high',
      mode: 'retro',
      delivered_at: '2026-08-09T01:00:00Z',
      disposition: 'file',
      disposition_at: '2026-08-09T03:02:00Z',
      target_id: 'T999',
    },
  ],
};

function rowsByFP(model) {
  const m = {};
  model.rows.forEach(function (r) { m[r.fingerprint] = r; });
  return m;
}

test('empty store is calm and empty, never an error', function () {
  const m = RD.normalizePayload({ judgments: [], total: 0, pending: 0, count: 0 });
  assert.strictEqual(m.empty, true);
  assert.strictEqual(m.error, '');
  assert.strictEqual(m.total, 0);
  assert.strictEqual(m.rows.length, 0);
  assert.ok(RD.emptyText().length > 0);
  // No counts line when there is nothing to count.
  assert.strictEqual(RD.countsText(m), '');
});

test('missing judgments array degrades to empty, not a crash', function () {
  const m = RD.normalizePayload(null);
  assert.strictEqual(m.empty, true);
  assert.strictEqual(m.error, '');
  assert.strictEqual(m.rows.length, 0);
});

test('fetch failure is a loud error, not "nothing found"', function () {
  const m = RD.normalizePayload(null, new Error('HTTP 500'));
  assert.strictEqual(m.error, 'HTTP 500');
  // The distinction the owner depends on: a failure must not read as empty.
  assert.strictEqual(m.empty, false);
  assert.strictEqual(m.rows.length, 0);
});

test('pending / ignored / filed shapes all reach the owner list', function () {
  const m = RD.normalizePayload(PAYLOAD);
  assert.strictEqual(m.total, 3);
  assert.strictEqual(m.pending, 1);
  assert.strictEqual(m.empty, false);
  assert.strictEqual(m.path, '/tmp/state/rsi/dispositions.json');
  const by = rowsByFP(m);

  const pending = by['fp-pending'];
  assert.strictEqual(pending.disposition, 'pending');
  assert.strictEqual(pending.dispositionLabel, 'pending');
  assert.strictEqual(pending.pending, true);
  assert.strictEqual(pending.severity, 'medium');
  assert.strictEqual(pending.title, 'chat gap');
  assert.ok(pending.deliveredAt > 0);
  assert.ok(pending.evidence.indexOf('chatlog-2026-08-09') >= 0);
  assert.strictEqual(RD.detailText(pending), '');

  const ignored = by['fp-ignored'];
  assert.strictEqual(ignored.dispositionLabel, 'ignored');
  assert.strictEqual(ignored.pending, false);
  // An ignore with no visible reason is indistinguishable from a drop.
  assert.ok(RD.detailText(ignored).indexOf('one-off') >= 0);

  const filed = by['fp-filed'];
  assert.strictEqual(filed.dispositionLabel, 'filed');
  assert.strictEqual(filed.targetID, 'T999');
  assert.strictEqual(filed.retro, true, 'T353 retro provenance must survive');
  assert.ok(RD.detailText(filed).indexOf('🎯T999') >= 0);

  assert.strictEqual(RD.countsText(m), '3 judgments · 1 pending');
});

test('rows sort newest delivery first, then severity', function () {
  const m = RD.normalizePayload(PAYLOAD);
  assert.deepStrictEqual(
    m.rows.map(function (r) { return r.fingerprint; }),
    ['fp-pending', 'fp-ignored', 'fp-filed']);

  // Same delivery instant → higher severity first, then fingerprint.
  const tied = RD.normalizePayload({
    judgments: [
      { fingerprint: 'b', severity: 'low', delivered_at: '2026-08-09T01:00:00Z' },
      { fingerprint: 'a', severity: 'high', delivered_at: '2026-08-09T01:00:00Z' },
    ],
  });
  assert.deepStrictEqual(
    tied.rows.map(function (r) { return r.fingerprint; }), ['a', 'b']);
});

test('blank / unknown disposition reads as pending', function () {
  const m = RD.normalizePayload({
    judgments: [
      { fingerprint: 'blank', delivered_at: '2026-08-09T01:00:00Z' },
      { fingerprint: 'bogus', disposition: 'nonsense', delivered_at: '2026-08-09T01:00:00Z' },
    ],
  });
  m.rows.forEach(function (r) {
    assert.strictEqual(r.disposition, 'pending', r.fingerprint);
    assert.strictEqual(r.pending, true, r.fingerprint);
  });
  // Derived when the server omits the count.
  assert.strictEqual(m.pending, 2);
});

test('Go zero time is absent, not 0001', function () {
  assert.strictEqual(RD.parseTime('0001-01-01T00:00:00Z'), 0);
  assert.strictEqual(RD.parseTime(''), 0);
  assert.strictEqual(RD.parseTime(undefined), 0);
  assert.ok(RD.parseTime('2026-08-09T03:00:00Z') > 0);
});

test('a judgment with no name still renders a title', function () {
  const r = RD.row({ fingerprint: 'x', observation: 'only an observation' });
  assert.strictEqual(r.title, 'only an observation');
  const bare = RD.row({ fingerprint: 'y' });
  assert.ok(bare.title.length > 0, 'never an anonymous blank row');
});

test('coach is a real RHS tab alongside frontier and transcript', function () {
  assert.strictEqual(FT.nextBottomTab('frontier', 'coach'), 'coach');
  assert.strictEqual(FT.nextBottomTab('coach', 'frontier'), 'frontier');
  // Sticky on unknown clicks, and never silently swallows the coach pane.
  assert.strictEqual(FT.nextBottomTab('coach', 'bogus'), 'coach');
  assert.strictEqual(FT.nextBottomTab('transcript', 'bogus'), 'transcript');
  assert.strictEqual(FT.nextBottomTab('frontier', 'bogus'), 'frontier');
});

if (failed) {
  console.error(failed + ' failed');
  process.exit(1);
}
console.log('All rsi_dispositions tests passed');
