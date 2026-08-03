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
  assert.strictEqual(state.assistantBubbles[0], 'Table then Note after tools');
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

// ── 🎯T147 join-time fence repair at segment edges ───────────────

test('T147 coalesceAssistantText: Intro. + ```cpp fence inserts blank line', () => {
  // Acceptance fixture: two frames at ACP segment boundary (not T145 alone).
  const prev = 'Intro.';
  const next = '```cpp\ncode\n```';
  const out = ChatEvents.coalesceAssistantText(prev, next);
  assert.strictEqual(out, 'Intro.\n\n```cpp\ncode\n```');
  // Must not smush: period immediately followed by fence.
  assert.ok(!out.includes('.```'), 'must not produce smushed prose.```');
  assert.ok(/\n```cpp/.test(out), 'fence must start after a newline');
});

test('T147 coalesceAssistantText: already-newline prev is unchanged join', () => {
  const out = ChatEvents.coalesceAssistantText('Intro.\n', '```cpp\nx\n```');
  assert.strictEqual(out, 'Intro.\n```cpp\nx\n```');
});

test('T147 coalesceAssistantText: non-fence next is bare concat', () => {
  assert.strictEqual(ChatEvents.coalesceAssistantText('Hello', '.'), 'Hello.');
  assert.strictEqual(ChatEvents.coalesceAssistantText('a', 'b'), 'ab');
});

test('T147 coalesceAssistantText: empty/null edges', () => {
  assert.strictEqual(ChatEvents.coalesceAssistantText('', '```\nx\n```'), '```\nx\n```');
  assert.strictEqual(ChatEvents.coalesceAssistantText('Intro.', ''), 'Intro.');
  assert.strictEqual(ChatEvents.coalesceAssistantText(null, '```\n```'), '```\n```');
  assert.strictEqual(ChatEvents.coalesceAssistantText('x', null), 'x');
});

test('T147 joinAssistantTexts: multi-part segment edges', () => {
  const out = ChatEvents.joinAssistantTexts([
    'Checking conventions briefly, then a small clean snippet.',
    '```cpp\nint main() {}\n```',
  ]);
  assert.strictEqual(
    out,
    'Checking conventions briefly, then a small clean snippet.\n\n```cpp\nint main() {}\n```',
  );
  assert.ok(!out.includes('.```'), 'join must not smush period+fence');
});

test('T147 applyChatEvents: tool_result gap then fence segment coalesces with NL', () => {
  // Proven root cause shape: prose segment, tool frames, then fence-start segment.
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
  const raw = state.assistantBubbles[0];
  assert.ok(!raw.includes('.```'), 'must not smush at tool_result gap');
  assert.ok(
    raw.includes('snippet.\n\n```cpp') || raw.includes('snippet.\n```cpp'),
    'at least one newline before fence; got: ' + JSON.stringify(raw.slice(0, 120)),
  );
  assert.strictEqual(
    raw,
    'Checking C++ conventions briefly, then a small clean snippet.\n\n```cpp\nint x = 1;\n```',
  );
});

test('T147 index.html: no bare _streamRaw += / pending.raw += / join(\'\') on assistant paths', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(
    html.includes('ChatEvents.coalesceAssistantText'),
    'must wire ChatEvents.coalesceAssistantText',
  );
  assert.ok(
    html.includes('ChatEvents.joinAssistantTexts'),
    'must wire ChatEvents.joinAssistantTexts',
  );
  // Bare stream/history joins are the T147 failure modes.
  assert.ok(
    !html.includes('_streamRaw +='),
    'must not bare-append _streamRaw (use coalesceAssistantText)',
  );
  assert.ok(
    !html.includes('pending.raw +='),
    'must not bare-append pending.raw (use coalesceAssistantText)',
  );
  // Multi-block join must not use empty-string join for assistant text.
  assert.ok(
    !/\.map\(\s*b\s*=>\s*b\.text\s*\)\.join\(\s*['"]{2}\s*\)/.test(html),
    'must not join assistant text blocks with join(\'\')',
  );
});

// ── 🎯T161 general segment-edge paragraph/block separation (T147 subset) ─
// T147 only special-cased fence openers. T161 generalizes the same join
// helper for paragraph/block edges when ACP emits separate segments
// (prose. + tool_result + next prose) that bare concat would fuse.

test('T161 coalesceAssistantText: two prose paragraphs without NL get blank line', () => {
  // Owner acceptance fixture: no leading/trailing NL on either segment.
  const prev = 'First paragraph ends here.';
  const next = 'Second paragraph starts here.';
  const out = ChatEvents.coalesceAssistantText(prev, next);
  assert.strictEqual(out, 'First paragraph ends here.\n\nSecond paragraph starts here.');
  assert.ok(!out.includes('here.Second'), 'must not smash paragraphs');
  assert.ok(/\.\n\nS/.test(out), 'must insert blank line at segment edge');
});

test('T161 joinAssistantTexts: multi-part prose paragraphs separated', () => {
  const out = ChatEvents.joinAssistantTexts([
    'First paragraph ends here.',
    'Second paragraph starts here.',
  ]);
  assert.strictEqual(
    out,
    'First paragraph ends here.\n\nSecond paragraph starts here.',
  );
});

test('T161 coalesceAssistantText: fence case remains T147 subset', () => {
  // Keep T147 green: Intro. + ```cpp still inserts blank line.
  const out = ChatEvents.coalesceAssistantText('Intro.', '```cpp\ncode\n```');
  assert.strictEqual(out, 'Intro.\n\n```cpp\ncode\n```');
  assert.ok(!out.includes('.```'), 'fence must not smush');
});

test('T161 coalesceAssistantText: does not invent mid-sentence breaks', () => {
  // Token glue and same-paragraph continuations stay bare concat.
  assert.strictEqual(ChatEvents.coalesceAssistantText('Hello', '.'), 'Hello.');
  assert.strictEqual(ChatEvents.coalesceAssistantText('a', 'b'), 'ab');
  assert.strictEqual(ChatEvents.coalesceAssistantText('Hel', 'lo.'), 'Hello.');
  // Single-token next after period (screenshot token stream) — not a para segment.
  assert.strictEqual(ChatEvents.coalesceAssistantText('Hello.', 'What'), 'Hello.What');
  // Leading space on next ⇒ same-paragraph continue (model already spaced).
  assert.strictEqual(
    ChatEvents.coalesceAssistantText('Done.', ' More text.'),
    'Done. More text.',
  );
  // Mid-sentence capital is rare; no sentence ender ⇒ bare concat.
  assert.strictEqual(
    ChatEvents.coalesceAssistantText('the value is', 'X'),
    'the value isX',
  );
});

test('T161 coalesceAssistantText: block openers at segment edge get blank line', () => {
  assert.strictEqual(
    ChatEvents.coalesceAssistantText('Here are the items:', '- first\n- second'),
    'Here are the items:\n\n- first\n- second',
  );
  assert.strictEqual(
    ChatEvents.coalesceAssistantText('Overview.', '# Title\nbody'),
    'Overview.\n\n# Title\nbody',
  );
});

test('T161 applyChatEvents: tool_result gap then next paragraph separates', () => {
  // Proven shape: prose segment, tool frames, then next paragraph segment.
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
