// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T384: owner turns persist in the same shape as agent turns — browser half.
//
//   node web/scripts/owner_turn_shape_test.js
//
// The owner's complaint: type into a fleet aside, press Enter, the message
// seems to send and then disappears. Evidence from his own journal,
// ~/.jevons/agent-chatlogs/att-msln9k27-nf4y87.jsonl: every assistant record
// stores content as a list of typed blocks, and the single owner record stores
// it as a bare string. The display model read only the bare string, and the
// server wrote only the bare string, so the two agreed — until anything walked
// blocks, at which point the owner's own words were the one thing on the wire
// that could not be read.
//
// The server now writes owner turns as typed blocks. These tests pin the other
// half of the contract: the browser must read BOTH shapes. The block half is
// what stops the new wire from vanishing; the bare-string half is what stops
// the fix from stranding every journal already on disk.

'use strict';

const assert = require('assert');
const CE = require('./chat_events.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('  ok -', name);
  } catch (e) {
    failed++;
    console.error('  FAIL -', name);
    console.error('   ', e && e.stack ? e.stack.split('\n').slice(0, 8).join('\n    ') : e);
  }
}

console.log('owner_turn_shape_test (🎯T384)');

// The shape the server writes today — identical to an assistant turn's.
function blockUser(text) {
  return {
    type: 'user',
    timestamp: '2026-08-09T12:00:00.000Z',
    message: { role: 'user', content: [{ type: 'text', text: text }] },
  };
}

// The shape every chatlog on disk still holds.
function legacyUser(text) {
  return {
    type: 'user',
    timestamp: '2026-08-09T11:32:25.879763Z',
    message: { role: 'user', content: text },
  };
}

// ── userContentText: one reader, both shapes ────────────────────────────

test('userContentText reads the block shape the server now writes', function () {
  assert.strictEqual(CE.userContentText(blockUser('does this send?')), 'does this send?');
});

test('userContentText still reads the legacy bare string (acceptance 4)', function () {
  assert.strictEqual(
    CE.userContentText(legacyUser('park this. claude remote suffices for now.')),
    'park this. claude remote suffices for now.',
  );
});

test('userContentText joins multiple text blocks', function () {
  const m = {
    type: 'user',
    message: { role: 'user', content: [{ type: 'text', text: 'a' }, { type: 'text', text: 'b' }] },
  };
  assert.strictEqual(CE.userContentText(m), 'ab');
});

test('userContentText ignores non-text blocks and junk', function () {
  const m = {
    type: 'user',
    message: {
      role: 'user',
      content: [{ type: 'tool_result', content: 'x' }, null, { type: 'text', text: 'kept' }],
    },
  };
  assert.strictEqual(CE.userContentText(m), 'kept');
  assert.strictEqual(CE.userContentText(null), '');
  assert.strictEqual(CE.userContentText({}), '');
  assert.strictEqual(CE.userContentText({ message: {} }), '');
});

// ── The vanish itself: the aside/sidebar display model ──────────────────

// RED before the fix: applyLiveDisplayFrame did
//   const text = typeof content === 'string' ? content : '';
// so a block-shaped owner turn produced '' and returned the line list
// unchanged — the owner's message never reached the pane. This is the exact
// mechanism behind "I type, press Enter, it seems to send, then it disappears".
test('a block-shaped owner turn reaches the aside pane', function () {
  const lines = CE.applyLiveDisplayFrame([], blockUser('does this survive a reload?'), {});
  assert.strictEqual(lines.length, 1, 'owner turn was dropped by the display model');
  assert.strictEqual(lines[0].role, 'user');
  assert.strictEqual(lines[0].text, 'does this survive a reload?');
});

test('a legacy bare-string owner turn still reaches the pane', function () {
  const lines = CE.applyLiveDisplayFrame([], legacyUser('park this.'), {});
  assert.strictEqual(lines.length, 1, 'legacy owner history stopped rendering');
  assert.strictEqual(lines[0].text, 'park this.');
});

test('owner turn and agent reply both paint, in order', function () {
  let lines = CE.applyLiveDisplayFrame([], blockUser('ping'), {});
  lines = CE.applyLiveDisplayFrame(lines, {
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text: 'pong' }], stop_reason: 'end_turn' },
  }, {});
  const shape = lines.map(l => l.role + ':' + l.text);
  assert.deepStrictEqual(shape, ['user:ping', 'assistant:pong']);
});

// A mixed journal — the realistic state of every existing chatlog once the
// server starts writing blocks: old bare-string turns followed by new
// block-shaped ones. Both must render, and neither may swallow the other.
test('a mixed legacy/new journal hydrates every owner turn', function () {
  const frames = [
    legacyUser('first message, written before the fix'),
    { type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'ack' }], stop_reason: 'end_turn' } },
    blockUser('second message, written after the fix'),
  ];
  const lines = CE.coalesceLiveDisplayFrames(frames, {});
  const userTexts = lines.filter(l => l.role === 'user').map(l => l.text);
  assert.deepStrictEqual(userTexts, [
    'first message, written before the fix',
    'second message, written after the fix',
  ]);
});

// ── Provenance and dedupe keep working across the shape change ──────────

test('consecutive duplicate owner echo dedupes across shapes', function () {
  // Optimistic paint (bare string, from the browser) then the server's
  // block-shaped echo of the same words must not double-paint.
  let lines = CE.applyLiveDisplayFrame([], legacyUser('same words'), {});
  lines = CE.applyLiveDisplayFrame(lines, blockUser('same words'), {});
  assert.strictEqual(lines.length, 1, 'shape change reintroduced a double owner bubble');
});

test('an empty block list is not an owner turn', function () {
  const m = { type: 'user', message: { role: 'user', content: [] } };
  const lines = CE.applyLiveDisplayFrame([], m, {});
  assert.strictEqual(lines.length, 0);
});

if (failed) {
  console.error(failed + ' test(s) failed');
  process.exit(1);
}
console.log('owner_turn_shape_test: all passed');
