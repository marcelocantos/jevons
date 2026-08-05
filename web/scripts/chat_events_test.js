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

test('empty-content end_turn clears (Grok ACP terminal)', () => {
  assert.strictEqual(ChatEvents.shouldClearWorking(endTurn()), true);
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

test('T249 index.html: resolveOpenStreamEl re-homes same stream_id (no multi-bubble)', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('resolveOpenStreamEl'), 'must resolve open stream before minting bubble');
  assert.ok(html.includes('🎯T249') || html.includes('T249'), 'T249 marker in live paint path');
  // isConnected alone must not mint a second bubble for the same stream_id.
  assert.ok(
    !/openStreamById\[streamId\]\.isConnected\s*&&[\s\S]{0,80}typeof openStreamById\[streamId\]\._streamRaw/.test(html),
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
  assert.ok(html.includes('openStreamById'), 'must keep stream_id → el map');
  assert.ok(html.includes('stream_id') || html.includes('streamId'), 'must read wire stream id');
  // User path must not call sealAssistantStream (T223 pin).
  const userBlock = html.match(/if\s*\(\s*typ\s*===\s*['"]user['"]\s*\)\s*\{[\s\S]*?\} else if\s*\(\s*typ\s*===\s*['"]assistant['"]/);
  assert.ok(userBlock, 'user handle block must exist');
  assert.ok(
    !/sealAssistantStream\s*\(/.test(userBlock[0]),
    'user mid-stream must not sealAssistantStream',
  );
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
});

// ── index.html wiring ───────────────────────────────────────────

test('index.html wires ChatEvents + stream seal', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('scripts/chat_events.js'), 'must load chat_events.js');
  assert.ok(html.includes('ChatEvents.shouldClearWorking'), 'must call shouldClearWorking');
  assert.ok(html.includes('ChatEvents.hasAssistantText'), 'must use hasAssistantText');
  assert.ok(html.includes('appendOrAddJevons'), 'must stream-merge assistant chunks');
  assert.ok(html.includes('sealAssistantStream'), 'must seal stream on terminal');
  assert.ok(
    html.includes('_streamRaw'),
    'merge must key on _streamRaw (not only workingEl)',
  );
});

test('T159 index.html: openStreamEl handle + seal only via shouldClearWorking', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.includes('openStreamEl'), 'must keep explicit open-stream handle (🎯T159)');
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
  assert.ok(
    html.includes('ChatEvents.joinAssistantSegments'),
    'must wire ChatEvents.joinAssistantSegments',
  );
  assert.ok(
    html.includes('ChatEvents.appendAssistantStream'),
    'must wire ChatEvents.appendAssistantStream',
  );
  assert.ok(
    html.includes('ChatEvents.joinAssistantTexts'),
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

// ── Go package tests ────────────────────────────────────────────

test('go chat wire + roundtrip tests pass', () => {
  const r = spawnSync('go', [
    'test', './internal/server/', '-count=1',
    '-run', 'TestChat|TestDeliver|TestHandleAgent|TestUIContract|TestMultiChunk',
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
