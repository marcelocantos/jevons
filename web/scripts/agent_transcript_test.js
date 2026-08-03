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
  // 🎯T209: primary path is inspect_subscribe on /ws/chat; HTTP residual OK.
  assert.ok(html.indexOf('inspect_subscribe') >= 0 || html.indexOf('subscribeAgentInspect') >= 0,
    'inspect subscribe wire path');
  assert.ok(html.indexOf('/api/agents/') >= 0 && html.indexOf('transcript') >= 0,
    'HTTP transcript residual for debug/export');
  assert.ok(html.indexOf('pickAutoSelect') >= 0 || html.indexOf('AgentTranscript.pickAutoSelect') >= 0,
    'auto-select on new aside');
  // Must not dump fleet monologue into #messages as the select path.
  assert.ok(!/function selectAgent[\s\S]{0,800}msgs\.appendChild/.test(html),
    'selectAgent must not append to main messages');
});

// 🎯T208: quiet/background inspect re-paint must not steal rhsBottomTab.
// Frontier stays active across refreshAgents + wire updates while selectedAgent set.
test('T208 quiet inspect re-paint does not setRhsBottomTab transcript', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const renderFn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(renderFn, 'renderAgentInspect present before loadAgentTranscript');
  assert.ok(!/setRhsBottomTab\s*\(/.test(renderFn[0]),
    'renderAgentInspect must not call setRhsBottomTab (quiet poll steals Frontier)');
  // selectAgent still switches to Transcript on explicit owner pick.
  const selectFn = html.match(/function selectAgent\([\s\S]*?\nfunction hideAgentInspect/);
  assert.ok(selectFn, 'selectAgent present before hideAgentInspect');
  assert.ok(
    /setRhsBottomTab\([\s\S]*?tabAfterAgentSelect\(true\)/.test(selectFn[0]) ||
      /setRhsBottomTab\([\s\S]*?['"]transcript['"]/.test(selectFn[0]),
    'selectAgent must set transcript tab on open inspect');
  // 🎯T209: body updates via agent_transcript wire; no quiet HTTP poll loop.
  assert.ok(html.indexOf('handleAgentTranscriptWire') >= 0,
    'wire handler re-paints inspect without tab steal');
  assert.ok(!/setInterval\s*\(\s*function\s*\(\s*\)\s*\{\s*if\s*\(\s*selectedAgent\s*\)\s*loadAgentTranscript/.test(html),
    'no setInterval loadAgentTranscript poll while selected');
  // Explicit tab click wiring remains.
  assert.ok(/btn\.dataset\.tab/.test(html) && /setRhsBottomTab\(btn\.dataset\.tab\)/.test(html),
    'owner tab click still sets rhsBottomTab');
});

// 🎯T209: inspect uses /ws/chat multiplex — not a required 4s HTTP poll.
test('T209 inspect wire path: subscribe on select, no setInterval poll', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const selectFn = html.match(/function selectAgent\([\s\S]*?\nfunction hideAgentInspect/);
  assert.ok(selectFn, 'selectAgent present');
  assert.ok(/subscribeAgentInspect\s*\(/.test(selectFn[0]),
    'selectAgent must subscribe inspect on wire');
  assert.ok(!/loadAgentTranscript\s*\(\s*selectedAgent/.test(selectFn[0]) ||
    /subscribeAgentInspect/.test(selectFn[0]),
    'selectAgent primary path is wire subscribe');
  const hideFn = html.match(/function hideAgentInspect\([\s\S]*?\nfunction /);
  assert.ok(hideFn && /unsubscribeAgentInspect/.test(hideFn[0]),
    'hide unsubscribes inspect');
  assert.ok(html.indexOf("typ === 'agent_transcript'") >= 0 ||
    html.indexOf('typ === "agent_transcript"') >= 0 ||
    html.indexOf("=== 'agent_transcript'") >= 0,
    'handle routes agent_transcript frames');
  // No 4s transcript poll interval (product path).
  assert.ok(html.indexOf('agentTranscriptTimer') < 0,
    'agentTranscriptTimer poll removed');
  assert.ok(!/setInterval\s*\([^)]*4000\s*\)/.test(html) ||
    !/loadAgentTranscript\(selectedAgent,\s*\{\s*quiet:\s*true\s*\}\)/.test(html),
    'no 4s quiet loadAgentTranscript interval');
  // Pure helpers exported.
  assert.ok(typeof AT.inspectSubscribeFrame === 'function');
  assert.ok(typeof AT.applyInspectLiveFrame === 'function');
  const sub = JSON.parse(AT.inspectSubscribeFrame('jv-t209-wire'));
  assert.strictEqual(sub.type, 'inspect_subscribe');
  assert.strictEqual(sub.name, 'jv-t209-wire');
  const unsub = JSON.parse(AT.inspectUnsubscribeFrame());
  assert.strictEqual(unsub.type, 'inspect_unsubscribe');
  // Progressive live coalesce.
  let lines = AT.applyInspectLiveFrame([], {
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text: 'Hel' }] },
  });
  lines = AT.applyInspectLiveFrame(lines, {
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text: 'lo' }] },
  });
  assert.strictEqual(lines.length, 1);
  assert.strictEqual(lines[0].text, 'Hello');
  lines = AT.applyInspectLiveFrame(lines, {
    type: 'assistant',
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  });
  assert.ok(!lines[0]._stream, 'terminal seals stream');
});

// 🎯T205 / T157: RHS inspect uses shared paintBody + .msg chrome (not .ai-turn fork).
test('T205 renderAgentInspect uses paintBody + .msg (not .ai-turn log panel)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const fn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(fn, 'renderAgentInspect present before loadAgentTranscript');
  const body = fn[0];
  assert.ok(body.indexOf('paintBody') >= 0,
    'must paint via shared paintBody (main sealed path)');
  assert.ok(body.indexOf('className = \'msg ') >= 0 || body.indexOf('className = "msg ') >= 0 ||
    body.indexOf("className = 'msg '") >= 0 || /className\s*=\s*['"]msg\s/.test(body) ||
    body.indexOf("'msg '") >= 0 || body.indexOf('"msg "') >= 0 || body.indexOf('msg ') >= 0,
    'must use .msg bubble class');
  assert.ok(body.indexOf('msg-body') >= 0, 'must use .msg-body');
  assert.ok(body.indexOf('ai-turn') < 0, 'must not build .ai-turn log-panel chrome');
  assert.ok(body.indexOf('applyAfterUpdate') >= 0 || body.indexOf('shouldPin') >= 0,
    'must apply stick/free after update (not unconditional pin only)');
  // No unconditional always-on pin without tracking gate.
  assert.ok(!/agentInspectBody\.scrollTop\s*=\s*agentInspectBody\.scrollHeight\s*;\s*\n\}/.test(body),
    'must not unconditionally set scrollTop = scrollHeight at end of render');
  // CSS: no mirrored markdown fork under .ai-turn
  assert.ok(html.indexOf('#agent-inspect-body .ai-turn.assistant .ai-text') < 0,
    'must not mirror .msg.jevons under .ai-turn CSS fork');
  assert.ok(html.indexOf('overflow-anchor: none') >= 0 || html.indexOf('overflow-anchor:none') >= 0,
    'inspect scroll container uses overflow-anchor:none like main');
  assert.ok(/#agent-inspect-body\s*\{[^}]*overflow-anchor\s*:\s*none/.test(html),
    '#agent-inspect-body must set overflow-anchor:none');
  // Shared chrome: global .msg.jevons styles remain; inspect hosts .msg
  assert.ok(html.indexOf('.msg.jevons pre') >= 0, 'main .msg.jevons chrome still present');
  assert.ok(html.indexOf('createScrollFollow') >= 0, 'wires createScrollFollow');
  assert.ok(html.indexOf('linesFingerprint') >= 0, 'poll fingerprint skip path');
});

test('T205 paintInspectLinesHTML: .msg chrome + bold/heading/fence + user path', function () {
  function parseLikeSeal(md) {
    const raw = String(md || '');
    let out = raw;
    out = out.replace(/^#\s+(.+)$/m, '<h1>$1</h1>');
    out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    out = out.replace(/```[\w]*\n([\s\S]*?)```/g, '<pre><code>$1</code></pre>');
    if (out === raw) out = '<p>' + AT.escapeHtml(raw) + '</p>';
    return out;
  }
  function userPath(t) {
    // Stand-in for renderUserText: escape; promote `> quote` lines.
    const s = String(t || '');
    if (/^>\s?/m.test(s)) {
      return s.split('\n').map(function (line) {
        const m = line.match(/^\s*>\s?(.*)$/);
        return m ? '<blockquote>' + AT.escapeHtml(m[1]) + '</blockquote>' : AT.escapeHtml(line);
      }).join('');
    }
    return AT.escapeHtml(s);
  }
  const lines = [
    { role: 'user', text: 'plain no-md text' },
    {
      role: 'assistant',
      text: '# Status\n\nDone with **bold** work.\n\n```js\nconst x = 1;\n```',
    },
    { role: 'user', text: '> quoted line' },
  ];
  const html = AT.paintInspectLinesHTML(lines, {
    parseAssistantMarkdown: parseLikeSeal,
    renderUserText: userPath,
  });
  assert.ok(html.indexOf('class="msg jevons"') >= 0, 'assistant → .msg.jevons');
  assert.ok(html.indexOf('class="msg user"') >= 0, 'user → .msg.user');
  assert.ok(html.indexOf('class="msg-body"') >= 0, '.msg-body present');
  assert.ok(html.indexOf('ai-turn') < 0, 'no .ai-turn in fixture');
  assert.ok(html.indexOf('<h1>') >= 0 && html.indexOf('Status') >= 0, 'heading element');
  assert.ok(html.indexOf('<strong>') >= 0 && html.indexOf('bold') >= 0, 'strong element');
  assert.ok(html.indexOf('<pre>') >= 0 && html.indexOf('<code>') >= 0, 'fence → pre/code');
  // Non-MD user stays on renderUserText (no marked strong); quotes still work.
  const userChunks = html.match(/<div class="msg user">[\s\S]*?<\/div>/g) || [];
  assert.ok(userChunks.length >= 2, 'two user turns');
  assert.ok(userChunks[0].indexOf('plain no-md text') >= 0, 'plain user text present');
  assert.ok(userChunks[0].indexOf('<strong>') < 0,
    'non-MD user turn must not gain <strong>');
  assert.ok(userChunks[1].indexOf('<blockquote>') >= 0, 'user quotes via renderUserText');
});

test('T205 paintInspectLineBody roles + msgRole', function () {
  const md = function (t) { return '<p><strong>' + t + '</strong></p>'; };
  const a = AT.paintInspectLineBody('assistant', 'hi', { parseAssistantMarkdown: md });
  assert.strictEqual(a.mode, 'html');
  assert.strictEqual(a.msgRole, 'jevons');
  assert.ok(a.content.indexOf('<strong>') >= 0);
  // 🎯T221: MD-shaped user with parseAssistantMarkdown → html strong path.
  const u = AT.paintInspectLineBody('user', '**x**', { parseAssistantMarkdown: md });
  assert.strictEqual(u.mode, 'html');
  assert.strictEqual(u.msgRole, 'user');
  assert.ok(u.content.indexOf('<strong>') >= 0, 'MD-shaped user uses marked path');
  // Plain user without MD markers: renderUserText path.
  const u2 = AT.paintInspectLineBody('user', '> q', {
    renderUserText: function (t) { return '<blockquote>' + t.slice(2) + '</blockquote>'; },
  });
  assert.strictEqual(u2.mode, 'html');
  assert.ok(u2.content.indexOf('<blockquote>') >= 0);
  // Plain user, no renderUserText, no MD → text mode.
  const u3 = AT.paintInspectLineBody('user', 'hello only');
  assert.strictEqual(u3.mode, 'text');
  assert.strictEqual(u3.content, 'hello only');
  assert.strictEqual(AT.inspectToMsgRole('assistant'), 'jevons');
  assert.strictEqual(AT.inspectToMsgRole('user'), 'user');
  assert.strictEqual(AT.inspectToMsgRole('other'), 'status');
});

// 🎯T205 stickiness pure policy (follow/preserve).
test('T205 scroll follow: track pins; free preserves prevTop', function () {
  const f = AT.createScrollFollow({ eps: 16 });
  assert.strictEqual(f.getMode(), 'track');
  assert.strictEqual(f.shouldPin(), true);
  assert.strictEqual(f.nextScrollTop({ scrollHeight: 900, prevTop: 40 }), 900,
    'track → pin to scrollHeight');

  f.leaveTrack({ scrollTop: 100, scrollHeight: 900, clientHeight: 200 });
  assert.strictEqual(f.getMode(), 'free');
  assert.strictEqual(f.shouldPin(), false);
  assert.strictEqual(f.nextScrollTop({ scrollHeight: 1200, prevTop: 100 }), 100,
    'free → preserve prevTop (not yanked to bottom)');

  // Wheel up leaves track; wheel down may re-enter when at bottom.
  f.enterTrack();
  f.onWheel(-12, { scrollTop: 50, scrollHeight: 500, clientHeight: 200 });
  assert.strictEqual(f.getMode(), 'free');

  // applyAfterUpdate against fake el
  const el = {
    scrollTop: 80,
    scrollHeight: 1000,
    clientHeight: 300,
  };
  f.enterTrack();
  f.applyAfterUpdate(el, 80);
  assert.strictEqual(el.scrollTop, 1000, 'track apply pins');

  f.leaveTrack({ scrollTop: 200, scrollHeight: 1000, clientHeight: 300 });
  el.scrollTop = 200;
  el.scrollHeight = 1400;
  f.applyAfterUpdate(el, 200);
  assert.strictEqual(el.scrollTop, 200, 'free apply preserves prevTop');
});

test('T205 scroll follow: geometry re-enter only after leaving ε band', function () {
  const f = AT.createScrollFollow({ eps: 16 });
  // Leave while still near bottom → mayEnter false until user clears band.
  const near = { scrollTop: 484, scrollHeight: 500, clientHeight: 10 }; // dist=6
  f.leaveTrack(near);
  assert.strictEqual(f.getMode(), 'free');
  // Still in band: tryEnter should not re-arm track.
  f.tryEnterFromGeometry(near);
  assert.strictEqual(f.getMode(), 'free', 'still in ε band after leave → stay free');
  // Clear band then return to bottom.
  const away = { scrollTop: 0, scrollHeight: 500, clientHeight: 100 }; // dist large
  f.tryEnterFromGeometry(away);
  assert.strictEqual(f.getMode(), 'free'); // not at bottom
  const bottom = { scrollTop: 400, scrollHeight: 500, clientHeight: 100 }; // dist=0
  f.tryEnterFromGeometry(bottom);
  assert.strictEqual(f.getMode(), 'track', 'after leaving band, at-bottom re-enters track');
});

test('T205 linesFingerprint stable; changes on new turn', function () {
  const a = [
    { role: 'user', text: 'hi' },
    { role: 'assistant', text: 'yo' },
  ];
  const b = a.concat([{ role: 'assistant', text: 'more' }]);
  assert.strictEqual(AT.linesFingerprint(a), AT.linesFingerprint(a));
  assert.notStrictEqual(AT.linesFingerprint(a), AT.linesFingerprint(b));
});

// 🎯T167: one vertical scroll surface for RHS inspect — outer body only.
test('T167 single scroll: no nested overflow-y auto/scroll on turn sections', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(
    /#agent-inspect-body\s*\{[^}]*overflow-y:\s*auto/.test(html),
    '#agent-inspect-body must be the vertical scroll container',
  );
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
  assert.ok((body.match(/class="msg /g) || []).length >= 4, 'multi-turn .msg DOM');
  assert.ok(body.indexOf('class="msg-body"') >= 0, '.msg-body wrappers present');
  assert.ok(!/\boverflow(-y)?\s*[:=]\s*(auto|scroll)/i.test(body),
    'turn HTML must not set overflow auto/scroll');
  assert.ok(!/\bmax-height\s*[:=]/i.test(body),
    'turn HTML must not set max-height');
  // CSS: per-turn under inspect must not get overflow-y auto|scroll as a nest trap.
  function ruleBlock(selector) {
    const re = new RegExp(
      selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}',
    );
    const m = html.match(re);
    return m ? m[1] : '';
  }
  const msgBody = ruleBlock('#agent-inspect-body > .msg .msg-body');
  if (msgBody) {
    assert.ok(!/overflow-y\s*:\s*(auto|scroll)/i.test(msgBody),
      '.msg-body under inspect must not use overflow-y auto/scroll');
  }
  // No residual .ai-turn overflow traps.
  assert.ok(html.indexOf('#agent-inspect-body .ai-text') < 0,
    'legacy .ai-text rules removed');
});

// 🎯T217: product path must paint assistant MD as HTML (<strong>/<pre>), not raw **.
// Live repro (2026-08-03 daily :13705): selectAgent → WS agent_transcript history →
// renderAgentInspect → paintBody('jevons') → parseAssistantMarkdown; DOM has strong/table.
// 🎯T221 supersedes prior residual that user dumps stay plain — MD-shaped user
// and <user_query> injects now mark down on the inspect path only.
test('T217 renderAgentInspect: assistant→jevons paintBody, never textContent for MD role', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const fn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(fn, 'renderAgentInspect present');
  const body = fn[0];
  // Role map: assistant must go through inspectToMsgRole → jevons (not user/status).
  assert.ok(body.indexOf('inspectToMsgRole') >= 0, 'must map via inspectToMsgRole');
  assert.ok(
    body.indexOf("line.role === 'assistant' ? 'jevons'") >= 0 ||
      body.indexOf('inspectToMsgRole(line.role)') >= 0,
    'assistant maps to jevons for paintBody role',
  );
  // Must call paintBody with msgRole for non-user (assistant/status).
  assert.ok(/paintBody\s*\(\s*d\s*,\s*msgRole\s*,/.test(body),
    'paintBody(d, msgRole, …) — role must be mapped class, not wire role');
  // Fallback when paintBody missing: still parseAssistantMarkdown for jevons (T217).
  assert.ok(body.indexOf('parseAssistantMarkdown') >= 0,
    'inspect render has parseAssistantMarkdown fallback for jevons');
  // 🎯T221: user branch uses paintInspectLineBody (inspect-only MD policy).
  assert.ok(body.indexOf('paintInspectLineBody') >= 0,
    'inspect user path uses paintInspectLineBody (T221)');
  assert.ok(body.indexOf("msgRole === 'user'") >= 0 || body.indexOf('msgRole === "user"') >= 0,
    'explicit user branch for inspect MD paint');
  // Must not be the only paint path for all roles: textContent alone for every line.
  // Allowed: textContent for status / last-resort catch — not as sole assistant path.
  assert.ok(body.indexOf("msgRole === 'jevons'") >= 0 || body.indexOf('msgRole === "jevons"') >= 0,
    'explicit jevons branch for MD paint (not blind textContent)');
  // paintBody itself: jevons uses innerHTML + parseAssistantMarkdown, not textContent.
  const paint = html.match(/function paintBody\([\s\S]*?\nfunction maybeCloseTargetAside/);
  assert.ok(paint, 'paintBody present before maybeCloseTargetAside');
  const pb = paint[0];
  assert.ok(/role\s*===\s*['"]jevons['"]/.test(pb), 'paintBody branches on jevons');
  assert.ok(pb.indexOf('parseAssistantMarkdown') >= 0, 'paintBody uses marked path for jevons');
  assert.ok(/_body\.innerHTML\s*=\s*parseAssistantMarkdown/.test(pb),
    'jevons body is innerHTML from parseAssistantMarkdown');
  // textContent for assistant would be the T217 bug; only non-jevons/non-user may use it.
  const textContentAssigns = pb.match(/_body\.textContent\s*=/g) || [];
  assert.ok(textContentAssigns.length >= 1, 'status/other may use textContent');
  // The textContent assign must sit in the else (not under jevons).
  const jevonsBlock = pb.match(/if\s*\(\s*role\s*===\s*['"]jevons['"]\s*\)\s*\{[\s\S]*?\}\s*else if/);
  assert.ok(jevonsBlock, 'jevons if-block present');
  assert.ok(jevonsBlock[0].indexOf('textContent') < 0,
    'paintBody jevons branch must not assign textContent (raw ** bug)');
  // Main chat paintBody user path stays non-MD (T221 inspect-only).
  const userBlock = pb.match(/else if\s*\(\s*role\s*===\s*['"]user['"]\s*\)\s*\{[\s\S]*?\}\s*else/);
  assert.ok(userBlock, 'paintBody user branch present');
  assert.ok(userBlock[0].indexOf('renderUserText') >= 0,
    'main paintBody user still uses renderUserText (not product-wide MD)');
  assert.ok(userBlock[0].indexOf('parseAssistantMarkdown') < 0,
    'main paintBody user must not call parseAssistantMarkdown');
});

test('T217 paintInspectLinesHTML: fixture **bold**/fence → strong/pre (not raw stars)', function () {
  function parseLikeMarked(md) {
    // Minimal stand-in for sealed parseAssistantMarkdown / marked.parse.
    let s = String(md || '');
    s = s.replace(/^##\s+(.+)$/m, '<h2>$1</h2>');
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/```[\w]*\n([\s\S]*?)```/g, '<pre><code>$1</code></pre>');
    if (s.indexOf('<') < 0) s = '<p>' + AT.escapeHtml(s) + '</p>';
    return s;
  }
  const lines = [
    { role: 'assistant', text: '## Gate\n\nDone with **bold** work.\n\n```js\nconst x = 1;\n```' },
    // Non-MD plain system dump (no ** / lists) stays non-marked residual.
    { role: 'user', text: '<system-reminder>\nplain wall only\n</system-reminder>' },
  ];
  const out = AT.paintInspectLinesHTML(lines, { parseAssistantMarkdown: parseLikeMarked });
  assert.ok(out.indexOf('class="msg jevons"') >= 0, 'assistant → .msg.jevons');
  assert.ok(out.indexOf('<strong>') >= 0 && out.indexOf('bold') >= 0,
    'assistant body must contain <strong> not only raw **');
  assert.ok(out.indexOf('<pre>') >= 0, 'assistant fence → <pre>');
  assert.ok(out.indexOf('<h2>') >= 0, 'assistant heading → <h2>');
  // Raw ** must not be the sole representation of assistant bold.
  const jevonsChunks = out.match(/<div class="msg jevons">[\s\S]*?<\/div>/g) || [];
  assert.ok(jevonsChunks.length >= 1);
  assert.ok(jevonsChunks[0].indexOf('<strong>') >= 0, 'jevons chunk has strong');
  assert.ok(!/Done with \*\*bold\*\* work/.test(jevonsChunks[0]),
    'assistant must not leave literal **bold** in HTML');
  // Residual: pure system-reminder without MD markers stays plain (no <strong>).
  const userChunks = out.match(/<div class="msg user">[\s\S]*?<\/div>/g) || [];
  assert.ok(userChunks.length >= 1);
  assert.ok(userChunks[0].indexOf('<strong>') < 0,
    'plain system-reminder residual: no marked <strong>');
  assert.ok(userChunks[0].indexOf('plain wall only') >= 0,
    'plain system-reminder text still visible');
  // Role map pure unit (no assistant→user).
  assert.strictEqual(AT.inspectToMsgRole('assistant'), 'jevons');
  assert.strictEqual(AT.inspectToMsgRole('user'), 'user');
  assert.notStrictEqual(AT.inspectToMsgRole('assistant'), 'user');
  assert.notStrictEqual(AT.inspectToMsgRole('assistant'), 'status');
});

// 🎯T221: inspect user fleet injects / MD-shaped turns → strong/list HTML (not raw **).
// Owner repro: jevons-po RHS Transcript T190 design-pin bubble as role=user
// wrapped in <user_query>…</user_query> with **Prefer option 2** and - lists.
test('T221 unwrapInspectUserText strips user_query wrapper', function () {
  const u = AT.unwrapInspectUserText(
    '<user_query>\n**Prefer option 2**\n\n- one\n- two\n</user_query>',
  );
  assert.strictEqual(u.wasWrapped, true);
  assert.ok(u.text.indexOf('**Prefer option 2**') >= 0);
  assert.ok(u.text.indexOf('<user_query>') < 0);
  assert.ok(u.text.indexOf('</user_query>') < 0);
  const plain = AT.unwrapInspectUserText('just text');
  assert.strictEqual(plain.wasWrapped, false);
  assert.strictEqual(plain.text, 'just text');
  // Nested / whitespace-tolerant
  const n = AT.unwrapInspectUserText('  <user_query attr="x"> hi </user_query>  ');
  assert.strictEqual(n.wasWrapped, true);
  assert.strictEqual(n.text, 'hi');
});

test('T221 inspectUserShouldMarkdown: inject + MD markers; plain residual', function () {
  assert.strictEqual(AT.inspectUserShouldMarkdown('hello', true), true, 'inject always MD');
  assert.strictEqual(AT.inspectUserShouldMarkdown('**Prefer option 2**', false), true);
  assert.strictEqual(AT.inspectUserShouldMarkdown('- list item here', false), true);
  assert.strictEqual(AT.inspectUserShouldMarkdown('## Head', false), true);
  assert.strictEqual(AT.inspectUserShouldMarkdown('plain wall only', false), false);
  assert.strictEqual(AT.inspectUserShouldMarkdown('', false), false);
});

test('T221 owner repro: user_query **Prefer option 2** list → strong/list HTML', function () {
  function parseLikeMarked(md) {
    let s = String(md || '');
    // Simulate marked: entities from pre-escape stay; MD becomes tags.
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    // List items (simple): consecutive - lines → <ul><li>
    const lines = s.split('\n');
    let out = '';
    let inList = false;
    for (let i = 0; i < lines.length; i++) {
      const m = lines[i].match(/^\s*[-*+]\s+(.+)$/);
      if (m) {
        if (!inList) { out += '<ul>'; inList = true; }
        out += '<li>' + m[1] + '</li>';
      } else {
        if (inList) { out += '</ul>'; inList = false; }
        if (lines[i].trim()) out += '<p>' + lines[i] + '</p>';
      }
    }
    if (inList) out += '</ul>';
    return out || s;
  }
  const inject =
    '<user_query>\n' +
    '**Prefer option 2** for the design pin.\n\n' +
    '- Keep inspect chrome shared\n' +
    '- Escape raw HTML/XSS\n' +
    '</user_query>';
  const lines = [{ role: 'user', text: inject }];
  const html = AT.paintInspectLinesHTML(lines, {
    parseAssistantMarkdown: parseLikeMarked,
    renderUserText: function (t) { return AT.escapeHtml(t); },
  });
  assert.ok(html.indexOf('class="msg user"') >= 0, 'user bubble');
  assert.ok(html.indexOf('<strong>') >= 0, 'must paint <strong> not raw stars');
  assert.ok(html.indexOf('Prefer option 2') >= 0, 'prefer-option text present');
  assert.ok(!/\*\*Prefer option 2\*\*/.test(html),
    'must not leave literal **Prefer option 2** stars');
  assert.ok(html.indexOf('<ul>') >= 0 || html.indexOf('<li>') >= 0,
    'list items become list HTML');
  assert.ok(html.indexOf('<user_query>') < 0, 'wrapper stripped from display');
  assert.ok(html.indexOf('</user_query>') < 0, 'closing wrapper stripped');
  // XSS: raw HTML in inject must not become live tags after escape+parse.
  const evil = AT.paintInspectLineBody(
    'user',
    '<user_query>**ok** <script>alert(1)</script></user_query>',
    { parseAssistantMarkdown: parseLikeMarked },
  );
  assert.strictEqual(evil.mode, 'html');
  assert.ok(evil.content.indexOf('<strong>') >= 0, 'bold still works');
  assert.ok(evil.content.indexOf('<script>') < 0, 'raw script tag escaped (no live script)');
  assert.ok(
    evil.content.indexOf('&lt;script&gt;') >= 0 || evil.content.indexOf('alert(1)') >= 0,
    'script content visible as escaped text',
  );
});

test('T221 renderAgentInspect wires paintInspectLineBody for user (inspect-only)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const fn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(fn, 'renderAgentInspect present');
  const body = fn[0];
  assert.ok(body.indexOf('paintInspectLineBody') >= 0,
    'must call paintInspectLineBody for T221 user policy');
  assert.ok(body.indexOf('T221') >= 0 || body.indexOf('user_query') >= 0
    || body.indexOf('inspect-only') >= 0,
    'T221 / inspect-only comment present');
  // Must not change main paintBody user → still renderUserText only.
  const paint = html.match(/function paintBody\([\s\S]*?\nfunction maybeCloseTargetAside/);
  assert.ok(paint, 'paintBody present');
  assert.ok(/role\s*===\s*['"]user['"][\s\S]*renderUserText/.test(paint[0]),
    'main paintBody user path still renderUserText');
});

test('T217 turnsToLines preserves assistant role (no silent map to user/other)', function () {
  const lines = AT.turnsToLines([
    { role: 'assistant', text: '**hi**' },
    { role: 'user', text: '**sys**' },
    { role: 'system', text: 'x' },
  ]);
  assert.strictEqual(lines[0].role, 'assistant');
  assert.strictEqual(lines[1].role, 'user');
  // Unknown roles pass through as-is (not forced to user).
  assert.strictEqual(lines[2].role, 'system');
  const painted = AT.paintInspectLineBody('assistant', '**hi**', {
    parseAssistantMarkdown: function (t) {
      return t.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    },
  });
  assert.strictEqual(painted.mode, 'html');
  assert.strictEqual(painted.msgRole, 'jevons');
  assert.ok(painted.content.indexOf('<strong>') >= 0);
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
