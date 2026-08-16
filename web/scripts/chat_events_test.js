// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic headless tests for chat working-indicator + stream coalesce.
// Run: node web/scripts/chat_events_test.js
//
// Oracle for the token-per-bubble regression: a Grok turn that streams
// ["Hello", ".", "What", "do", "you", "need", "?"] must produce exactly
// ONE assistant bubble containing the full sentence.

'use strict';

const assert = require('assert');
const path = require('path');
const fs = require('fs');
const { spawnSync } = require('child_process');
const ChatEvents = require('./chat_events.js');

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

function chunk(text) {
  return {
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text }] },
  };
}

function endTurn() {
  return {
    type: 'assistant',
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  };
}

function user(text) {
  return { type: 'user', message: { role: 'user', content: text } };
}

// ── shouldClearWorking ──────────────────────────────────────────

test('system clears working', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({ type: 'system' }), true);
});

test('mid-stream text chunk does NOT clear working', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking(chunk('Hello')), false);
  assert.strictEqual(ChatEvents.shouldClearWorking(chunk('.')), false);
});

test('assistant text with end_turn clears', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: {
      content: [{ type: 'text', text: 'hi' }],
      stop_reason: 'end_turn',
    },
  }), true);
});

test('empty-content end_turn is a seal signal (Grok ACP terminal)', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking(endTurn()), true);
});

// ── 🎯T260 working chrome vs seal ────────────────────────────────
// toolUse() is defined below with the T159 helpers (name, optional stopReason).

test('T260: tools-only end_turn seals but keeps working chrome', () => {
  const term = endTurn();
  assert.strictEqual(ChatEvents.shouldClearWorking(term), true, 'still a seal signal');
  assert.strictEqual(
    ChatEvents.shouldClearWorkingChrome({ hadVisible: false, hadTool: true, silent: false }, term),
    false,
    'tools-only must not clear owner chrome',
  );
});

test('T260: visible text then end_turn clears chrome', () => {
  const term = endTurn();
  assert.strictEqual(
    ChatEvents.shouldClearWorkingChrome({ hadVisible: true, hadTool: true, silent: false }, term),
    true,
  );
});

test('T260: silent-only terminal may clear without bubble (residual)', () => {
  const term = endTurn();
  assert.strictEqual(
    ChatEvents.shouldClearWorkingChrome({ hadVisible: false, hadTool: false, silent: true }, term),
    true,
  );
});

test('T260: vacuous empty end_turn clears chrome', () => {
  const term = endTurn();
  assert.strictEqual(
    ChatEvents.shouldClearWorkingChrome({ hadVisible: false, hadTool: false, silent: false }, term),
    true,
  );
});

test('T260 lifecycle: tools → empty end_turn → note stream → text → end_turn', () => {
  // Owner repro: CPU dig had tools-only end_turn, then agent_note re-prompt,
  // then visible "Investigating…" while UI had already gone idle.
  const events = [
    user('Why is jevonsd consuming so much CPU?'),
    toolUse('run_terminal_command'),
    toolUse('run_terminal_command'),
    endTurn(), // tools-only ACP stop — must KEEP working
    // Second stream (note re-prompt): more tools then visible reply
    toolUse('run_terminal_command'),
    chunk('Investigating jevonsd CPU.'),
    endTurn(),
  ];
  assert.strictEqual(
    ChatEvents.workingLifecycle(events.slice(0, 4)),
    true,
    'working must stay true after tools-only end_turn',
  );
  assert.strictEqual(
    ChatEvents.workingLifecycle(events),
    false,
    'working clears after owner-visible text + end_turn',
  );
  const mid = ChatEvents.applyChatEvents(events.slice(0, 4));
  assert.strictEqual(mid.working, true);
  assert.strictEqual(mid.openStream, -1, 'tools-only end_turn still seals stream');
  const done = ChatEvents.applyChatEvents(events);
  assert.strictEqual(done.working, false);
  assert.ok(done.assistantBubbles.some((b) => /Investigating/.test(b)));
});

test('T260: mid-stream text still does not clear; text+end_turn does', () => {
  assert.strictEqual(ChatEvents.workingLifecycle([user('hi'), chunk('Hello')]), true);
  assert.strictEqual(
    ChatEvents.workingLifecycle([user('hi'), chunk('Hello'), endTurn()]),
    false,
  );
});

test('tool_use-only assistant does not clear', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: { content: [{ type: 'tool_use', name: 'Bash', input: {} }] },
  }), false);
});

test('assistant with non-array content does not clear', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    type: 'assistant',
    message: { content: 'not-an-array' },
  }), false);
});

test('ACP raw without type does not clear', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking({
    sessionId: 's',
    update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'x' } },
  }), false);
  assert.strictEqual(ChatEvents.shouldClearWorking({ stopReason: 'end_turn' }), false);
});

// ── working stays true across stream, false after end_turn ─────

test('working stays true across multi-chunk stream until end_turn', () => {
  const events = [
    chunk('Hello'),
    chunk('.'),
    chunk(' What'),
    chunk(' do'),
    chunk(' you'),
    chunk(' need'),
    chunk('?'),
  ];
  // After stream only (no end_turn) working must remain true.
  assert.strictEqual(ChatEvents.workingLifecycle(events), true);
  assert.strictEqual(ChatEvents.workingLifecycle([...events, endTurn()]), false);
});

test('stuck lifecycle: raw ACP only (pre-fix) leaves working=true', () => {
  const events = [
    { sessionId: 's', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'pong' } } },
    { stopReason: 'end_turn' },
  ];
  assert.strictEqual(ChatEvents.workingLifecycle(events), true);
});

// ── stream coalesce (the screenshot regression) ─────────────────

test('screenshot regression: token stream → one bubble, full text', () => {
  // Exact pattern from the user screenshot: one user "hello", then
  // assistant tokens each rendered as their own bubble before the fix.
  const tokens = ['Hello', '.', 'What', 'do', 'you', 'need', '?'];
  const events = [user('hello'), ...tokens.map(chunk), endTurn()];
  const state = ChatEvents.applyChatEvents(events);

  assert.strictEqual(state.working, false, 'working must clear after end_turn');
  assert.strictEqual(state.userTexts.length, 1, 'one user bubble');
  assert.strictEqual(state.userTexts[0], 'hello');
  assert.strictEqual(
    state.assistantBubbles.length,
    1,
    `expected 1 assistant bubble, got ${state.assistantBubbles.length}: ${JSON.stringify(state.assistantBubbles)}`,
  );
  assert.strictEqual(state.assistantBubbles[0], tokens.join(''));
  assert.strictEqual(state.openStream, -1, 'stream must be sealed');
});

test('two turns produce two sealed assistant bubbles', () => {
  const events = [
    user('hi'),
    chunk('Hello'),
    chunk(' there'),
    endTurn(),
    user('again'),
    chunk('Hi'),
    chunk(' again'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 2);
  assert.strictEqual(state.assistantBubbles[0], 'Hello there');
  assert.strictEqual(state.assistantBubbles[1], 'Hi again');
  assert.strictEqual(state.working, false);
});

test('user echo dedupe does not double-count', () => {
  const events = [user('hello'), user('hello'), chunk('Hi'), endTurn()];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.userTexts.length, 1);
});

test('tool_use mid-turn then text then end_turn: one bubble, cleared', () => {
  const events = [
    user('run it'),
    {
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'tool_use', input: {} }] },
    },
    chunk('done'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.assistantBubbles[0], 'done');
  assert.strictEqual(state.working, false);
});

// ── 🎯T159 one bubble per terminal stop_reason ───────────────────

function toolUse(name, stopReason) {
  const message = {
    role: 'assistant',
    content: [{ type: 'tool_use', name: name || 'Bash', input: {} }],
  };
  if (stopReason) message.stop_reason = stopReason;
  return { type: 'assistant', message };
}

test('T159: stop_reason tool_use never clears working', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking(toolUse('Bash', 'tool_use')), false);
  assert.strictEqual(ChatEvents.isTerminalStop(toolUse('Bash', 'tool_use')), false);
  assert.ok(ChatEvents.TERMINAL_STOPS.has('end_turn'));
  assert.ok(ChatEvents.TERMINAL_STOPS.has('stop_sequence'));
  assert.ok(ChatEvents.TERMINAL_STOPS.has('max_tokens'));
  assert.ok(!ChatEvents.TERMINAL_STOPS.has('tool_use'));
});

test('T159 hermetic: multi-chunk + tool_use + text + end_turn → one bubble', () => {
  // Acceptance fixture: stream text → tool round(s) → more text → terminal.
  const events = [
    user('clean asides'),
    chunk('| Bubble | Content |\n'),
    chunk('|--------|---------|\n'),
    chunk('| 1 | table |\n'),
    toolUse('use_tool', 'tool_use'),
    toolUse('run_terminal_command'),
    chunk('\n**Note:** daily daemon needs the binary.\n'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(
    state.assistantBubbles.length,
    1,
    `expected 1 bubble, got ${state.assistantBubbles.length}: ${JSON.stringify(state.assistantBubbles)}`,
  );
  const raw = state.assistantBubbles[0];
  assert.ok(raw.includes('| Bubble |'), 'table head present');
  assert.ok(raw.includes('**Note:**'), 'note present in same bubble');
  assert.strictEqual(state.openStream, -1, 'sealed after end_turn');
  assert.strictEqual(state.working, false);
});

test('T159: text before and after tool_use stop_reason stays one bubble', () => {
  const events = [
    user('x'),
    chunk('Table then '),
    toolUse('search_tool', 'tool_use'),
    chunk('Note after tools'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  // 🎯T161: tool_use is a segment edge → structural blank line; still one bubble.
  assert.strictEqual(state.assistantBubbles[0], 'Table then \n\nNote after tools');
});

test('T159: two real end_turns → two bubbles (no continuity heuristic)', () => {
  const events = [
    user('a'),
    chunk('first'),
    endTurn(),
    // No clever merge across real terminal boundaries — even without a user.
    chunk('second'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 2);
  assert.strictEqual(state.assistantBubbles[0], 'first');
  assert.strictEqual(state.assistantBubbles[1], 'second');
});

// ── 🎯T223 join by stream_id; user mid-stream does not split ──────

function chunkWithId(text, streamId) {
  return {
    type: 'assistant',
    stream_id: streamId,
    message: { role: 'assistant', content: [{ type: 'text', text }] },
  };
}

function endTurnWithId(streamId) {
  return {
    type: 'assistant',
    stream_id: streamId,
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  };
}

test('T223: user mid-assistant without seal keeps one legacy bubble', () => {
  // Journal-shaped interleave: asst half, owner user, asst half, end_turn.
  const events = [
    user('start'),
    chunk('First half ends mid'),
    chunk('-sentence.'),
    user('observe bullseye'),
    chunk(' Second half continues.'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(
    state.assistantBubbles.length,
    1,
    `expected 1 bubble, got ${state.assistantBubbles.length}: ${JSON.stringify(state.assistantBubbles)}`,
  );
  assert.strictEqual(
    state.assistantBubbles[0],
    'First half ends mid-sentence. Second half continues.',
  );
  assert.strictEqual(state.userTexts.length, 2);
  assert.strictEqual(state.openStream, -1);
});

test('T223: interleaved fragments join by stream_id; two ids stay two', () => {
  const events = [
    chunkWithId('A1', 's-a'),
    chunkWithId('B1', 's-b'),
    user('noise'),
    chunkWithId('A2', 's-a'),
    chunkWithId('B2', 's-b'),
    endTurnWithId('s-a'),
    endTurnWithId('s-b'),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 2);
  assert.strictEqual(state.assistantBubbles[0], 'A1A2');
  assert.strictEqual(state.assistantBubbles[1], 'B1B2');
  assert.strictEqual(ChatEvents.streamIdOf(chunkWithId('x', 's-a')), 's-a');
});

// ── 🎯T249 one stream_id = one growing bubble (never multi mid-stream) ──

test('T249 multi-paragraph stream stays one bubble at every intermediate step', () => {
  // Owner repro shape: T247 land reply (paragraph-ish splits) — all one stream_id.
  const sid = '0c38c30e-53f3-4783-8e08-dc633e707850';
  const tokens = [
    '**', '🎯', 'T', '247', ' landed', '**', ' —', ' independent', ' check', ' agrees',
    ' (`', '0', 'fb', 'ce', '59', '`,', ' herm', 'etics', ' green', ').',
    '\n\n',
    '**', 'Hard', '-', 'reload', '**', ' so', ' `', 'target', ':`', ' open',
    '\n\n',
    'Still', ' in', ' progress', ':', ' **', 'T', '246', '**', ' and', ' **', 'T', '248', '**.',
  ];
  const state = ChatEvents.createTurnState();
  let maxBubbles = 0;
  ChatEvents.applyChatEvent(state, {
    type: 'assistant',
    stream_id: sid,
    message: {
      role: 'assistant',
      content: [{ type: 'tool_use', name: 'run_terminal_command', input: {} }],
    },
  });
  for (const t of tokens) {
    ChatEvents.applyChatEvent(state, chunkWithId(t, sid));
    if (state.assistantBubbles.length > maxBubbles) {
      maxBubbles = state.assistantBubbles.length;
    }
    assert.strictEqual(
      state.assistantBubbles.length,
      1,
      `mid-stream bubble count ${state.assistantBubbles.length} after token ${JSON.stringify(t)}`,
    );
  }
  ChatEvents.applyChatEvent(state, endTurnWithId(sid));
  assert.strictEqual(maxBubbles, 1, 'max mid-stream bubbles must stay 1');
  assert.strictEqual(state.assistantBubbles.length, 1);
  const body = state.assistantBubbles[0];
  assert.ok(body.includes('independent check agrees'), body);
  assert.ok(body.includes('Hard-reload') || body.includes('Hard'), body);
  assert.ok(body.includes('Still in progress'), body);
  assert.ok(body.includes('\n\n'), 'paragraph breaks preserved inside one bubble');
  assert.strictEqual(state.openById[sid], undefined, 'sealed stream not open');
});

test('T249 distinct stream_ids stay separate bubbles', () => {
  const events = [
    chunkWithId('first turn body', 'sid-1'),
    endTurnWithId('sid-1'),
    chunkWithId('second turn body', 'sid-2'),
    endTurnWithId('sid-2'),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 2);
  assert.strictEqual(state.assistantBubbles[0], 'first turn body');
  assert.strictEqual(state.assistantBubbles[1], 'second turn body');
});

test('T249 conversation_widget.js: resolveOpen re-homes same stream_id (no multi-bubble)', () => {
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(/function resolveOpen\(/.test(src), 'widget must resolve open stream before minting bubble');
  assert.ok(html.includes('🎯T249') || html.includes('T249') || src.includes('T249'),
    'T249 marker on the live join path');
  // isConnected alone must not mint a second bubble for the same stream_id.
  assert.ok(
    !/openStreamById\[streamId\]\.isConnected\s*&&[\s\S]{0,80}typeof openStreamById\[streamId\]\._streamRaw/.test(src),
    'must not gate join solely on isConnected (T249 re-home path)',
  );
});

// ── 🎯T245 silent-only turns do not leak into next visible bubble ──

test('T245 isSilentAssistantText matches [silent] prefix', () => {
  assert.ok(ChatEvents.isSilentAssistantText('[silent] PO re-pressured'));
  assert.ok(ChatEvents.isSilentAssistantText('  [SILENT] ops ok'));
  assert.ok(!ChatEvents.isSilentAssistantText('Owner needs this'));
  assert.ok(!ChatEvents.isSilentAssistantText(''));
});

test('T245 pure silent stream + agent_note + visible → only second bubble', () => {
  const events = [
    chunkWithId('[silent] PO already re-pressured jv-t244; no further action.', 's-silent'),
    endTurnWithId('s-silent'),
    { type: 'agent_note', text: 'worker note' },
    chunkWithId('**🎯T244 landed.**', 's-vis'),
    endTurnWithId('s-vis'),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(
    state.assistantBubbles.length,
    1,
    `want 1 bubble, got ${JSON.stringify(state.assistantBubbles)}`,
  );
  assert.strictEqual(state.assistantBubbles[0], '**🎯T244 landed.**');
  assert.ok(state.assistantBubbles[0].indexOf('[silent]') < 0);
});

test('T245 multi-fragment silent then visible: only visible body', () => {
  const events = [
    chunkWithId('[silent]', 's9'),
    chunkWithId(' continued jv-t245', 's9'),
    endTurnWithId('s9'),
    chunkWithId('Owner needs this.', 's10'),
    endTurnWithId('s10'),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.assistantBubbles[0], 'Owner needs this.');
});

test('T223 index.html: no seal on user; join keys stream_id', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  assert.ok(/var byId = Object\.create\(null\)/.test(src) || /byId\[sid\]/.test(src),
    'widget must keep stream_id → el map');
  assert.ok(html.includes('stream_id') || html.includes('streamId'), 'must read wire stream id');
  // User path must not call sealAssistantStream (T223 pin).
  const userBlock = html.match(/if \(typ === 'user'\) \{[\s\S]*?\n    return;\n  \}/);
  assert.ok(userBlock, 'user handle block must exist');
  assert.ok(
    !/sealAssistantStream\s*\(/.test(userBlock[0]),
    'user mid-stream must not sealAssistantStream',
  );
  assert.ok(userBlock[0].indexOf('applyWireEvent') >= 0,
    'user ingest is the shared apply, not a second painter');
});

test('text+end_turn in one event: one bubble and clear', () => {
  const events = [
    user('x'),
    {
      type: 'assistant',
      message: {
        content: [{ type: 'text', text: 'complete' }],
        stop_reason: 'end_turn',
      },
    },
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.assistantBubbles[0], 'complete');
  assert.strictEqual(state.working, false);
});

// ── wire shape from Go normaliser (static samples) ──────────────

test('normalised wire samples: mid-stream keeps working, end clears', () => {
  const mid = JSON.parse(JSON.stringify({
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text: 'Hello' }] },
  }));
  const term = JSON.parse(JSON.stringify({
    type: 'assistant',
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  }));
  assert.strictEqual(ChatEvents.shouldClearWorking(mid), false);
  assert.strictEqual(ChatEvents.shouldClearWorking(term), true);
  assert.strictEqual(ChatEvents.shouldClearWorking({ type: 'system' }), true);
  // T260: empty end_turn alone is seal-true; chrome needs hadVisible/silent/vacuous.
  assert.strictEqual(
    ChatEvents.shouldClearWorkingChrome({ hadVisible: true }, term),
    true,
  );
  assert.strictEqual(
    ChatEvents.shouldClearWorkingChrome({ hadTool: true, hadVisible: false }, term),
    false,
  );
});

test('T260 index.html: shouldClearWorkingChrome + agent_note re-arm', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('shouldClearWorkingChrome'), 'must gate chrome on T260 policy');
  assert.ok(html.includes('ownerWorkingChrome'), 'must track owner-turn chrome flags');
  assert.ok(
    /agent_note[\s\S]*?ownerWorkingChrome/.test(html) ||
      /typ==='agent_note'[\s\S]{0,400}setWorking\(true\)/.test(html),
    'agent_note must re-arm working while owner turn open',
  );
});

// ── 🎯T272 level-triggered working after reconnect ───────────────

test('T272 workingLevelFromSample: open-turn fixture → working without send edge', () => {
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ working: true }),
    true,
    'history_meta.working true must re-arm',
  );
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ busy: true }),
    true,
  );
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ state: 'thinking' }),
    true,
  );
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ promptInFlight: true }),
    true,
  );
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ openStream: true }),
    true,
  );
});

test('T272 workingLevelFromSample: sealed history-only hydrate → idle', () => {
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ working: false }),
    false,
    'clean hydrate must not leave stuck working',
  );
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ type: 'history_meta', older: 0, total: 10 }),
    false,
  );
  assert.strictEqual(
    ChatEvents.workingLevelFromSample({ state: 'idle' }),
    false,
  );
  assert.strictEqual(ChatEvents.workingLevelFromSample(null), false);
  assert.strictEqual(ChatEvents.workingLevelFromSample({}), false);
});

test('T272 index.html: history_meta level sample (not edge-only kickoff)', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('applyWorkingLevelSample'), 'must apply level sample after hydrate');
  assert.ok(html.includes('workingLevelFromSample'), 'must use pure level helper');
  assert.ok(
    /history_meta[\s\S]{0,1200}applyWorkingLevelSample/.test(html),
    'history_meta path must call applyWorkingLevelSample',
  );
  // Product model is level re-sample, not "remember send edge across reload".
  assert.ok(
    /T272/.test(html) && /level/.test(html),
    'must document T272 level-trigger in product path',
  );
});

// ── 🎯T327 main working chrome clears / does not bind on aside route ─

function isAsideWireFixture(t) {
  const s = String(t || '');
  return /^\s*\[attention\s*:/i.test(s) || /^\s*\[target-aside\s*:/i.test(s) ||
    /\[attention\s*:/i.test(s.split(/\r?\n/).find(function (l) { return l.trim(); }) || '') ||
    /\[target-aside\s*:/i.test(s.split(/\r?\n/).find(function (l) { return l.trim(); }) || '');
}

test('T327 shouldBindMainWorkingChrome: main wire binds', () => {
  assert.strictEqual(
    ChatEvents.shouldBindMainWorkingChrome('file a target please', isAsideWireFixture),
    true,
  );
  assert.strictEqual(
    ChatEvents.shouldBindMainWorkingChrome('hello', null),
    true,
  );
});

test('T327 shouldBindMainWorkingChrome: attention / target-aside do not bind', () => {
  assert.strictEqual(
    ChatEvents.shouldBindMainWorkingChrome(
      '[attention:att-1|Title]\nbody',
      isAsideWireFixture,
    ),
    false,
  );
  assert.strictEqual(
    ChatEvents.shouldBindMainWorkingChrome(
      '[target-aside: att-2 | File this]\nfile X\n\n(Ceremony: …)',
      isAsideWireFixture,
    ),
    false,
  );
});

test('T327 shouldBindMainWorkingChrome: routed flag alone suppresses main', () => {
  assert.strictEqual(
    ChatEvents.shouldBindMainWorkingChrome('plain body', null, { routed: true }),
    false,
  );
});

test('T327 planMainWorkingAfterSend: aside → open false + suppress true', () => {
  const att = ChatEvents.planMainWorkingAfterSend(
    '[attention:att-x|t]\nhello',
    isAsideWireFixture,
  );
  assert.strictEqual(att.openMainWorking, false);
  assert.strictEqual(att.suppressMainWorking, true);

  const main = ChatEvents.planMainWorkingAfterSend('ship it', isAsideWireFixture);
  assert.strictEqual(main.openMainWorking, true);
  assert.strictEqual(main.suppressMainWorking, false);

  // Mutation proof: re-plan same inputs stable; flip isAsideWire flips result.
  const again = ChatEvents.planMainWorkingAfterSend(
    '[attention:att-x|t]\nhello',
    isAsideWireFixture,
  );
  assert.deepStrictEqual(again, att);
  const asMain = ChatEvents.planMainWorkingAfterSend(
    '[attention:att-x|t]\nhello',
    function () { return false; },
  );
  assert.strictEqual(asMain.openMainWorking, true, 'without aside detector, wire would bind');
  assert.strictEqual(asMain.suppressMainWorking, false);
});

test('T327 index.html: route-to-aside path leaves main WorkingProgress not open', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(
    html.includes('planMainWorkingAfterSend') || html.includes('shouldBindMainWorkingChrome'),
    'submit path must use T327 pure bind decision',
  );
  assert.ok(
    /mainWorkingSuppressed|suppressMainWorking/.test(html),
    'must track suppress so level/tool re-arm cannot reopen main mid aside turn',
  );
  // submitWireText must not unconditionally setWorking(true) for aside wires.
  assert.ok(
    /function submitWireText[\s\S]{0,2500}(planMainWorkingAfterSend|shouldBindMainWorkingChrome)/.test(html),
    'submitWireText must plan main working (T327)',
  );
  assert.ok(
    /plan\.openMainWorking[\s\S]{0,200}setWorking\(true\)/.test(html) ||
      /openMainWorking\)\s*\{\s*[\s\S]{0,120}setWorking\(true\)/.test(html),
    'main working open only when plan says so',
  );
  assert.ok(
    /suppressMainWorking|mainWorkingSuppressed\s*=\s*true/.test(html),
    'aside route sets suppress so main stays not open',
  );
  assert.ok(
    /T327/.test(html),
    'product path documents T327',
  );
});

// ── index.html wiring ───────────────────────────────────────────

test('index.html wires ChatEvents + stream seal', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  assert.ok(html.includes('scripts/chat_events.js'), 'must load chat_events.js');
  assert.ok(html.includes('ChatEvents.shouldClearWorking'), 'must call shouldClearWorking');
  assert.ok(src.includes('isSilentAssistantText'), 'apply uses isSilentAssistantText');
  assert.ok(html.includes('appendOrAddJevons'), 'must stream-merge assistant chunks');
  assert.ok(html.includes('mainConversation.applyWireEvent'), 'main live ingest is the shared apply');
  assert.ok(
    src.includes('_streamRaw'),
    'merge must key on _streamRaw (not only workingEl)',
  );
});

test('T159 index.html: open-stream handle + seal only via shouldClearWorking', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const src = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  assert.ok(/var openEl = null/.test(src), 'widget must keep explicit open-stream handle (🎯T159)');
  assert.ok(html.includes('clearOpenStreamHandle'), 'must clear handle on seal/wipe');
  // Seal path must gate on shouldClearWorking (terminal stops), not bare tool_use.
  assert.ok(
    /if\s*\(\s*ChatEvents\.shouldClearWorking\s*\(\s*m\s*\)\s*\)\s*\{[\s\S]*?sealAssistantStream\s*\(/.test(html),
    'sealAssistantStream must be gated on shouldClearWorking',
  );
  // Must not seal on tool_use stop_reason string checks inventing policy.
  assert.ok(
    !/stop_reason\s*===\s*['"]tool_use['"]/.test(html),
    'must not special-case tool_use string for seal policy',
  );
});

// ── 🎯T161 structural segment-edge join (replaces T147 content sniff) ─
// joinAssistantSegments: blank line only when neither side has a boundary
// break — never inspects for ``` / capital / lists / word counts.
// appendAssistantStream: bare concat for intra-segment tokens.
// applyChatEvents: segment join only after tool_use/tool_result or multi-block.

test('T161 joinAssistantSegments: two segments without boundary NL get blank line', () => {
  const out = ChatEvents.joinAssistantSegments(
    'First paragraph ends here.',
    'Second paragraph starts here.',
  );
  assert.strictEqual(out, 'First paragraph ends here.\n\nSecond paragraph starts here.');
  assert.ok(!out.includes('here.Second'), 'must not smash segments');
});

test('T161 joinAssistantSegments: same rule for fence/list/heading payloads', () => {
  // Same structural join for all segment strings — no content special-cases.
  assert.strictEqual(
    ChatEvents.joinAssistantSegments('Intro.', '```cpp\ncode\n```'),
    'Intro.\n\n```cpp\ncode\n```',
  );
  assert.strictEqual(
    ChatEvents.joinAssistantSegments('Here are the items:', '- first\n- second'),
    'Here are the items:\n\n- first\n- second',
  );
  assert.strictEqual(
    ChatEvents.joinAssistantSegments('Overview.', '# Title\nbody'),
    'Overview.\n\n# Title\nbody',
  );
});

test('T161 joinAssistantSegments: existing boundary break is unchanged', () => {
  assert.strictEqual(
    ChatEvents.joinAssistantSegments('Intro.\n', '```cpp\nx\n```'),
    'Intro.\n```cpp\nx\n```',
  );
  assert.strictEqual(
    ChatEvents.joinAssistantSegments('Intro.', '\n```cpp\nx\n```'),
    'Intro.\n```cpp\nx\n```',
  );
});

test('T161 joinAssistantSegments: empty/null edges', () => {
  assert.strictEqual(ChatEvents.joinAssistantSegments('', '```\nx\n```'), '```\nx\n```');
  assert.strictEqual(ChatEvents.joinAssistantSegments('Intro.', ''), 'Intro.');
  assert.strictEqual(ChatEvents.joinAssistantSegments(null, '```\n```'), '```\n```');
  assert.strictEqual(ChatEvents.joinAssistantSegments('x', null), 'x');
});

test('T161 appendAssistantStream: always bare concat (token stream)', () => {
  assert.strictEqual(ChatEvents.appendAssistantStream('Hello', '.'), 'Hello.');
  assert.strictEqual(ChatEvents.appendAssistantStream('a', 'b'), 'ab');
  assert.strictEqual(ChatEvents.appendAssistantStream('Hello.', 'What'), 'Hello.What');
  assert.strictEqual(ChatEvents.appendAssistantStream('Intro.', '```cpp'), 'Intro.```cpp');
});

test('T161 joinAssistantTexts: multi-part segments always structural join', () => {
  const out = ChatEvents.joinAssistantTexts([
    'Checking conventions briefly, then a small clean snippet.',
    '```cpp\nint main() {}\n```',
  ]);
  assert.strictEqual(
    out,
    'Checking conventions briefly, then a small clean snippet.\n\n```cpp\nint main() {}\n```',
  );
  assert.strictEqual(
    ChatEvents.joinAssistantTexts([
      'First paragraph ends here.',
      'Second paragraph starts here.',
    ]),
    'First paragraph ends here.\n\nSecond paragraph starts here.',
  );
});

test('T161 applyChatEvents: continuous token stream stays bare-fused', () => {
  const tokens = ['Hello', '.', 'What', 'do', 'you', 'need', '?'];
  const events = [user('hello'), ...tokens.map(chunk), endTurn()];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.assistantBubbles[0], tokens.join(''));
});

test('T161 applyChatEvents: tool_use gap then next segment gets structural join', () => {
  // Prose segment, tool frame, then next segment (prose / fence / list — same path).
  const events = [
    user('explain two points'),
    chunk('First paragraph ends here.'),
    {
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'read_file', input: {} }] },
    },
    chunk('Second paragraph starts here.'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(
    state.assistantBubbles[0],
    'First paragraph ends here.\n\nSecond paragraph starts here.',
  );
});

test('T161 applyChatEvents: tool gap then fence segment (same structural path)', () => {
  const events = [
    user('write a snippet of C++ code'),
    chunk('Checking C++ conventions briefly, then a small clean snippet.'),
    {
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'read_file', input: {} }] },
    },
    chunk('```cpp\nint x = 1;\n```'),
    endTurn(),
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(
    state.assistantBubbles[0],
    'Checking C++ conventions briefly, then a small clean snippet.\n\n```cpp\nint x = 1;\n```',
  );
});

test('T161 applyChatEvents: multi-block text parts join as segments', () => {
  const events = [
    user('x'),
    {
      type: 'assistant',
      message: {
        content: [
          { type: 'text', text: 'First paragraph ends here.' },
          { type: 'text', text: 'Second paragraph starts here.' },
        ],
        stop_reason: 'end_turn',
      },
    },
  ];
  const state = ChatEvents.applyChatEvents(events);
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(
    state.assistantBubbles[0],
    'First paragraph ends here.\n\nSecond paragraph starts here.',
  );
});

test('T161 index.html: segment-edge join wired; no bare += / join(\'\')', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const widget = fs.readFileSync(path.join(__dirname, 'conversation_widget.js'), 'utf8');
  assert.ok(
    widget.includes('joinAssistantSegments'),
    'widget must wire ChatEvents.joinAssistantSegments',
  );
  assert.ok(
    widget.includes('appendAssistantStream'),
    'widget must wire ChatEvents.appendAssistantStream',
  );
  assert.ok(
    html.includes('ChatEvents.joinAssistantTexts') || widget.includes('joinAssistantTexts'),
    'must wire ChatEvents.joinAssistantTexts',
  );
  assert.ok(
    html.includes('segmentEdge') || html.includes('streamSegmentEdge'),
    'must track structural segment edge from protocol',
  );
  assert.ok(
    !html.includes('_streamRaw +='),
    'must not bare-append _streamRaw',
  );
  assert.ok(
    !html.includes('pending.raw +='),
    'must not bare-append pending.raw',
  );
  assert.ok(
    !/\.map\(\s*b\s*=>\s*b\.text\s*\)\.join\(\s*['"]{2}\s*\)/.test(html),
    'must not join assistant text blocks with join(\'\')',
  );
  // No content-sniff join helpers in product path.
  assert.ok(
    !/needsJoinBreak|looksLikeParagraphSegment|SENTENCE_END_RE/.test(html),
    'must not embed content-sniff helpers in index.html',
  );
});

// ── regression guard: old clear-on-first-text policy is gone ────

test('regression guard: hasText without stop must not clear', () => {
  // This was the broken policy that caused the screenshot.
  const m = chunk('Hello');
  assert.strictEqual(ChatEvents.hasAssistantText(m), true);
  assert.strictEqual(ChatEvents.stopReason(m), '');
  assert.strictEqual(ChatEvents.shouldClearWorking(m), false);
});

// ── 🎯T279 post-send owner-turn retention ─────────────────────────
// Vanish path: wire.accept + clear composer, then soft reconnect suppresses
// user echo until history_meta → no bubble. Fix: optimistic paint + merge
// keep pending owner turns across hydrate/WS reconcile.

function isAsideWireFixture(t) {
  const s = String(t || '');
  return /^\s*\[(?:attention|target-aside)\s*:/i.test(s);
}

test('T279 planOptimisticMainUserPaint paints plain owner text', () => {
  const plan = ChatEvents.planOptimisticMainUserPaint(
    [{ text: 'earlier' }],
    'I just submitted a message',
    isAsideWireFixture,
  );
  assert.strictEqual(plan.paint, true);
  assert.strictEqual(plan.text, 'I just submitted a message');
  assert.strictEqual(plan.reason, 'optimistic');
});

test('T279 planOptimisticMainUserPaint skips aside wires (main never paints)', () => {
  const wire = '[target-aside: att-x | play spin]\nclicking play shows spinner';
  const plan = ChatEvents.planOptimisticMainUserPaint([], wire, isAsideWireFixture);
  assert.strictEqual(plan.paint, false);
  assert.strictEqual(plan.reason, 'aside-wire');
});

test('T279 planOptimisticMainUserPaint dedupes when already last', () => {
  const plan = ChatEvents.planOptimisticMainUserPaint(
    [{ text: 'same body' }],
    'same body',
    isAsideWireFixture,
  );
  assert.strictEqual(plan.paint, false);
  assert.strictEqual(plan.reason, 'already-last');
});

test('T279 planRetainOwnerTurns keeps optimistic missing from shorter hydrate', () => {
  // Simulate: optimistic painted [a, b, owner-new]; hydrate/WS list shorter omits owner-new.
  const painted = ['a', 'b', 'owner-new'];
  const hydrateOnly = ['a', 'b']; // shorter page / reconcile without pending
  const pending = ['owner-new'];
  // If reconcile replaced with hydrateOnly, retain must restore pending.
  const plan = ChatEvents.planRetainOwnerTurns(hydrateOnly, pending);
  assert.deepStrictEqual(plan.missing, ['owner-new']);
  assert.deepStrictEqual(plan.keepTexts, ['a', 'b', 'owner-new']);
  // Already painted: no missing.
  const plan2 = ChatEvents.planRetainOwnerTurns(painted, pending);
  assert.deepStrictEqual(plan2.missing, []);
  assert.deepStrictEqual(plan2.keepTexts, painted);
});

test('T279 planRepaintAfterSoftReconnect: unacked main body not in DOM → repaint', () => {
  const plan = ChatEvents.planRepaintAfterSoftReconnect({
    paintedUserTexts: ['old turn'],
    pendingTexts: ['vanished owner turn', '[attention:att-1|t]\naside body'],
    isAsideWire: isAsideWireFixture,
  });
  assert.deepStrictEqual(plan.repaint, ['vanished owner turn']);
  // Already painted → empty.
  const plan2 = ChatEvents.planRepaintAfterSoftReconnect({
    paintedUserTexts: ['vanished owner turn'],
    pendingTexts: ['vanished owner turn'],
    isAsideWire: isAsideWireFixture,
  });
  assert.deepStrictEqual(plan2.repaint, []);
});

test('T279 isDuplicateUserEcho matches optimistic then server echo', () => {
  assert.strictEqual(
    ChatEvents.isDuplicateUserEcho('hello owner', 'hello owner'),
    true,
  );
  assert.strictEqual(
    ChatEvents.isDuplicateUserEcho('hello owner', 'different'),
    false,
  );
  assert.strictEqual(ChatEvents.isDuplicateUserEcho('', 'x'), false);
});

// 🎯T281: unwrap-aware main echo dedupe (optimistic plain vs ACP wrapper).
test('T281 isDuplicateUserEcho unwraps user_query for equality', () => {
  assert.strictEqual(
    ChatEvents.isDuplicateUserEcho(
      'do a release.',
      '<user_query>\ndo a release.\n</user_query>',
    ),
    true,
    'wrapped echo matches plain optimistic',
  );
  assert.strictEqual(
    ChatEvents.normalizeOwnerEchoText('<user_query>hi</user_query>'),
    'hi',
  );
  assert.strictEqual(
    ChatEvents.isDuplicateUserEcho('a', 'b'),
    false,
  );
});

test('T279 applyChatEvents: optimistic-then-echo does not double userTexts', () => {
  // Pure event model: first user is optimistic equivalent; second is server echo.
  const state = ChatEvents.applyChatEvents([
    user('owner outbound'),
    user('owner outbound'), // dupe echo
  ]);
  assert.deepStrictEqual(state.userTexts, ['owner outbound']);
});

test('T279 vanish path fails without retention; passes with merge', () => {
  // Without retain: painted loses pending after "hydrate replace".
  const afterOptimistic = ['prior', 'just submitted'];
  const afterBadHydrate = ['prior']; // drop — the bug
  assert.ok(
    afterBadHydrate.indexOf('just submitted') < 0,
    'fixture: vanish path drops owner turn',
  );
  const retained = ChatEvents.planRetainOwnerTurns(afterBadHydrate, ['just submitted']);
  assert.ok(
    retained.keepTexts.indexOf('just submitted') >= 0,
    'retention merge must keep owner turn',
  );
  assert.deepStrictEqual(retained.missing, ['just submitted']);
});

test('T279 index.html: optimistic paint + soft-reconnect retain wired', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('paintOptimisticMainUser'), 'must paint optimistic main user');
  assert.ok(html.includes('retainPendingOwnerTurnsVisible'), 'must retain after soft reconnect');
  assert.ok(html.includes('planOptimisticMainUserPaint'), 'must use pure paint plan');
  assert.ok(html.includes('planRepaintAfterSoftReconnect'), 'must use pure repaint plan');
  assert.ok(
    /submitWireText[\s\S]{0,800}paintOptimisticMainUser/.test(html),
    'submitWireText must call paintOptimisticMainUser after accept',
  );
  assert.ok(
    /wasSoft[\s\S]{0,600}retainPendingOwnerTurnsVisible/.test(html),
    'soft history_meta must retain pending owner turns',
  );
  assert.ok(
    /isDuplicateUserEcho|lastHist[\s\S]{0,200}content/.test(html),
    'user echo must dedupe against optimistic paint',
  );
  // target:/aside: create must show opening on RHS immediately (not only freeform deliver).
  assert.ok(
    /showAsideOpeningWorking\(r\.threadId/.test(html) ||
      /showAsideOpeningWorking\(\s*r\.threadId/.test(html),
    'target/aside create must optimistic-paint RHS opening',
  );
  assert.ok(/T279/.test(html) || /🎯T279/.test(html), 'T279 marker in product path');
});

// ── 🎯T329 ONE shared display coalesce (main + RHS) ──────────────

test('T329 isNonBoundaryUserText: system-reminder / brief / background-task', () => {
  assert.strictEqual(
    ChatEvents.isNonBoundaryUserText(
      '<system-reminder>\nBackground task "x" completed.\n</system-reminder>',
    ),
    true,
  );
  assert.strictEqual(
    ChatEvents.isNonBoundaryUserText('[Jevons fleet standing brief]\nrules'),
    true,
  );
  assert.strictEqual(
    ChatEvents.isNonBoundaryUserText('Background task call-abc completed'),
    true,
  );
  assert.strictEqual(ChatEvents.isNonBoundaryUserText('real owner prose'), false);
  assert.strictEqual(ChatEvents.isNonBoundaryUserText('[event: worker-idle] x'), true);
});

test('T329 applyLiveDisplayFrame: multi-tool + inject → one assistant bubble', () => {
  // Real Grok multi-tool shape: text → tool_use → tool_result → system-reminder
  // user inject → more text → end_turn. One continuous assistant bubble.
  let lines = [];
  const sid = 't329-stream';
  const steps = [
    {
      type: 'assistant',
      stream_id: sid,
      message: { content: [{ type: 'text', text: 'I will read the file.' }] },
    },
    {
      type: 'assistant',
      stream_id: sid,
      message: {
        content: [{ type: 'tool_use', name: 'read_file', input: { path: 'x' } }],
        stop_reason: 'tool_use',
      },
    },
    { type: 'tool_result', content: [{ type: 'text', text: 'ok' }] },
    {
      type: 'user',
      message: {
        content:
          '<system-reminder>\nBackground task "call-1" completed (exit code: 0).\n</system-reminder>',
      },
    },
    {
      type: 'assistant',
      stream_id: sid,
      message: { content: [{ type: 'text', text: ' Then edit it.' }] },
    },
    {
      type: 'assistant',
      stream_id: sid,
      message: {
        content: [{ type: 'tool_use', name: 'search_replace', input: {} }],
        stop_reason: 'tool_use',
      },
    },
    { type: 'tool_result', content: [{ type: 'text', text: 'done' }] },
    {
      type: 'assistant',
      stream_id: sid,
      message: { content: [{ type: 'text', text: ' Done.' }] },
    },
    {
      type: 'assistant',
      stream_id: sid,
      message: { content: [], stop_reason: 'end_turn' },
    },
  ];
  for (const ev of steps) {
    lines = ChatEvents.applyLiveDisplayFrame(lines, ev);
  }
  const asst = lines.filter((l) => l && l.role === 'assistant');
  assert.strictEqual(
    asst.length,
    1,
    `expected 1 assistant bubble, got ${asst.length}: ${JSON.stringify(lines)}`,
  );
  assert.ok(asst[0].text.includes('I will read the file.'));
  assert.ok(asst[0].text.includes('Then edit it.'));
  assert.ok(asst[0].text.includes('Done.'));
  assert.ok(!asst[0]._stream, 'terminal seals stream');
  // Inject still present as user row for inspect nugget paint.
  const users = lines.filter((l) => l && l.role === 'user');
  assert.strictEqual(users.length, 1);
  assert.ok(users[0].text.indexOf('system-reminder') >= 0);
});

test('T329 coalesceLiveDisplayFrames: unlabeled multi-turn splits on owner user', () => {
  const chunks = ChatEvents.coalesceLiveDisplayFrames(
    [
      { type: 'user', message: { content: 'hello' } },
      {
        type: 'assistant',
        message: { content: [{ type: 'text', text: 'first reply' }] },
      },
      { type: 'user', message: { content: 'next' } },
      {
        type: 'assistant',
        message: { content: [{ type: 'text', text: 'second reply' }] },
      },
    ],
    { roleMap: { assistant: 'jevons' } },
  );
  assert.strictEqual(chunks.length, 4, JSON.stringify(chunks));
  assert.strictEqual(chunks[1].text, 'first reply');
  assert.strictEqual(chunks[3].text, 'second reply');
});

test('T329 applyChatEvents: non-boundary user does not re-arm working chrome', () => {
  const state = ChatEvents.applyChatEvents([
    user('owner'),
    chunk('Working on it'),
    {
      type: 'user',
      message: {
        content: '<system-reminder>\nBackground task done.\n</system-reminder>',
      },
    },
  ]);
  assert.strictEqual(state.userTexts.length, 1, 'inject not counted as owner turn');
  assert.strictEqual(state.assistantBubbles.length, 1);
  assert.strictEqual(state.working, true, 'still mid-turn after inject');
});

// ── 🎯T362 leaked protocol frames never become owner bubbles ─────

const UX_STATE_FRAME = '{"type":"ux_state","composer_blocked":false}';
const UX_STATE_BLOCKED =
  '{"type":"ux_state","composer_blocked":true,"reason":"overseer_down"}';

function userFrame(text) {
  return { type: 'user', message: { role: 'user', content: text } };
}

test('T362 isProtocolControlFrameText: frames yes, owner prose no', () => {
  [
    UX_STATE_FRAME,
    UX_STATE_BLOCKED,
    '  ' + UX_STATE_FRAME + '  ',
    '{"type":"ping"}',
    '{"type":"interrupt"}',
    '{"turns":2,"type":"rewind"}',
    '{"name":"jv-x","type":"inspect_subscribe"}',
  ].forEach((f) => {
    assert.strictEqual(ChatEvents.isProtocolControlFrameText(f), true, f);
  });
  [
    '',
    'Fix the ux_state leak please.',
    'Look at {"type":"ux_state"} in my chat and kill it.',
    '{"composer_blocked":false}',
    '{"type":""}',
    '{"type":42}',
    '["type","ux_state"]',
    '{"type":"ux_state"',
  ].forEach((p) => {
    assert.strictEqual(ChatEvents.isProtocolControlFrameText(p), false, p);
  });
});

test('T362 live wire: ux_state frame paints no .msg.user bubble', () => {
  let lines = [];
  lines = ChatEvents.applyLiveDisplayFrame(lines, userFrame(UX_STATE_FRAME));
  lines = ChatEvents.applyLiveDisplayFrame(lines, userFrame(UX_STATE_BLOCKED));
  assert.deepStrictEqual(lines, [], 'control frames must not paint');
});

test('T362 live wire: leaked frame does not seal an open jevons bubble', () => {
  let lines = [];
  const sid = 't362-stream';
  lines = ChatEvents.applyLiveDisplayFrame(lines, {
    type: 'assistant',
    stream_id: sid,
    message: { content: [{ type: 'text', text: 'Working on ' }] },
  });
  lines = ChatEvents.applyLiveDisplayFrame(lines, userFrame(UX_STATE_FRAME));
  lines = ChatEvents.applyLiveDisplayFrame(lines, {
    type: 'assistant',
    stream_id: sid,
    message: { content: [{ type: 'text', text: 'the leak.' }] },
  });
  const users = lines.filter((l) => l.role === 'user');
  assert.strictEqual(users.length, 0, 'no owner bubble from a frame');
  const asst = lines.filter((l) => l.role === 'assistant');
  assert.strictEqual(asst.length, 1, 'frame must not split the stream');
  assert.strictEqual(asst[0].text, 'Working on the leak.');
});

test('T362 hydrate: journaled ux_state lines never rehydrate as owner turns', () => {
  // The frames leaked into the durable journal before the server filtered
  // them, so reload is the path the owner actually saw them on.
  const lines = ChatEvents.coalesceLiveDisplayFrames([
    JSON.stringify(userFrame('kill the ux_state leak')),
    JSON.stringify(userFrame(UX_STATE_FRAME)),
    JSON.stringify(userFrame(UX_STATE_BLOCKED)),
    JSON.stringify({
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'On it.' }] },
    }),
  ]);
  const users = lines.filter((l) => l.role === 'user');
  assert.strictEqual(users.length, 1, 'only the real owner turn survives');
  assert.strictEqual(users[0].text, 'kill the ux_state leak');
  assert.ok(
    !lines.some((l) => /ux_state"\s*,/.test(String(l.text))),
    'no rehydrated frame body in any display line',
  );
});

test('T362 turn state: a frame is not an owner turn and re-arms nothing', () => {
  let state = ChatEvents.createTurnState();
  state = ChatEvents.applyChatEvent(state, userFrame(UX_STATE_FRAME));
  assert.strictEqual(state.userTexts.length, 0, 'not counted as an owner turn');
  assert.strictEqual(state.working, false, 'must not re-arm working chrome');

  // Real prose still arms it — the gate is not swallowing owner turns.
  state = ChatEvents.applyChatEvent(state, userFrame('real owner prose'));
  assert.deepStrictEqual(state.userTexts, ['real owner prose']);
  assert.strictEqual(state.working, true);
});

test('T362 send + soft reconnect never paint a control frame', () => {
  assert.strictEqual(
    ChatEvents.planOptimisticMainUserPaint([], UX_STATE_FRAME).paint,
    false,
  );
  assert.strictEqual(
    ChatEvents.planOptimisticMainUserPaint([], UX_STATE_FRAME).reason,
    'protocol-frame',
  );
  assert.strictEqual(
    ChatEvents.planOptimisticMainUserPaint([], 'ship it').paint,
    true,
  );
  const plan = ChatEvents.planRepaintAfterSoftReconnect({
    paintedUserTexts: [],
    pendingTexts: [UX_STATE_FRAME, 'ship it'],
  });
  assert.deepStrictEqual(plan.repaint, ['ship it']);
});

// ── 🎯T381 turn provenance decides how a user-role bubble paints ──

test('T381: turn origin is read from the wire, and unmarked means owner', () => {
  const agentReport = {
    type: 'user',
    turn_origin: 'agent',
    message: { role: 'user', content: '## Oracle evidence' },
  };
  const ownerTyped = {
    type: 'user',
    message: { role: 'user', content: '## and this stays literal' },
  };
  assert.strictEqual(ChatEvents.turnOriginOf(agentReport), 'agent');
  assert.strictEqual(ChatEvents.turnOriginOf(ownerTyped), 'owner');
  // Verbatim is the safe default everywhere it is not stated otherwise: a
  // journal line written before the field existed, a provider that never
  // learned it, junk, or nothing at all.
  assert.strictEqual(ChatEvents.turnOriginOf(null), 'owner');
  assert.strictEqual(ChatEvents.turnOriginOf({ turn_origin: '' }), 'owner');
  assert.strictEqual(ChatEvents.turnOriginOf({ turn_origin: 'nonsense' }), 'owner');
  assert.strictEqual(ChatEvents.turnOriginOf({ turn_origin: ' AGENT ' }), 'agent');
});

test('T381: only an agent-origin user bubble paints markdown', () => {
  assert.strictEqual(ChatEvents.bubblePaintsMarkdown('user', 'agent'), true);
  // The half a careless fix breaks: owner input is verbatim ON PURPOSE.
  assert.strictEqual(ChatEvents.bubblePaintsMarkdown('user', 'owner'), false);
  assert.strictEqual(ChatEvents.bubblePaintsMarkdown('user', undefined), false);
  // Assistant turns were never in question.
  assert.strictEqual(ChatEvents.bubblePaintsMarkdown('jevons', 'owner'), true);
  assert.strictEqual(ChatEvents.bubblePaintsMarkdown('assistant', 'owner'), true);
  // Status/chrome rows are not bubbles at all.
  assert.strictEqual(ChatEvents.bubblePaintsMarkdown('status', 'agent'), false);
});

test('T381: provenance survives display coalesce, so a reload paints the same', () => {
  const report = [
    '🎯T22 SEALED',
    '',
    '**Commit:** `bec51ca`',
    '',
    '| Criterion | Status |',
    '| --- | --- |',
    '| 1 | green |',
  ].join('\n');
  const rows = ChatEvents.coalesceLiveDisplayFrames([
    { type: 'user', message: { role: 'user', content: 'why is **this** literal?' } },
    { type: 'user', turn_origin: 'agent', message: { role: 'user', content: report } },
  ], { roleMap: { assistant: 'jevons' } });
  assert.strictEqual(rows.length, 2, 'both turns hydrate');
  assert.strictEqual(rows[0].role, 'user');
  assert.ok(!rows[0].origin, 'owner row carries no origin marker — absence IS owner');
  assert.strictEqual(rows[1].role, 'user');
  assert.strictEqual(rows[1].origin, 'agent', 'agent report keeps its provenance through hydrate');
  assert.strictEqual(rows[1].text, report, 'report text is not rewritten, only classified');
});

// ── Go package tests ────────────────────────────────────────────

test('go chat wire + roundtrip tests pass', () => {
  const r = spawnSync('go', [
    'test', './internal/server/', '-count=1',
    '-run', 'TestChat|TestDeliver|TestHandleAgent|TestUIContract|TestMultiChunk|TestUXState|TestNonControlMessages',
  ], {
    cwd: path.join(__dirname, '..', '..'),
    encoding: 'utf8',
    timeout: 120000,
  });
  if (r.status !== 0) {
    throw new Error((r.stdout || '') + (r.stderr || '') || `exit ${r.status}`);
  }
});

if (failed) {
  console.error(`\n${failed} test(s) failed`);
  process.exit(1);
}
console.log('\nall chat_events tests passed');
