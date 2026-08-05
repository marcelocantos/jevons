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

// ── History replay pin suppress (reload/reconnect) ───────────────────

test('T119.1 suppress pin during replay; final pin at bottom', function () {
  assert.strictEqual(VL.shouldSuppressPinDuringReplay(true), true);
  assert.strictEqual(VL.shouldSuppressPinDuringReplay(false), false);
  assert.strictEqual(VL.finalPinScrollTop(1000, 300), 700);
  assert.strictEqual(VL.finalPinScrollTop(100, 300), 0);
  assert.ok(VL.REPLAY_IDLE_END_MS >= 50 && VL.REPLAY_IDLE_END_MS <= 500);
});

test('index.html wires historyReplayActive suppress on scrollDown', function () {
  const fs = require('fs');
  const path = require('path');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('beginHistoryReplay') >= 0);
  assert.ok(html.indexOf('endHistoryReplayAndPin') >= 0);
  assert.ok(html.indexOf('historyReplayActive') >= 0);
  assert.ok(/function scrollDown\(\)[\s\S]{0,200}historyReplayActive/.test(html),
    'scrollDown checks historyReplayActive');
  assert.ok(html.indexOf("endHistoryReplayAndPin('history_meta')") >= 0 ||
    html.indexOf('endHistoryReplayAndPin("history_meta")') >= 0 ||
    html.indexOf("endHistoryReplayAndPin('history_meta')") >= 0);
});

console.log(process.exitCode ? 'FAIL' : 'PASS virtual_list_test');
