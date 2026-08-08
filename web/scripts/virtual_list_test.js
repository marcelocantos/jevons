// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracles for 🎯T119 shell+content transcript windowing.
// Acceptance bullets map 1:1 to named tests below.

'use strict';

const assert = require('assert');
const VL = require('./virtual_list.js');

function test(name, fn) {
  try {
    fn();
    console.log('ok - ' + name);
  } catch (e) {
    console.error('not ok - ' + name);
    console.error(e);
    process.exitCode = 1;
  }
}

// ── Legacy T56 band helpers (still hold) ─────────────────────────────

test('visibleIndices returns only near-viewport items', function () {
  const tops = [];
  for (let i = 0; i < 100; i++) tops.push({ top: i * 40, height: 40 });
  const vis = VL.visibleIndices(tops, 2000, 400, 200);
  assert.ok(vis.length < 30, 'bounded visible set, got ' + vis.length);
  assert.ok(vis[0] > 40, 'starts mid-list');
  assert.ok(vis[vis.length - 1] < 70, 'ends mid-list');
});

test('materialisedCount grows much slower than N', function () {
  const n = 500;
  const mat = VL.materialisedCount(n, 48, 600, 800);
  assert.ok(mat < 80, 'materialised ' + mat + ' for N=' + n);
  assert.ok(mat > 5, 'some materialised');
});

test('shouldMaterialize edge cases', function () {
  assert.strictEqual(VL.shouldMaterialize(0, 100, 0, 300, 0), true);
  assert.strictEqual(VL.shouldMaterialize(5000, 100, 0, 300, 0), false);
});

// ── 🎯T246: stay expanded while any part on-screen ───────────────────

test('T246 anyPartInViewport: partial overlap stays material', function () {
  // Fully inside
  assert.strictEqual(VL.anyPartInViewport(100, 50, 0, 300), true);
  // Top edge just inside
  assert.strictEqual(VL.anyPartInViewport(299, 50, 0, 300), true);
  // Bottom edge just inside (1px above scrollTop still counts)
  assert.strictEqual(VL.anyPartInViewport(-49, 50, 0, 300), true);
  // Fully above fold
  assert.strictEqual(VL.anyPartInViewport(0, 100, 100, 300), false);
  assert.strictEqual(VL.isFullyAboveViewport(0, 100, 100), true);
  // Fully below fold
  assert.strictEqual(VL.anyPartInViewport(400, 50, 0, 300), false);
  // Zero height at boundary: not intersecting (bot === scrollTop)
  assert.strictEqual(VL.anyPartInViewport(100, 0, 100, 300), false);
});

test('T246 shouldAutoCollapseOffScreen: only when fully off-screen, not latest', function () {
  // Latest never auto-collapses
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: true, autoExpanded: true, userToggled: false,
    top: 0, height: 100, scrollTop: 500, clientHeight: 300,
  }), false);
  // Manual toggle never auto-collapses
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, autoExpanded: true, userToggled: true,
    top: 0, height: 100, scrollTop: 500, clientHeight: 300,
  }), false);
  // Not auto-expanded → no auto-collapse action
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, autoExpanded: false, userToggled: false,
    top: 0, height: 100, scrollTop: 500, clientHeight: 300,
  }), false);
  // Still partially in viewport → keep expanded
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, autoExpanded: true, userToggled: false,
    top: 250, height: 100, scrollTop: 0, clientHeight: 300,
  }), false);
  // Fully above fold → may collapse
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, autoExpanded: true, userToggled: false,
    top: 0, height: 100, scrollTop: 200, clientHeight: 300,
  }), true);
  // Fully below fold → may collapse
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, autoExpanded: true, userToggled: false,
    top: 800, height: 100, scrollTop: 0, clientHeight: 300,
  }), true);
});

// ── 🎯T261: near-end in-view non-collapse ────────────────────────────

test('T261 isNearTranscriptEnd: pin-bottom and mid-scroll', function () {
  // scrollHeight 1000, client 300 → max scrollTop 700
  assert.strictEqual(VL.isNearTranscriptEnd(700, 1000, 300), true);
  assert.strictEqual(VL.isNearTranscriptEnd(680, 1000, 300, 48), true); // within slack
  assert.strictEqual(VL.isNearTranscriptEnd(600, 1000, 300, 48), false);
  assert.strictEqual(VL.isNearTranscriptEnd(0, 1000, 300), false);
  // Empty / short list (no overflow)
  assert.strictEqual(VL.isNearTranscriptEnd(0, 200, 300), true);
});

test('T261 shouldAutoExpandInView: only tall + near-end + in viewport', function () {
  // In view near end → expand
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: true, userToggled: false, historyReplayActive: false,
    top: 700, height: 100, scrollTop: 600, clientHeight: 300,
  }), true);
  // Not tall → no
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: false, nearEnd: true, userToggled: false,
    top: 700, height: 100, scrollTop: 600, clientHeight: 300,
  }), false);
  // Mid-history free scroll (not near end) → no force-expand
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: false, userToggled: false,
    top: 100, height: 100, scrollTop: 80, clientHeight: 300,
  }), false);
  // Manual toggle wins
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: true, userToggled: true,
    top: 700, height: 100, scrollTop: 600, clientHeight: 300,
  }), false);
  // Fully above fold even near end → no
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: true, userToggled: false,
    top: 0, height: 100, scrollTop: 600, clientHeight: 300,
  }), false);
  // Mid history-replay → no expand until pin
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: true, userToggled: false, historyReplayActive: true,
    top: 700, height: 100, scrollTop: 600, clientHeight: 300,
  }), false);
});

test('T261 shouldRunOffScreenCollapse: suppressed during history replay', function () {
  assert.strictEqual(VL.shouldRunOffScreenCollapse(true), false);
  assert.strictEqual(VL.shouldRunOffScreenCollapse(false), true);
  assert.strictEqual(VL.shouldRunOffScreenCollapse(0), true);
});

// ── recent-first hydrate ─────────────────────────────────────────────

test('T119 recent-first hydrate lands on latest end', function () {
  const n = 200;
  const plan = VL.recentFirstMaterializePlan(n, 48, 600, 800);
  assert.strictEqual(plan.landsOnLatest, true);
  assert.ok(plan.materialIndices.indexOf(n - 1) >= 0, 'last index material');
  assert.ok(plan.materialIndices.indexOf(0) < 0, 'oldest not in first paint band');
  assert.ok(plan.unmeasuredIndices.length > 0, 'older shells deferred');
  assert.ok(plan.scrollTop > 0, 'scroll pinned toward end');
});

test('T119 lazy measure: no render-all-then-strip at startup', function () {
  const n = 400;
  const budget = VL.startupMaterializeBudget(n, 48, 600, 800);
  assert.ok(budget.mustMaterialize < 80, 'bounded first paint, got ' + budget.mustMaterialize);
  assert.ok(budget.mayDefer > 300, 'most shells deferred, got ' + budget.mayDefer);
  assert.strictEqual(budget.total, n);
  assert.ok(budget.mustMaterialize + budget.mayDefer === n);
});

// ── whole-chunk only ─────────────────────────────────────────────────

test('T119 whole-chunk only: consecutive assistant frames coalesce', function () {
  const frames = [
    { type: 'user', message: { content: 'hello' }, timestamp: '2026-01-01T00:00:00Z' },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: '# Title\n\n' }] },
      timestamp: '2026-01-01T00:00:01Z',
    },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'body paragraph' }] },
      timestamp: '2026-01-01T00:00:02Z',
    },
    { type: 'user', message: { content: 'next' } },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'reply two' }] },
    },
  ];
  const chunks = VL.coalesceTranscriptFrames(frames);
  assert.strictEqual(chunks.length, 4);
  assert.strictEqual(chunks[0].role, 'user');
  assert.strictEqual(chunks[0].text, 'hello');
  assert.strictEqual(chunks[1].role, 'jevons');
  assert.strictEqual(chunks[1].text, '# Title\n\nbody paragraph');
  assert.strictEqual(chunks[2].text, 'next');
  assert.strictEqual(chunks[3].text, 'reply two');
  chunks.forEach(function (c) {
    assert.ok(VL.isWholeChunk(c), 'chunk must be whole');
  });
});

test('T119 whole-chunk only: rejects partial/incomplete stubs', function () {
  assert.strictEqual(VL.isWholeChunk({ role: 'jevons', text: 'ok' }), true);
  assert.strictEqual(VL.isWholeChunk({ role: 'jevons', text: 'x', partial: true }), false);
  assert.strictEqual(VL.isWholeChunk({ role: 'jevons', text: 'x', incomplete: true }), false);
  assert.strictEqual(VL.isWholeChunk({ role: 'assistant', text: 'x' }), false);
  assert.strictEqual(VL.isWholeChunk({ role: 'user', text: '' }), false);
  const filtered = VL.filterWholeChunks([
    { role: 'user', text: 'a' },
    { role: 'jevons', text: 'b', partial: true },
    { role: 'jevons', text: 'c' },
  ]);
  assert.strictEqual(filtered.length, 2);
});

test('T119 whole-chunk only: tool frames do not become display units', function () {
  const chunks = VL.coalesceTranscriptFrames([
    { type: 'user', message: { content: 'go' } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'x', input: {} }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: 'done' }] } },
  ]);
  assert.strictEqual(chunks.length, 2);
  assert.strictEqual(chunks[1].text, 'done');
});

// ── 🎯T161 structural segment edges in coalesceTranscriptFrames ────────

test('T161 continuous text frames bare-concat (intra-segment tokens)', function () {
  // No tool gap → consecutive assistant text frames are stream tokens.
  const chunks = VL.coalesceTranscriptFrames([
    { type: 'user', message: { content: 'hi' } },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'Hel' }] },
    },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'lo.' }] },
    },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'What' }] },
    },
  ]);
  assert.strictEqual(chunks.length, 2);
  assert.strictEqual(chunks[1].text, 'Hello.What');
});

test('T161 tool_use gap then next text uses structural segment join', function () {
  const chunks = VL.coalesceTranscriptFrames([
    { type: 'user', message: { content: 'two paras' } },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'First paragraph ends here.' }] },
    },
    {
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'read_file', input: {} }] },
    },
    {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'Second paragraph starts here.' }] },
    },
  ]);
  assert.strictEqual(chunks.length, 2);
  assert.strictEqual(
    chunks[1].text,
    'First paragraph ends here.\n\nSecond paragraph starts here.',
  );
});

test('T161 tool gap then fence/list uses same structural path (no content sniff)', function () {
  const fence = VL.coalesceTranscriptFrames([
    { type: 'assistant', message: { content: [{ type: 'text', text: 'Intro.' }] } },
    { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'x', input: {} }] } },
    { type: 'assistant', message: { content: [{ type: 'text', text: '```cpp\ncode\n```' }] } },
  ]);
  assert.strictEqual(fence[0].text, 'Intro.\n\n```cpp\ncode\n```');

  const list = VL.coalesceTranscriptFrames([
    { type: 'assistant', message: { content: [{ type: 'text', text: 'Items:' }] } },
    { type: 'tool_result', content: [{ type: 'text', text: 'ok' }] },
    { type: 'assistant', message: { content: [{ type: 'text', text: '- a\n- b' }] } },
  ]);
  assert.strictEqual(list[0].text, 'Items:\n\n- a\n- b');
});

// ── 🎯T250 aside wires never become main transcript chunks ──────────

test('T250 coalesce skips attention and target-aside user frames', function () {
  const AT = require('./attention_threads.js');
  // Ensure VL can see AttentionThreads when running under Node (no browser global).
  global.AttentionThreads = AT;
  const frames = [
    { type: 'user', message: { content: 'main hello' } },
    {
      type: 'user',
      message: { content: '[attention:att-x|billing]\nbilling body' },
    },
    {
      type: 'assistant',
      message: {
        content: [{ type: 'text', text: 'main reply' }],
        stop_reason: 'end_turn',
      },
    },
    {
      type: 'user',
      message: {
        content: '[target-aside: att-y | File this]\nbody\n\n(Ceremony: x)',
      },
    },
    {
      type: 'user',
      message: { content: 'still main' },
    },
  ];
  const chunks = VL.coalesceTranscriptFrames(frames);
  const userTexts = chunks.filter(function (c) { return c.role === 'user'; }).map(function (c) {
    return c.text;
  });
  assert.deepStrictEqual(userTexts, ['main hello', 'still main'],
    'aside wires excluded from main chunks: ' + JSON.stringify(userTexts));
  assert.ok(chunks.some(function (c) {
    return c.role === 'jevons' && c.text === 'main reply';
  }), 'assistant main reply still present');
  // Sidebar path still extracts them from the same fixture.
  const cache = AT.extractAsideWireTurnsFromFrames(frames);
  assert.ok(cache['att-x'] && cache['att-x'][0].text === 'billing body');
  assert.ok(cache['att-y'] && cache['att-y'][0].text.indexOf('body') === 0);
});

// 🎯T264: image-prefix + attention wire must not flash into main chunks.
test('T264 coalesce skips image-prefixed attention wire (flash class)', function () {
  const AT = require('./attention_threads.js');
  global.AttentionThreads = AT;
  const incident =
    '[attention:att-msftck4l-9sguxj|how does bullseye compare to beads?]\n' +
    'how does bullseye compare to beads?';
  const frames = [
    { type: 'user', message: { content: 'main hello' } },
    { type: 'user', message: { content: incident } },
    {
      type: 'user',
      message: {
        content: '[image: abc123]\n[attention:att-x|t]\nbody',
      },
    },
    { type: 'user', message: { content: 'still main' } },
  ];
  const chunks = VL.coalesceTranscriptFrames(frames);
  const userTexts = chunks.filter(function (c) { return c.role === 'user'; }).map(function (c) {
    return c.text;
  });
  assert.deepStrictEqual(userTexts, ['main hello', 'still main'],
    'T264 flash wires excluded: ' + JSON.stringify(userTexts));
  assert.ok(userTexts.every(function (t) {
    return t.indexOf('[attention:') < 0 && t.indexOf('[target-aside:') < 0;
  }), 'no raw wire marker prefix in main chunks');
});

// ── 🎯T245 silent-only turns must not leak/coalesce into next bubble ──

test('T245 pure [silent] + agent_note + visible → only second body', function () {
  // Owner screenshot regression: two ACP turns with only agent_note between
  // must not glue silent ops chatter onto the next owner-visible reply.
  const chunks = VL.coalesceTranscriptFrames([
    {
      type: 'assistant',
      stream_id: '35728c40',
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: '[silent] PO already re-pressured jv-t244; no further action.' }],
        stop_reason: 'end_turn',
      },
    },
    { type: 'agent_note', text: '[Agent jevons-po responded] Independent gate…' },
    {
      type: 'assistant',
      stream_id: '9cb609c1',
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: '**🎯T244 landed.**\n\nIndependent check.' }],
        stop_reason: 'end_turn',
      },
    },
  ]);
  assert.strictEqual(chunks.length, 1, 'only visible turn: ' + JSON.stringify(chunks));
  assert.strictEqual(chunks[0].role, 'jevons');
  assert.strictEqual(chunks[0].text, '**🎯T244 landed.**\n\nIndependent check.');
  assert.ok(chunks[0].text.indexOf('[silent]') < 0, 'silent must not leak');
  assert.ok(chunks[0].text.indexOf('re-pressured') < 0, 'silent body must not glue');
});

test('T245 empty sealed silent + agent_note + visible → only second body', function () {
  // Journal path after T240: silent stream is empty body + end_turn.
  const chunks = VL.coalesceTranscriptFrames([
    {
      type: 'assistant',
      stream_id: '35728c40',
      message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
    },
    { type: 'agent_note', text: 'note' },
    {
      type: 'assistant',
      stream_id: '9cb609c1',
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: '**🎯T244 landed.**' }],
        stop_reason: 'end_turn',
      },
    },
  ]);
  assert.strictEqual(chunks.length, 1);
  assert.strictEqual(chunks[0].text, '**🎯T244 landed.**');
});

test('T245 two visible turns with only agent_note between stay two bubbles', function () {
  // Terminal seal + stream_id: consecutive overseer turns must not glue.
  const chunks = VL.coalesceTranscriptFrames([
    {
      type: 'assistant',
      stream_id: 's-a',
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: 'First turn.' }],
        stop_reason: 'end_turn',
      },
    },
    { type: 'agent_note', text: 'worker note' },
    {
      type: 'assistant',
      stream_id: 's-b',
      message: {
        role: 'assistant',
        content: [{ type: 'text', text: 'Second turn.' }],
        stop_reason: 'end_turn',
      },
    },
  ]);
  assert.strictEqual(chunks.length, 2, JSON.stringify(chunks));
  assert.strictEqual(chunks[0].text, 'First turn.');
  assert.strictEqual(chunks[1].text, 'Second turn.');
});

test('T245 multi-fragment silent stream stripped before visible', function () {
  const chunks = VL.coalesceTranscriptFrames([
    {
      type: 'assistant',
      stream_id: 's9',
      message: { content: [{ type: 'text', text: '[silent]' }] },
    },
    {
      type: 'assistant',
      stream_id: 's9',
      message: { content: [{ type: 'text', text: ' continued jv-t245' }] },
    },
    {
      type: 'assistant',
      stream_id: 's9',
      message: { content: [], stop_reason: 'end_turn' },
    },
    { type: 'agent_note', text: 'note' },
    {
      type: 'assistant',
      stream_id: 's10',
      message: {
        content: [{ type: 'text', text: 'Owner needs this.' }],
        stop_reason: 'end_turn',
      },
    },
  ]);
  assert.strictEqual(chunks.length, 1);
  assert.strictEqual(chunks[0].text, 'Owner needs this.');
});

test('T161 extract multi-block: text parts are segments', function () {
  const chunks = VL.coalesceTranscriptFrames([
    {
      type: 'assistant',
      message: {
        content: [
          { type: 'text', text: 'First paragraph ends here.' },
          { type: 'text', text: 'Second paragraph starts here.' },
        ],
      },
    },
  ]);
  assert.strictEqual(chunks.length, 1);
  assert.strictEqual(
    chunks[0].text,
    'First paragraph ends here.\n\nSecond paragraph starts here.',
  );
});


// ── full data resident + windowed content ────────────────────────────

test('T119 full data: progressive pages cover entire older range without scroll gate', function () {
  const pages = VL.progressiveHistoryPages(500, 200);
  assert.ok(pages.length >= 3);
  assert.strictEqual(pages[0].end, 500);
  assert.strictEqual(pages[pages.length - 1].start, 0);
  let covered = 0;
  pages.forEach(function (p) {
    covered += p.limit;
    assert.ok(p.limit > 0 && p.limit <= 200);
  });
  assert.strictEqual(covered, 500);
  // Policy: pages are a pure plan — no dependency on scrollTop.
  assert.ok(typeof pages[0].scrollTop === 'undefined');
});

test('T119 windowed content: material count << N while data count = N', function () {
  const n = 1000;
  const mat = VL.materialisedCount(n, 40, 500, 800);
  assert.ok(mat < 100, 'content windowed, mat=' + mat);
  // Data residency is independent: progressive plan holds all N in pages.
  const pages = VL.progressiveHistoryPages(n, 200);
  const dataLines = pages.reduce(function (s, p) { return s + p.limit; }, 0);
  assert.strictEqual(dataLines, n);
});

// ── shell+content state machine + size cache ─────────────────────────

test('T119 shell states: unmeasured | dematerialized | material', function () {
  assert.strictEqual(VL.shellState({ material: true }), VL.SHELL_MATERIAL);
  assert.strictEqual(VL.shellState({ material: false, hasValidSize: true }), VL.SHELL_DEMATERIALIZED);
  assert.strictEqual(VL.shellState({ material: false, hasValidSize: false }), VL.SHELL_UNMEASURED);
  assert.strictEqual(VL.nextStateOnEnterBand(VL.SHELL_UNMEASURED), VL.SHELL_MATERIAL);
  assert.strictEqual(VL.nextStateOnEnterBand(VL.SHELL_DEMATERIALIZED), VL.SHELL_MATERIAL);
  assert.strictEqual(
    VL.nextStateOnLeaveBand(VL.SHELL_MATERIAL, true),
    VL.SHELL_DEMATERIALIZED,
  );
  assert.strictEqual(
    VL.nextStateOnLeaveBand(VL.SHELL_MATERIAL, false),
    VL.SHELL_UNMEASURED,
  );
});

test('T119 size cache: dematerialize freezes height; rematerialize stable', function () {
  const cache = VL.createSizeCache();
  const id = 'chunk-1';
  VL.recordSize(cache, id, 120, 640);
  const e = VL.getSize(cache, id);
  assert.strictEqual(e.height, 120);
  assert.strictEqual(e.width, 640);
  assert.ok(VL.sizeValidAtWidth(e, 640));
  assert.ok(!VL.sizeValidAtWidth(e, 400));
  // Frozen shell keeps height without content.
  assert.strictEqual(VL.frozenShellHeight(e, 640, 72), 120);
  // Wrong width falls back to estimate (no collapse to 0).
  assert.strictEqual(VL.frozenShellHeight(e, 400, 72), 72);
  assert.ok(VL.frozenShellHeight(null, 640, 72) > 0);
});

test('T119 size cache: dimension stability after dematerialize round-trip', function () {
  const cache = VL.createSizeCache();
  VL.recordSize(cache, 'a', 200, 800);
  // leave band → dematerialized with frozen height
  let state = VL.SHELL_MATERIAL;
  state = VL.nextStateOnLeaveBand(state, true);
  assert.strictEqual(state, VL.SHELL_DEMATERIALIZED);
  const h1 = VL.frozenShellHeight(VL.getSize(cache, 'a'), 800, 72);
  // re-enter → material (content from full chunk data; height from cache then remeasure)
  state = VL.nextStateOnEnterBand(state);
  assert.strictEqual(state, VL.SHELL_MATERIAL);
  VL.recordSize(cache, 'a', 200, 800); // remeasure same
  const h2 = VL.getSize(cache, 'a').height;
  assert.strictEqual(h1, h2);
});

// ── resize remeasure ─────────────────────────────────────────────────

test('T119 resize invalidates size cache; remeasure near first', function () {
  const cache = VL.createSizeCache();
  VL.recordSize(cache, 'a', 100, 600);
  VL.recordSize(cache, 'b', 150, 600);
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 600), false);
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 480), true);
  const cleared = VL.invalidateOnWidthChange(cache, 600, 480);
  assert.strictEqual(cleared, 2);
  assert.strictEqual(VL.getSize(cache, 'a'), null);

  const tops = [];
  for (let i = 0; i < 50; i++) tops.push({ top: i * 40, height: 40 });
  // Mid-list scroll
  const order = VL.remeasureOrder(tops, 800, 400, 200);
  assert.ok(order.immediate.length > 0);
  assert.ok(order.deferred.length > 0);
  assert.ok(order.immediate[0] > 5, 'near is mid-list, not only start');
  // Immediate indices are subset of band; deferred are the rest.
  const imm = new Set(order.immediate);
  order.deferred.forEach(function (i) {
    assert.ok(!imm.has(i));
  });
  assert.strictEqual(order.immediate.length + order.deferred.length, 50);
});

// ── enter/leave band events ──────────────────────────────────────────

test('T119 events: enter/leave band without polling', function () {
  const tops = [];
  for (let i = 0; i < 40; i++) tops.push({ top: i * 50, height: 50 });
  const a = VL.enterLeaveBand(tops, 0, 200, 50, []);
  assert.ok(a.enter.length > 0);
  assert.strictEqual(a.leave.length, 0);
  const b = VL.enterLeaveBand(tops, 1000, 200, 50, a.material);
  assert.ok(b.enter.length > 0);
  assert.ok(b.leave.length > 0);
  const drivers = VL.residencyDrivers();
  assert.ok(drivers.indexOf('scroll') >= 0);
  assert.ok(drivers.indexOf('resize') >= 0);
  assert.ok(drivers.indexOf('chunk_append') >= 0);
  assert.ok(drivers.indexOf('jump_to_bottom') >= 0);
  assert.ok(drivers.indexOf('intersection') >= 0);
});

// ── jump-to-bottom; no jump-to-top ───────────────────────────────────

test('T119 jump-to-bottom hotkey + FAB; no jump-to-top', function () {
  const p = VL.jumpPolicy();
  assert.strictEqual(p.hasJumpToBottom, true);
  assert.strictEqual(p.hasJumpToTop, false);
  assert.ok(p.hotkeys.indexOf('End') >= 0);

  assert.strictEqual(VL.isJumpToBottomHotkey('End', {}), true);
  assert.strictEqual(VL.isJumpToBottomHotkey('ArrowDown', { metaKey: true }), true);
  assert.strictEqual(VL.isJumpToBottomHotkey('ArrowDown', { ctrlKey: true }), true);
  assert.strictEqual(VL.isJumpToBottomHotkey('Home', {}), false);
  assert.strictEqual(VL.isJumpToBottomHotkey('ArrowUp', { metaKey: true }), false);

  assert.strictEqual(VL.shouldShowJumpFab('free', false), true);
  assert.strictEqual(VL.shouldShowJumpFab('free', true), false);
  assert.strictEqual(VL.shouldShowJumpFab('track', true), false);
});

// ── estimate for unmeasured shells ───────────────────────────────────

test('T119 estimate height for unmeasured shells (lazy)', function () {
  const short = VL.estimateHeightFromText('hi');
  const long = VL.estimateHeightFromText('line\n'.repeat(40));
  assert.ok(short >= VL.DEFAULT_ESTIMATE_HEIGHT);
  assert.ok(long > short);
  assert.ok(long <= 480);
});

// ── 🎯T336 progressive rematerialize / Page Up thrash oracle ─────────

test('T336 rematerializePriority: strict viewport first', function () {
  // Inside viewport → 0
  assert.strictEqual(VL.rematerializePriority(100, 50, 0, 300), 0);
  // Fully above → positive distance
  assert.ok(VL.rematerializePriority(0, 50, 200, 300) > 0);
  // Fully below → positive distance
  assert.ok(VL.rematerializePriority(500, 50, 0, 300) > 0);
  // Closer above beats farther above
  const nearAbove = VL.rematerializePriority(100, 50, 200, 300); // bot=150, st=200 → 51
  const farAbove = VL.rematerializePriority(0, 50, 200, 300);     // bot=50 → 151
  assert.ok(nearAbove < farAbove);
});

test('T336 planRematerializeFrame: caps per frame; viewport-first order', function () {
  const pending = [
    { index: 0, top: 0, height: 40 },       // far above
    { index: 1, top: 200, height: 40 },     // in viewport (scroll 180, ch 100)
    { index: 2, top: 240, height: 40 },     // in viewport
    { index: 3, top: 400, height: 40 },     // below
    { index: 4, top: 160, height: 40 },     // near / partial
    { index: 5, top: 280, height: 40 },     // below-ish
    { index: 6, top: 320, height: 40 },
    { index: 7, top: 360, height: 40 },
  ];
  const plan = VL.planRematerializeFrame(pending, 180, 100, 3);
  assert.strictEqual(plan.maxPerFrame, 3);
  assert.strictEqual(plan.thisFrame.length, 3);
  assert.strictEqual(plan.remaining.length, pending.length - 3);
  assert.strictEqual(plan.syncWouldThrash, true);
  // First batch must prefer in-viewport (priority 0)
  plan.thisFrame.forEach(function (item) {
    const p = VL.rematerializePriority(item.top, item.height, 180, 100);
    assert.strictEqual(p, 0, 'thisFrame item must be strict-viewport, got p=' + p + ' idx=' + item.index);
  });
  // Unbounded sync is rejected by oracle
  const syncAll = VL.planRematerializeFrame(pending, 180, 100, pending.length);
  assert.strictEqual(syncAll.syncWouldThrash, false);
  assert.strictEqual(syncAll.thisFrame.length, pending.length);
});

test('T336 pageUpRematerializeBudget: thrash oracle fails unbounded sync path', function () {
  // Long transcript, short bubbles, large buffer → many shells enter on Page Up.
  const budget = VL.pageUpRematerializeBudget({
    n: 200,
    avgHeight: 48,
    clientHeight: 600,
    buffer: 800,
    pageFactor: 0.8,
    maxPerFrame: VL.REMATERIALIZE_PER_FRAME,
  });
  assert.ok(budget.enterCount > VL.REMATERIALIZE_PER_FRAME,
    'Page Up must enter more shells than one frame cap (got enter=' + budget.enterCount + ')');
  assert.strictEqual(budget.syncWouldThrash, true,
    'unbounded sync rematerialize of Page-Up enter set must thrash');
  assert.ok(budget.thisFrameCount <= VL.REMATERIALIZE_PER_FRAME);
  assert.strictEqual(budget.thisFrameCount, VL.REMATERIALIZE_PER_FRAME);
  assert.strictEqual(budget.remainingCount, budget.enterCount - budget.thisFrameCount);
  // Progressive multi-frame fill covers all enters
  let covered = budget.thisFrameCount;
  let remaining = budget.enterCount - budget.thisFrameCount;
  let frames = 1;
  while (remaining > 0 && frames < 50) {
    const step = Math.min(VL.REMATERIALIZE_PER_FRAME, remaining);
    covered += step;
    remaining -= step;
    frames++;
  }
  assert.strictEqual(covered, budget.enterCount);
  assert.ok(frames > 1, 'progressive fill spans multiple frames');
});

test('T336 pageUpRematerializeBudget: small jump does not thrash', function () {
  // Few messages all already near material band at bottom.
  const budget = VL.pageUpRematerializeBudget({
    n: 8,
    avgHeight: 80,
    clientHeight: 600,
    buffer: 800,
    pageFactor: 0.8,
    maxPerFrame: VL.REMATERIALIZE_PER_FRAME,
  });
  // Entire list fits in band at bottom; Page Up may enter 0–few.
  assert.ok(budget.enterCount <= VL.REMATERIALIZE_PER_FRAME,
    'small list enter=' + budget.enterCount);
  assert.strictEqual(budget.syncWouldThrash, false);
});

test('T336 REMATERIALIZE_PER_FRAME is a positive bound', function () {
  assert.ok(VL.REMATERIALIZE_PER_FRAME > 0);
  assert.ok(VL.REMATERIALIZE_PER_FRAME <= 20, 'cap stays small enough to bound main-thread work');
});

// Source contract: index.html wires progressive rematerialize + PageUp no loadEarlier.
test('T336 index wires planRematerializeFrame; PageUp does not call loadEarlier', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('planRematerializeFrame') >= 0, 'virtualize uses planner');
  assert.ok(html.indexOf('scheduleRematerialize') >= 0, 'progressive remat schedule');
  assert.ok(html.indexOf('REMATERIALIZE_PER_FRAME') >= 0, 'per-frame cap wired');
  assert.ok(html.indexOf('flushRematerializeFrame') >= 0, 'rAF flush path');
  // PageUp handler: scrollBy only — strip comments then forbid loadEarlier calls.
  const pu = html.indexOf("e.key === 'PageUp'");
  assert.ok(pu >= 0, 'PageUp handler present');
  const branchEnd = html.indexOf("e.key === 'PageDown'", pu);
  const branch = html.slice(pu, branchEnd > pu ? branchEnd : pu + 400);
  const codeOnly = branch.replace(/\/\/[^\n]*/g, '');
  assert.ok(codeOnly.indexOf('scrollBy') >= 0, 'PageUp scrolls');
  assert.ok(!/\bloadEarlier\b/.test(codeOnly), 'PageUp code must not reference loadEarlier');
  assert.ok(codeOnly.indexOf('leaveTrackBottom') >= 0, 'PageUp leaves track');
  // Collapse-off-screen must not rematerialize virt-shells synchronously.
  const colStart = html.indexOf('function collapseAutoExpandedOffScreen');
  assert.ok(colStart >= 0);
  const colEnd = html.indexOf('function expandInViewNearEnd', colStart);
  const colBody = html.slice(colStart, colEnd > colStart ? colEnd : colStart + 2000);
  // Flag flip without rematerializeMsg on virt-shell path (T336).
  assert.ok(colBody.indexOf("classList.contains('virt-shell')") >= 0);
  assert.ok(colBody.indexOf('continue') >= 0, 'shell collapse skips sync paint');
});

// ── History replay pin suppress (reload/reconnect) ───────────────────

test('T119.1 suppress pin during replay; final pin at bottom', function () {
  assert.strictEqual(VL.shouldSuppressPinDuringReplay(true), true);
  assert.strictEqual(VL.shouldSuppressPinDuringReplay(false), false);
  assert.strictEqual(VL.finalPinScrollTop(1000, 300), 700);
  assert.strictEqual(VL.finalPinScrollTop(100, 300), 0);
  assert.ok(VL.REPLAY_IDLE_END_MS >= 50 && VL.REPLAY_IDLE_END_MS <= 500);
});

// ── 🎯T341: pin threshold + expand-edge hysteresis + width eps ────────

test('T341 shouldPinScroll: sub-threshold height/scrollTop is no-op; force always', function () {
  assert.strictEqual(VL.shouldPinScroll({ force: true }), true);
  // Already at bottom, same height → no pin
  assert.strictEqual(VL.shouldPinScroll({
    prevScrollHeight: 1000,
    nextScrollHeight: 1000,
    clientHeight: 300,
    scrollTop: 700,
  }), false);
  // 1px height micro-change while already pinned → no pin
  assert.strictEqual(VL.shouldPinScroll({
    prevScrollHeight: 1000,
    nextScrollHeight: 1001,
    clientHeight: 300,
    scrollTop: 700,
  }), false);
  // ≥2px height growth → pin
  assert.strictEqual(VL.shouldPinScroll({
    prevScrollHeight: 1000,
    nextScrollHeight: 1002,
    clientHeight: 300,
    scrollTop: 700,
  }), true);
  // Drifted off bottom by ≥ threshold → pin even if height flat
  assert.strictEqual(VL.shouldPinScroll({
    prevScrollHeight: 1000,
    nextScrollHeight: 1000,
    clientHeight: 300,
    scrollTop: 698,
  }), true);
  // Accumulated growth from last pin height
  assert.strictEqual(VL.shouldPinScroll({
    prevScrollHeight: 1000,
    nextScrollHeight: 1005,
    clientHeight: 300,
    scrollTop: 700,
  }), true);
});

test('T341 visibleOverlapPx + auto-expand min-visible hysteresis', function () {
  // Bubble fully in view
  assert.strictEqual(VL.visibleOverlapPx(100, 50, 0, 300), 50);
  // 1px edge contact
  assert.strictEqual(VL.visibleOverlapPx(299, 50, 0, 300), 1);
  // Fully above
  assert.strictEqual(VL.visibleOverlapPx(0, 50, 100, 300), 0);
  // 1px contact must NOT auto-expand (enter threshold 8)
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: true, userToggled: false, historyReplayActive: false,
    top: 299, height: 50, scrollTop: 0, clientHeight: 300,
  }), false);
  // ≥8px overlap does expand
  assert.strictEqual(VL.shouldAutoExpandInView({
    tall: true, nearEnd: true, userToggled: false, historyReplayActive: false,
    top: 292, height: 50, scrollTop: 0, clientHeight: 300,
  }), true);
  // Collapse still only when fully outside (1px remaining keeps open)
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, userToggled: false, autoExpanded: true,
    top: 299, height: 50, scrollTop: 0, clientHeight: 300,
  }), false);
  assert.strictEqual(VL.shouldAutoCollapseOffScreen({
    isLatest: false, userToggled: false, autoExpanded: true,
    top: 0, height: 50, scrollTop: 100, clientHeight: 300,
  }), true);
  assert.ok(VL.MIN_VISIBLE_PX_FOR_AUTO_EXPAND >= 4);
  assert.ok(VL.PIN_HEIGHT_THRESHOLD_PX >= 1);
});

test('T341 shouldInvalidateSizeCache ignores sub-pixel width jitter', function () {
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 600), false);
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 600.4), false);
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 599.6), false);
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 601), true);
  assert.strictEqual(VL.shouldInvalidateSizeCache(600, 480), true);
  assert.ok(VL.WIDTH_INVALIDATE_EPS_PX >= 1);
});

test('index.html wires T341 pin gate + scrollbar-gutter + thrash counters', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('shouldPinScroll') >= 0, 'shouldPinScroll wired');
  assert.ok(html.indexOf('PIN_HEIGHT_THRESHOLD_PX') >= 0, 'pin threshold wired');
  assert.ok(html.indexOf('scrollbar-gutter: stable') >= 0 || html.indexOf('scrollbar-gutter:stable') >= 0,
    'scrollbar-gutter stable on #messages');
  assert.ok(html.indexOf('__layoutThrash') >= 0, 'idle thrash counters exposed');
  assert.ok(html.indexOf('scrollDownPinned') >= 0, 'pinned-write counter');
  // work-dots must stay opacity-only (no transform layout shift)
  const bounce = html.indexOf('@keyframes work-bounce');
  assert.ok(bounce >= 0);
  const frame = html.slice(bounce, bounce + 120);
  assert.ok(frame.indexOf('opacity') >= 0, 'work-bounce uses opacity');
  assert.ok(frame.indexOf('transform') < 0, 'work-bounce must not transform');
});

test('index.html wires T261 expandInViewNearEnd + suppress collapse mid-replay', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('expandInViewNearEnd') >= 0, 'expandInViewNearEnd present');
  assert.ok(html.indexOf('shouldAutoExpandInView') >= 0, 'shouldAutoExpandInView wired');
  assert.ok(html.indexOf('shouldRunOffScreenCollapse') >= 0, 'shouldRunOffScreenCollapse wired');
  assert.ok(html.indexOf('isNearTranscriptEnd') >= 0, 'isNearTranscriptEnd wired');
  assert.ok(/function endHistoryReplayAndPin[\s\S]{0,1500}refreshLatestExpansion\(\)/.test(html),
    'history pin calls refreshLatestExpansion (T261)');
});

test('index.html wires historyReplayActive suppress on scrollDown', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('beginHistoryReplay') >= 0);
  assert.ok(html.indexOf('endHistoryReplayAndPin') >= 0);
  assert.ok(html.indexOf('historyReplayActive') >= 0);
  assert.ok(/function scrollDown\([^)]*\)[\s\S]{0,400}historyReplayActive/.test(html),
    'scrollDown checks historyReplayActive');
  assert.ok(html.indexOf("endHistoryReplayAndPin('history_meta')") >= 0 ||
    html.indexOf('endHistoryReplayAndPin("history_meta")') >= 0 ||
    html.indexOf("endHistoryReplayAndPin('history_meta')") >= 0);
});

// ── 🎯T347: hard-reload end-first pin, lazy replay, parade-proof ─────

test('T347 shouldReenterReplay re-suppresses pre-meta frames only', function () {
  // Early idle-end mid-burst → the next replay frame re-enters suppression.
  assert.strictEqual(VL.shouldReenterReplay({
    awaitingHistoryMeta: true, historyReplayActive: false, msSinceConnect: 900,
  }), true);
  // Already suppressed → nothing to do.
  assert.strictEqual(VL.shouldReenterReplay({
    awaitingHistoryMeta: true, historyReplayActive: true, msSinceConnect: 900,
  }), false);
  // Post-meta (live) frames never re-enter.
  assert.strictEqual(VL.shouldReenterReplay({
    awaitingHistoryMeta: false, historyReplayActive: false, msSinceConnect: 900,
  }), false);
  // Meta never arrived: degrade to live after the cap (streaming must pin).
  assert.strictEqual(VL.shouldReenterReplay({
    awaitingHistoryMeta: true, historyReplayActive: false,
    msSinceConnect: VL.REPLAY_REENTER_MAX_MS + 1,
  }), false);
  assert.ok(VL.REPLAY_REENTER_MAX_MS >= 10000);
});

test('T347 replay appends stay lazy; virtualize deferred until pin', function () {
  assert.strictEqual(VL.shouldPaintOnReplayAppend(true), false);
  assert.strictEqual(VL.shouldPaintOnReplayAppend(false), true);
  assert.strictEqual(VL.shouldVirtualizeDuringReplay(true), false);
  assert.strictEqual(VL.shouldVirtualizeDuringReplay(false), true);
});

test('T347 reload trace: end-first pin, zero climb, band-bounded materialize', function () {
  const n = 2000;
  const t = VL.replayHydrateTrace({ n: n, avgHeight: 72, clientHeight: 600 });
  assert.strictEqual(t.midHydrateClimbs, 0, 'no scrollTop climb during hydrate');
  assert.strictEqual(t.paintedDuringReplay, 0, 'no markdown paint during replay');
  assert.strictEqual(t.finalDistToBottom, 0, 'pin lands at the live end');
  assert.ok(t.landsOnLatest, 'latest turn is in the material set');
  assert.ok(t.materializedAtPin <= t.materialBandBound,
    'materialize bounded by viewport+buffer band (' + t.materializedAtPin +
    ' > ' + t.materialBandBound + ')');
  assert.ok(t.materializedAtPin < n / 10, 'far-above turns stay shells');
  // scrollTop is flat through the burst; the only move is the one end pin.
  for (let i = 0; i < n; i++) assert.strictEqual(t.scrollTops[i], 0);
  assert.ok(t.scrollTops[n] > 0, 'single pin jump at history_meta');
});

test('T347 oracle catches both regressions it exists for', function () {
  // Per-message pin during the burst (early idle-end) → the scroll parade.
  const parade = VL.replayHydrateTrace({
    n: 500, avgHeight: 72, clientHeight: 600, suppressPinDuringReplay: false,
  });
  assert.ok(parade.midHydrateClimbs > 100,
    'parade sim must show per-message scrollTop climb, got ' + parade.midHydrateClimbs);
  // Eager full paint old→new during replay → the ~60s settle class.
  const eager = VL.replayHydrateTrace({
    n: 500, avgHeight: 72, clientHeight: 600, lazyAppend: false,
  });
  assert.strictEqual(eager.paintedDuringReplay, 500,
    'eager sim must count full-list materialize');
});

test('index.html wires T347 lazy replay + pre-meta re-entry + gated virtualize', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('_awaitingHistoryMeta') >= 0, 'awaiting-meta flag present');
  assert.ok(html.indexOf('shouldReenterReplay') >= 0, 'pre-meta re-entry wired');
  assert.ok(html.indexOf('shouldPaintOnReplayAppend') >= 0, 'lazy replay append wired');
  assert.ok(/function virtualizeMessages\(\)[\s\S]{0,600}shouldVirtualizeDuringReplay/.test(html),
    'virtualize gated during replay');
  assert.ok(/function flushRematerializeFrame\(\)[\s\S]{0,500}historyReplayActive/.test(html),
    'remat queue held during replay');
  assert.ok(/history_meta'[\s\S]{0,600}_awaitingHistoryMeta = false/.test(html),
    'history_meta releases the pre-meta guard');
  // Seal + stream-render replay branches keep shells unpainted (estimate only).
  assert.ok(/function scheduleJevonsRender[\s\S]{0,900}virt-shell/.test(html),
    'stream render has a shell branch');
  assert.ok(/function sealAssistantStream[\s\S]{0,2200}virt-shell/.test(html),
    'seal has a shell branch');
});

console.log(process.exitCode ? 'FAIL' : 'PASS virtual_list_test');
