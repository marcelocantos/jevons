// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const AT = require('./agent_transcript.js');

let failed = 0;
function test(name, fn) {
  try {
    fn();
    console.log('ok  -', name);
  } catch (e) {
    failed++;
    console.error('FAIL-', name);
    console.error('    ', e && e.stack ? e.stack.split('\n').slice(0, 5).join('\n     ') : e);
  }
}

test('nextSelection toggles off', function () {
  assert.strictEqual(AT.nextSelection(null, 'po'), 'po');
  assert.strictEqual(AT.nextSelection('po', 'po'), null);
  assert.strictEqual(AT.nextSelection('po', 'worker'), 'worker');
});

test('overseer root never opens transcript pane (T124 residual)', function () {
  assert.strictEqual(AT.isOverseer('jevons'), true);
  assert.strictEqual(AT.isOverseer('Jevons'), true);
  assert.strictEqual(AT.isOverseer('po', 'work'), false);
  assert.strictEqual(AT.isOverseer('ceo', 'overseer'), true);
  assert.strictEqual(AT.shouldOpenTranscript('jevons'), false);
  assert.strictEqual(AT.shouldOpenTranscript('worker', 'work'), true);
  // Click overseer: clear / ignore inspect — never select for pane.
  assert.strictEqual(AT.nextSelection(null, 'jevons'), null);
  assert.strictEqual(AT.nextSelection('po', 'jevons'), null);
  assert.strictEqual(AT.nextSelection(null, 'ceo', { purpose: 'overseer' }), null);
  // Auto-select must not pick overseer even if mis-tagged as new aside name.
  const prev = [];
  const next = [{ name: 'jevons', purpose: 'overseer' }, { name: 'att-x', purpose: 'aside' }];
  assert.strictEqual(AT.pickAutoSelect(prev, next, null), 'att-x');
  assert.strictEqual(AT.pickAutoSelect([], [{ name: 'jevons', purpose: 'overseer' }], 'jevons'), null);
});

test('detectNewAsides finds purpose=aside only', function () {
  const prev = [{ name: 'jevons', purpose: 'overseer' }, { name: 'po', purpose: 'work' }];
  const next = prev.concat([
    { name: 'worker-1', purpose: 'work' },
    { name: 'att-side', purpose: 'aside' },
  ]);
  assert.deepStrictEqual(AT.detectNewAsides(prev, next), ['att-side']);
});

test('pickAutoSelect prefers new aside; keeps selection otherwise', function () {
  const prev = [{ name: 'jevons' }];
  const next = [
    { name: 'jevons', purpose: 'overseer' },
    { name: 'filing', purpose: 'aside' },
  ];
  assert.strictEqual(AT.pickAutoSelect(prev, next, null), 'filing');
  assert.strictEqual(AT.pickAutoSelect(next, next, 'filing'), 'filing');
  assert.strictEqual(AT.pickAutoSelect(next, [{ name: 'jevons' }], 'filing'), null);
});

test('paneModel maps turns; empty and error', function () {
  const m = AT.paneModel('po', {
    name: 'po',
    session_id: 's1',
    turns: [
      { role: 'user', text: 'hello' },
      { role: 'assistant', text: 'hi' },
    ],
  });
  assert.strictEqual(m.title, 'po');
  assert.strictEqual(m.lines.length, 2);
  assert.strictEqual(m.empty, false);
  const empty = AT.paneModel('x', { turns: [] });
  assert.strictEqual(empty.empty, true);
  const err = AT.paneModel('x', null, new Error('no transcript'));
  assert.ok(err.error.indexOf('no transcript') >= 0);
});

test('main chat rule constant is owner-overseer only', function () {
  assert.strictEqual(AT.MAIN_CHAT_IS_OWNER_OVERSEER_ONLY, true);
});

test('index.html wires agent inspect pane + selectAgent transcript', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('agent-inspect') >= 0 || html.indexOf('id="agent-transcript"') >= 0,
    'transcript pane host');
  assert.ok(html.indexOf('scripts/agent_transcript.js') >= 0, 'script tag');
  assert.ok(html.indexOf('/api/agents/') >= 0 && html.indexOf('transcript') >= 0,
    'fetches agent transcript');
  assert.ok(html.indexOf('pickAutoSelect') >= 0 || html.indexOf('AgentTranscript.pickAutoSelect') >= 0,
    'auto-select on new aside');
  // Must not dump fleet monologue into #messages as the select path.
  assert.ok(!/function selectAgent[\s\S]{0,800}msgs\.appendChild/.test(html),
    'selectAgent must not append to main messages');
});

// 🎯T157: RHS inspect assistant bodies use sealed markdown, not plain textContent-only.
test('T157 renderAgentInspect paints assistant via parseAssistantMarkdown', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const fn = html.match(/function renderAgentInspect\([\s\S]*?\n\}/);
  assert.ok(fn, 'renderAgentInspect present');
  const body = fn[0];
  assert.ok(body.indexOf('parseAssistantMarkdown') >= 0,
    'assistant paint must call parseAssistantMarkdown');
  assert.ok(body.indexOf('paintInspectLineBody') >= 0 || body.indexOf('innerHTML') >= 0,
    'assistant path sets HTML (not text-only for all roles)');
  // Must not force every line through textContent only.
  assert.ok(!/text\.textContent\s*=\s*line\.text/.test(body),
    'must not paint all turns with textContent = line.text');
  // CSS: assistant not force-pre-wrap; user may pre-wrap; markdown chrome present.
  assert.ok(html.indexOf('#agent-inspect-body .ai-turn.assistant .ai-text') >= 0,
    'assistant .ai-text scoped styles');
  assert.ok(html.indexOf('#agent-inspect-body .ai-turn.user .ai-text') >= 0,
    'user .ai-text pre-wrap scope');
  assert.ok(
    /#agent-inspect-body[\s\S]{0,1200}\.ai-turn\.assistant[\s\S]{0,200}pre/.test(html) ||
      html.indexOf('#agent-inspect-body .ai-turn.assistant .ai-text pre') >= 0,
    'inspect mirrors code/pre styles for assistant markdown',
  );
  assert.ok(
    html.indexOf('#agent-inspect-body .ai-turn.assistant .ai-text table') >= 0 ||
      html.indexOf('#agent-inspect-body .ai-turn.assistant .ai-text strong') >= 0,
    'inspect mirrors table/strong styles',
  );
});

test('T157 paintInspectLinesHTML fixture: bold + heading + fence → real elements', function () {
  // Stand-in for parseAssistantMarkdown (marked is CDN-only in product HTML).
  function parseLikeSeal(md) {
    const raw = String(md || '');
    // Minimal tags covering acceptance: strong, heading, pre/code fence.
    let out = raw;
    out = out.replace(/^#\s+(.+)$/m, '<h1>$1</h1>');
    out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    out = out.replace(/```[\w]*\n([\s\S]*?)```/g, '<pre><code>$1</code></pre>');
    if (out === raw) out = '<p>' + AT.escapeHtml(raw) + '</p>';
    return out;
  }
  const lines = [
    { role: 'user', text: 'plain **not** md' },
    {
      role: 'assistant',
      text: '# Status\n\nDone with **bold** work.\n\n```js\nconst x = 1;\n```',
    },
  ];
  const html = AT.paintInspectLinesHTML(lines, { parseAssistantMarkdown: parseLikeSeal });
  assert.ok(html.indexOf('class="ai-turn assistant"') >= 0, 'assistant turn wrapper');
  assert.ok(html.indexOf('<h1>') >= 0 && html.indexOf('Status') >= 0, 'heading element');
  assert.ok(html.indexOf('<strong>') >= 0 && html.indexOf('bold') >= 0, 'strong element');
  assert.ok(html.indexOf('<pre>') >= 0 && html.indexOf('<code>') >= 0, 'fence → pre/code');
  // User stays escaped plain (no strong from **not**).
  assert.ok(html.indexOf('class="ai-turn user"') >= 0, 'user turn wrapper');
  const userChunk = html.match(/<div class="ai-turn user">[\s\S]*?<\/div><\/div>/);
  assert.ok(userChunk, 'user turn chunk');
  assert.ok(/plain \*\*not\*\* md/.test(userChunk[0]),
    'user body keeps literal stars (escaped plain), not <strong>');
  assert.ok(userChunk[0].indexOf('<strong>') < 0,
    'user turn must not gain <strong> from markdown');
});

test('T157 paintInspectLineBody roles', function () {
  const md = function (t) { return '<p><strong>' + t + '</strong></p>'; };
  const a = AT.paintInspectLineBody('assistant', 'hi', { parseAssistantMarkdown: md });
  assert.strictEqual(a.mode, 'html');
  assert.ok(a.content.indexOf('<strong>') >= 0);
  const u = AT.paintInspectLineBody('user', '**x**', { parseAssistantMarkdown: md });
  assert.strictEqual(u.mode, 'text');
  assert.strictEqual(u.content, '**x**');
});

// 🎯T167: one vertical scroll surface for RHS inspect — outer body only.
// Per-turn wrappers (.ai-turn / .ai-text) must not trap wheel with overflow-y.
test('T167 single scroll: no nested overflow-y auto/scroll on turn sections', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  // Outer pane is the scroll container.
  assert.ok(
    /#agent-inspect-body\s*\{[^}]*overflow-y:\s*auto/.test(html),
    '#agent-inspect-body must be the vertical scroll container',
  );
  // Multi-turn fixture: paint turns as product DOM; assert no inline overflow traps.
  const lines = [
    { role: 'user', text: 'prompt one' },
    { role: 'assistant', text: '## long\n\n' + 'x\n'.repeat(80) },
    { role: 'user', text: 'prompt two' },
    { role: 'assistant', text: '```\n' + 'line\n'.repeat(40) + '```' },
  ];
  const body = AT.paintInspectLinesHTML(lines, {
    parseAssistantMarkdown: function (t) {
      return '<p>' + AT.escapeHtml(t) + '</p>';
    },
  });
  assert.ok((body.match(/class="ai-turn /g) || []).length >= 4, 'multi-turn inspect DOM');
  assert.ok(body.indexOf('class="ai-text"') >= 0, '.ai-text wrappers present');
  // Fixture HTML must not set overflow / max-height on turn sections.
  assert.ok(!/\boverflow(-y)?\s*[:=]\s*(auto|scroll)/i.test(body),
    'turn HTML must not set overflow auto/scroll');
  assert.ok(!/\bmax-height\s*[:=]/i.test(body),
    'turn HTML must not set max-height');
  // CSS: .ai-turn and .ai-text under inspect must not get overflow-y auto|scroll or max-height.
  // (pre may keep overflow-x for wide code — residual OK.)
  function ruleBlock(selector) {
    const re = new RegExp(
      selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}',
    );
    const m = html.match(re);
    return m ? m[1] : '';
  }
  const aiText = ruleBlock('#agent-inspect-body .ai-text');
  assert.ok(aiText.length > 0, '.ai-text rule present');
  assert.ok(!/overflow-y\s*:\s*(auto|scroll)/i.test(aiText),
    '.ai-text must not use overflow-y auto/scroll');
  assert.ok(!/max-height\s*:/i.test(aiText),
    '.ai-text must not use max-height (nested scroll trap)');
  const aiTurn = ruleBlock('#agent-inspect-body .ai-turn');
  if (aiTurn) {
    assert.ok(!/overflow-y\s*:\s*(auto|scroll)/i.test(aiTurn),
      '.ai-turn must not use overflow-y auto/scroll');
    assert.ok(!/max-height\s*:/i.test(aiTurn),
      '.ai-turn must not use max-height');
  }
  // Intentional residual: pre may overflow-x only (not vertical nest).
  const preRule = ruleBlock('#agent-inspect-body .ai-turn.assistant .ai-text pre');
  if (preRule) {
    assert.ok(!/overflow-y\s*:\s*(auto|scroll)/i.test(preRule),
      'pre must not use overflow-y auto/scroll');
  }
});

test('T136 create-aside dual-write + no attention chip wall', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('ensureFleetAside') >= 0, 'registers fleet aside on create');
  assert.ok(html.indexOf('/api/asides') >= 0, 'POST /api/asides path');
  assert.ok(html.indexOf("attentionBar.classList.remove('visible')") >= 0 ||
    html.indexOf('classList.remove("visible")') >= 0,
    'attention bar not shown for aside stack');
  assert.ok(html.indexOf('appendThreadChip') === -1, 'no chip loop for asides');
});

// Boot TDZ: updateComposerPlaceholder reads selectedAgent; renderAttention()
// and refreshAgents run at page load. Late `let selectedAgent` threw, skipped
// applyTheme + connect → dark stuck + empty transcript.
test('index.html declares selectedAgent before boot renderAttention/refreshAgents', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const decl = html.indexOf('let selectedAgent = null;');
  assert.ok(decl >= 0, 'selectedAgent declared once with let');
  assert.strictEqual((html.match(/let selectedAgent/g) || []).length, 1, 'single let selectedAgent');
  const bootRender = html.lastIndexOf("if (typeof renderAttention === 'function') renderAttention();");
  // Boot refresh may be try-wrapped; match the interval arm + immediate call.
  const bootRefresh = html.indexOf('setInterval(function () { try { refreshAgents(); } catch (_) {} }, 30000);');
  const bootRefreshAlt = html.indexOf('setInterval(refreshAgents, 30000);');
  const bootRefreshAt = bootRefresh >= 0 ? bootRefresh : bootRefreshAlt;
  const theme = html.indexOf("applyTheme((document.cookie");
  const connect = html.indexOf('\nconnect();\n');
  assert.ok(bootRender > decl, 'boot renderAttention after selectedAgent decl');
  assert.ok(bootRefreshAt > decl, 'boot refreshAgents after selectedAgent decl');
  assert.ok(theme > decl, 'applyTheme after selectedAgent decl (script reaches theme)');
  assert.ok(connect > decl, 'connect after selectedAgent decl');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall agent_transcript tests passed');
