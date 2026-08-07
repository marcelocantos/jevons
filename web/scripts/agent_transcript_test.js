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
  // 🎯T233: pure system-reminder is a compact inject nugget, not a .msg.user bubble.
  assert.ok(out.indexOf('inject-nugget') >= 0, 'system-reminder → inject-nugget');
  assert.ok(out.indexOf('⋯ system') >= 0, 'system-reminder label ⋯ system');
  assert.ok(out.indexOf('plain wall only') >= 0,
    'system-reminder detail still available (hover tip)');
  assert.ok(!/<div class="msg user">[\s\S]*plain wall only/.test(out),
    'system-reminder must not paint as full user bubble');
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

// ── 🎯T233: harness injects → compacted ⋯ nuggets (inspect), owner prose bubbles ──

test('T233 classifyInspectUserLine: system-reminder / brief / event / owner residual', function () {
  const sys = AT.classifyInspectUserLine(
    '<system-reminder>\nBackground task done.\nUse get_output.\n</system-reminder>',
  );
  assert.strictEqual(sys.kind, 'inject');
  assert.strictEqual(sys.injectKind, 'system-reminder');
  assert.strictEqual(sys.label, '⋯ system');
  assert.ok(sys.detail.indexOf('Background task done') >= 0);
  assert.ok(sys.detail.indexOf('<system-reminder>') < 0, 'tags stripped from detail');

  const brief = AT.classifyInspectUserLine(
    '[Jevons fleet standing brief — apply for this whole assignment]\n\n## Delivery\n- local only\n\n[PO brief]\nDo the thing.',
  );
  assert.strictEqual(brief.kind, 'inject');
  assert.strictEqual(brief.injectKind, 'standing-brief');
  assert.strictEqual(brief.label, '⋯ brief');
  assert.ok(brief.detail.indexOf('PO brief') >= 0);

  const ev = AT.classifyInspectUserLine('[event: worker-finished] slice A landed');
  assert.strictEqual(ev.kind, 'inject');
  assert.strictEqual(ev.injectKind, 'event');
  assert.strictEqual(ev.label, '⋯ worker-finished');
  assert.ok(ev.detail.indexOf('slice A landed') >= 0);

  const daemon = AT.classifyInspectUserLine('[Daemon restart 12:00] sessions rehydrated');
  assert.strictEqual(daemon.kind, 'inject');
  assert.strictEqual(daemon.injectKind, 'daemon');
  assert.strictEqual(daemon.label, '⋯ system');

  // Owner prose residual — including MD-shaped user_query design pins (T221).
  const owner = AT.classifyInspectUserLine('Please fix the inspect chrome.');
  assert.strictEqual(owner.kind, 'owner');
  assert.strictEqual(owner.detail, 'Please fix the inspect chrome.');

  const pin = AT.classifyInspectUserLine(
    '<user_query>\n**Prefer option 2**\n\n- Keep inspect chrome\n</user_query>',
  );
  assert.strictEqual(pin.kind, 'owner', 'design pin stays owner bubble');
  assert.ok(pin.wasWrapped);
  assert.ok(pin.detail.indexOf('**Prefer option 2**') >= 0);

  // user_query wrapping a standing brief still classifies as inject.
  const wrappedBrief = AT.classifyInspectUserLine(
    '<user_query>\n[Jevons fleet standing brief — apply]\nLocal only.\n</user_query>',
  );
  assert.strictEqual(wrappedBrief.kind, 'inject');
  assert.strictEqual(wrappedBrief.injectKind, 'standing-brief');
});

test('T233 paintInjectNuggetHTML: turn-marker family + escaped hover detail', function () {
  const html = AT.paintInjectNuggetHTML(
    '⋯ system',
    'Background <script>alert(1)</script> done',
    'system-reminder',
  );
  assert.ok(html.indexOf('class="turn-marker inject-nugget"') >= 0, 'turn-marker family');
  assert.ok(html.indexOf('data-inject="system-reminder"') >= 0);
  assert.ok(html.indexOf('class="inject-label"') >= 0);
  assert.ok(html.indexOf('⋯ system') >= 0);
  assert.ok(html.indexOf('class="turn-tip"') >= 0, 'hover tip present');
  assert.ok(html.indexOf('class="turn-item inject-detail"') >= 0, 'detail path');
  assert.ok(html.indexOf('<script>') < 0, 'no live script');
  assert.ok(html.indexOf('&lt;script&gt;') >= 0, 'script escaped in tip');
});

test('T233 paintInspectLinesHTML: inject → nugget; owner → .msg.user', function () {
  const lines = [
    {
      role: 'user',
      text: '<system-reminder>\nBackground task "call-abc" completed (exit code: 0).\n</system-reminder>',
    },
    {
      role: 'user',
      text: '[Jevons fleet standing brief — apply for this whole assignment]\n\n## Status language',
    },
    { role: 'user', text: '[event: idle-nudge-brief] continue T233' },
    { role: 'user', text: 'Owner prose stays a normal bubble.' },
    { role: 'assistant', text: 'Working on it.' },
  ];
  const out = AT.paintInspectLinesHTML(lines, {
    parseAssistantMarkdown: function (t) { return '<p>' + AT.escapeHtml(t) + '</p>'; },
    renderUserText: function (t) { return AT.escapeHtml(t); },
  });
  // Injects: nugget chrome, not full user bubbles.
  assert.strictEqual((out.match(/inject-nugget/g) || []).length, 3, 'three inject nuggets');
  assert.ok(out.indexOf('⋯ system') >= 0);
  assert.ok(out.indexOf('⋯ brief') >= 0);
  assert.ok(out.indexOf('⋯ idle-nudge-brief') >= 0 || out.indexOf('⋯ event') >= 0);
  assert.ok(out.indexOf('Background task') >= 0, 'system detail in tip');
  assert.ok(out.indexOf('Status language') >= 0, 'brief detail in tip');
  // Owner residual: normal user bubble.
  assert.ok(out.indexOf('class="msg user"') >= 0, 'owner → .msg.user');
  assert.ok(out.indexOf('Owner prose stays a normal bubble.') >= 0);
  // No user bubble wrapping the system-reminder wall.
  assert.ok(!/<div class="msg user">[\s\S]*Background task/.test(out),
    'system-reminder not inside .msg.user');
  assert.ok(!/<div class="msg user">[\s\S]*fleet standing brief/.test(out),
    'standing brief not inside .msg.user');
  // Assistant still jevons bubble.
  assert.ok(out.indexOf('class="msg jevons"') >= 0);
});

test('T233 paintInspectLineBody mode=nugget for harness injects', function () {
  const n = AT.paintInspectLineBody(
    'user',
    '<system-reminder>\nplain wall only\n</system-reminder>',
  );
  assert.strictEqual(n.mode, 'nugget');
  assert.strictEqual(n.injectKind, 'system-reminder');
  assert.ok(n.content.indexOf('inject-nugget') >= 0);
  assert.ok(n.content.indexOf('plain wall only') >= 0);

  const o = AT.paintInspectLineBody('user', 'hello owner');
  assert.notStrictEqual(o.mode, 'nugget');
  assert.strictEqual(o.msgRole, 'user');
});

test('T233 renderAgentInspect wires nugget path + fingerprint inject-only', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const fn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(fn, 'renderAgentInspect present');
  const body = fn[0];
  assert.ok(body.indexOf("mode === 'nugget'") >= 0 || body.indexOf('mode === "nugget"') >= 0,
    'handles mode=nugget from paintInspectLineBody');
  assert.ok(body.indexOf('inject-nugget') >= 0 || body.indexOf('T233') >= 0,
    'T233 / inject-nugget path present');
  assert.ok(body.indexOf('inject-nugget') >= 0,
    'fingerprint skip covers inject-only transcripts');
  // CSS: inspect hosts turn-marker inject nuggets.
  assert.ok(/#agent-inspect-body\s*>\s*\.turn-marker\.inject-nugget/.test(html) ||
    html.indexOf('inject-nugget') >= 0,
    'inspect CSS for inject-nugget');
  // Main chat paintBody user path unchanged (product residual: inspect-first).
  const paint = html.match(/function paintBody\([\s\S]*?\nfunction maybeCloseTargetAside/);
  assert.ok(paint, 'paintBody present');
  assert.ok(paint[0].indexOf('nugget') < 0,
    'main paintBody does not take T233 nugget path (inspect-first residual)');
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

// 🎯T263: freeform aside create delivers opening + working chrome (not register-only).
test('T263 createAsideRequestBody includes opening text when freeform', function () {
  const bare = AT.createAsideRequestBody('att-x', 'title only');
  assert.strictEqual(bare.id, 'att-x');
  assert.strictEqual(bare.title, 'title only');
  assert.strictEqual(bare.text, undefined, 'register-only has no text');
  const withOpen = AT.createAsideRequestBody(
    'att-msftck4l-9sguxj',
    'how does bullseye compare to beads?',
    '  how does bullseye compare to beads?  ',
  );
  assert.strictEqual(withOpen.text, 'how does bullseye compare to beads?');
  assert.strictEqual(withOpen.id, 'att-msftck4l-9sguxj');
});

test('T263 freeformAsideCreateOpts only for aside: command', function () {
  // 🎯T270: kind always set for closed-history type; deliver only on freeform aside:.
  assert.deepStrictEqual(AT.freeformAsideCreateOpts('capture', 'note'), {
    kind: 'capture', command: 'capture',
  });
  assert.deepStrictEqual(AT.freeformAsideCreateOpts('target', 'file this'), {
    kind: 'target', command: 'target',
  });
  assert.deepStrictEqual(AT.freeformAsideCreateOpts('aside', '   '), {
    kind: 'side', command: 'aside',
  });
  const o = AT.freeformAsideCreateOpts('aside', 'how does bullseye compare to beads?');
  assert.strictEqual(o.text, 'how does bullseye compare to beads?');
  assert.strictEqual(o.expectDeliver, true);
  assert.strictEqual(o.kind, 'side');
});

test('T270 createAsideRequestBody carries kind', function () {
  const b = AT.createAsideRequestBody('att-x', 'title', '', 'target');
  assert.strictEqual(b.kind, 'target');
  assert.strictEqual(b.text, undefined);
  const bare = AT.createAsideRequestBody('att-y', 't');
  assert.strictEqual(bare.kind, undefined);
});

test('T263 index.html freeform create path + working chrome + loud fail', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('opts.text') >= 0 || html.indexOf('payload.text') >= 0,
    'ensureFleetAside posts opening text');
  assert.ok(html.indexOf('expectDeliver') >= 0, 'freeform expects deliver');
  assert.ok(html.indexOf('showAsideOpeningWorking') >= 0, 'working chrome helper');
  assert.ok(html.indexOf('showAsideDeliverError') >= 0, 'loud fail helper');
  assert.ok(html.indexOf('data-aside-working') >= 0, 'working indicator in inspect');
  assert.ok(html.indexOf('aside_create_deliver_error') >= 0 ||
    html.indexOf('start/deliver failed') >= 0,
    'deliver failure decision/log path');
  // Composer create path passes text for aside: only.
  assert.ok(html.indexOf("parsedCmd.command === 'aside'") >= 0);
  assert.ok(html.indexOf('createOpts.text') >= 0 || html.indexOf('createOpts =') >= 0);
});

// ── 🎯T252 auto-activate attention asides; sticky draft; next after send ──

test('T252 sidebarDraftIsEmpty treats whitespace as empty', function () {
  assert.strictEqual(AT.sidebarDraftIsEmpty(''), true);
  assert.strictEqual(AT.sidebarDraftIsEmpty('   '), true);
  assert.strictEqual(AT.sidebarDraftIsEmpty(null), true);
  assert.strictEqual(AT.sidebarDraftIsEmpty('hi'), false);
  assert.strictEqual(AT.sidebarDraftIsEmpty('  x  '), false);
});

test('T252 asideRequiresAttention: assistant-last and needs-owner', function () {
  const aside = { name: 'att-a', purpose: 'aside' };
  assert.strictEqual(AT.asideRequiresAttention(aside, { lastRole: 'assistant' }), true);
  assert.strictEqual(AT.asideRequiresAttention(aside, { lastRole: 'user' }), false);
  assert.strictEqual(AT.asideRequiresAttention(aside, {
    lines: [
      { role: 'user', text: 'q' },
      { role: 'assistant', text: 'a' },
    ],
  }), true);
  assert.strictEqual(AT.asideRequiresAttention(
    { name: 'att-b', purpose: 'aside', needs_owner: true },
    { lastRole: 'user' },
  ), true);
  assert.strictEqual(AT.asideRequiresAttention(
    { name: 'worker', purpose: 'work' },
    { lastRole: 'assistant' },
  ), false);
  assert.strictEqual(AT.asideRequiresAttention(
    { name: 'jevons', purpose: 'overseer' },
    { lastRole: 'assistant' },
  ), false);
});

test('T252 empty-composer + new attention → selection switches', function () {
  let queue = [];
  queue = AT.enqueueAttention(queue, 'att-a');
  // Viewing something else (or main); draft empty; new attention att-a
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: null,
    draftEmpty: true,
    reason: 'new-attention',
    newName: 'att-a',
  }), 'att-a');

  queue = AT.enqueueAttention(queue, 'att-b');
  // On att-a with empty draft; att-b newly needs attention → switch to att-b
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-a',
    draftEmpty: true,
    reason: 'new-attention',
    newName: 'att-b',
  }), 'att-b');
});

test('T252 non-empty draft → selection sticky (no mid-compose steal)', function () {
  const queue = ['att-a', 'att-b'];
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-a',
    draftEmpty: false,
    reason: 'new-attention',
    newName: 'att-b',
  }), 'att-a');
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-a',
    draft: 'working on a reply…',
    reason: 'new-attention',
    newName: 'att-b',
  }), 'att-a');
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-a',
    draftEmpty: false,
    reason: 'poll',
  }), 'att-a');
});

test('T252 post-send empty → next attention selected', function () {
  let queue = ['att-a', 'att-b', 'att-c'];
  // User sent on att-a → dequeue att-a, draft empty, after-send
  queue = AT.dequeueAttention(queue, 'att-a');
  assert.deepStrictEqual(queue, ['att-b', 'att-c']);
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-a',
    draftEmpty: true,
    reason: 'after-send',
  }), 'att-b');

  // Send on att-b → next att-c
  queue = AT.dequeueAttention(queue, 'att-b');
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-b',
    draftEmpty: true,
    reason: 'after-send',
  }), 'att-c');

  // Last one sent → empty queue → keep current (no forced switch residual)
  queue = AT.dequeueAttention(queue, 'att-c');
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: queue,
    currentSelection: 'att-c',
    draftEmpty: true,
    reason: 'after-send',
  }), 'att-c');
});

test('T252 residual: no attention asides → no forced switch', function () {
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: [],
    currentSelection: 'worker-1',
    draftEmpty: true,
    reason: 'poll',
  }), 'worker-1');
  assert.strictEqual(AT.pickAttentionAsideSelection({
    attentionNames: [],
    currentSelection: null,
    draftEmpty: true,
    reason: 'new-attention',
  }), null);
});

test('T252 detectNewAttentionAsides busy→idle + needs_owner flag', function () {
  const prev = [
    { name: 'att-busy', purpose: 'aside', phase: 'working' },
    { name: 'att-idle', purpose: 'aside', phase: 'idle' },
    { name: 'worker', purpose: 'work', phase: 'working' },
  ];
  const next = [
    { name: 'att-busy', purpose: 'aside', phase: 'idle' },
    { name: 'att-idle', purpose: 'aside', phase: 'idle' },
    { name: 'worker', purpose: 'work', phase: 'idle' },
    { name: 'att-flag', purpose: 'aside', needs_owner: true, phase: 'idle' },
  ];
  const news = AT.detectNewAttentionAsides(prev, next);
  assert.ok(news.indexOf('att-busy') >= 0, 'busy→idle aside');
  assert.ok(news.indexOf('att-flag') >= 0, 'needs_owner rose');
  assert.ok(news.indexOf('worker') < 0, 'work agents ignored');
  assert.ok(news.indexOf('att-idle') < 0, 'already idle no new attention');
});

test('T252 liveFrameSignalsOwnerAttention on terminal assistant stop', function () {
  assert.strictEqual(AT.liveFrameSignalsOwnerAttention({
    type: 'assistant',
    message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
  }), true);
  assert.strictEqual(AT.liveFrameSignalsOwnerAttention({
    type: 'assistant',
    message: { role: 'assistant', content: [{ type: 'text', text: '…' }] },
  }), false);
  assert.strictEqual(AT.liveFrameSignalsOwnerAttention({
    type: 'user',
    message: { content: 'hi' },
  }), false);
});

test('T252 index.html wires attention auto-select + sticky draft', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('pickAttentionAsideSelection') >= 0,
    'product calls pickAttentionAsideSelection');
  assert.ok(html.indexOf('asideAttentionQueue') >= 0 ||
    html.indexOf('enqueueAttention') >= 0,
    'attention queue state');
  assert.ok(html.indexOf('T252') >= 0, 'T252 marker in product wire');
  assert.ok(
    html.indexOf('isSidebarComposerDraftEmpty') >= 0 ||
    html.indexOf('sidebarDraftIsEmpty') >= 0,
    'draft empty gate for sticky',
  );
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
  // 🎯T289: the fallback poll became a multi-line block (hidden-tab skip), so
  // anchor on the immediate boot call — that is what actually runs at load and
  // what the TDZ guard protects.
  const bootRefreshImmediate = html.indexOf('\ntry { refreshAgents(); } catch (_) {}');
  const bootRefreshAt = bootRefresh >= 0 ? bootRefresh
    : (bootRefreshAlt >= 0 ? bootRefreshAlt : bootRefreshImmediate);
  const theme = html.indexOf("applyTheme((document.cookie");
  const connect = html.indexOf('\nconnect();\n');
  assert.ok(bootRender > decl, 'boot renderAttention after selectedAgent decl');
  assert.ok(bootRefreshAt > decl, 'boot refreshAgents after selectedAgent decl');
  assert.ok(theme > decl, 'applyTheme after selectedAgent decl (script reaches theme)');
  assert.ok(connect > decl, 'connect after selectedAgent decl');
});

// 🎯T251: sidebar Transcript composer (independent of main #input).
test('T251 sidebarComposerVisible only on transcript tab with selectable agent', function () {
  assert.strictEqual(AT.sidebarComposerVisible({
    tab: 'transcript', selectedAgent: 'att-billing', purpose: 'aside',
  }), true);
  assert.strictEqual(AT.sidebarComposerVisible({
    tab: 'transcript', selectedAgent: 'jv-t251-worker', purpose: 'work',
  }), true);
  assert.strictEqual(AT.sidebarComposerVisible({
    tab: 'frontier', selectedAgent: 'att-billing', purpose: 'aside',
  }), false, 'frontier tab hides sidebar composer');
  assert.strictEqual(AT.sidebarComposerVisible({
    tab: 'transcript', selectedAgent: null,
  }), false, 'no selection → no composer');
  assert.strictEqual(AT.sidebarComposerVisible({
    tab: 'transcript', selectedAgent: 'jevons', purpose: 'overseer',
  }), false, 'overseer never uses sidebar composer');
});

test('T251 sidebarSendRequest targets selected agent send API', function () {
  const ok = AT.sidebarSendRequest('att-msf-1', '  ship it  ');
  assert.strictEqual(ok.ok, true);
  assert.strictEqual(ok.name, 'att-msf-1');
  assert.strictEqual(ok.method, 'POST');
  assert.strictEqual(ok.url, '/api/agents/att-msf-1/send');
  assert.deepStrictEqual(ok.body, { text: 'ship it' });
  // Encoding for free-form names.
  const enc = AT.sidebarSendRequest('jv-t27.2-config', 'go');
  assert.strictEqual(enc.ok, true);
  assert.strictEqual(enc.url, '/api/agents/' + encodeURIComponent('jv-t27.2-config') + '/send');
  assert.strictEqual(AT.sidebarSendRequest(null, 'x').ok, false);
  assert.strictEqual(AT.sidebarSendRequest('att-x', '   ').reason, 'empty');
  assert.strictEqual(AT.sidebarSendRequest('jevons', 'hi').reason, 'overseer-main-only');
  assert.strictEqual(AT.agentSendPath('po'), '/api/agents/po/send');
  assert.strictEqual(AT.isSidebarDraftEmpty(''), true);
  assert.strictEqual(AT.isSidebarDraftEmpty('  \n'), true);
  assert.strictEqual(AT.isSidebarDraftEmpty('a'), false);
});

test('T251 classifySidebarComposerKey Enter sends, Shift+Enter newline', function () {
  assert.strictEqual(AT.classifySidebarComposerKey({ key: 'Enter' }), 'send');
  assert.strictEqual(AT.classifySidebarComposerKey({ key: 'Enter', shiftKey: true }), 'newline');
  assert.strictEqual(AT.classifySidebarComposerKey({ key: 'a' }), null);
  assert.strictEqual(AT.classifySidebarComposerKey({ key: 'Enter', isComposing: true }), null);
});

test('T251 index.html wires sidebar composer DOM + sendSidebarComposer path', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('id="agent-inspect-composer"') >= 0, 'composer host in transcript pane');
  assert.ok(html.indexOf('id="agent-inspect-input"') >= 0, 'sidebar message input');
  assert.ok(html.indexOf('id="agent-inspect-send"') >= 0, 'sidebar send control');
  assert.ok(html.indexOf('function sendSidebarComposer') >= 0, 'send handler');
  assert.ok(html.indexOf('function syncSidebarComposer') >= 0, 'visibility sync');
  // Send path targets selected agent via AgentTranscript.sidebarSendRequest / agent send API.
  assert.ok(html.indexOf('sidebarSendRequest') >= 0, 'uses pure send request helper');
  assert.ok(html.indexOf('__sidebarAgentSend') >= 0, 'hermetic mock seam');
  // Must not wire sidebar send through main transport / overseer #input.
  const sendFn = html.match(/function sendSidebarComposer\([\s\S]*?\nfunction |function sendSidebarComposer\([\s\S]*?\nif \(agentInspectInput\)/);
  assert.ok(sendFn, 'sendSidebarComposer body capturable');
  assert.ok(!/transport\.send\s*\(/.test(sendFn[0]),
    'sidebar send must not use main chat transport');
  assert.ok(!/\binput\.value\b/.test(sendFn[0]) || /agentInspectInput\.value/.test(sendFn[0]),
    'sidebar send reads agentInspectInput, not main #input alone');
  // Composer lives inside #agent-inspect (transcript tab panel).
  const inspectBlock = html.match(/id="agent-inspect"[\s\S]*?id="frontier-pane"|id="agent-inspect"[\s\S]*?<\/div>\s*<\/div>\s*<\/div>\s*<div id="activity-header"/);
  // Simpler structural: composer markup after agent-inspect-body, before closing agent-inspect.
  const bodyAt = html.indexOf('id="agent-inspect-body"');
  const composerAt = html.indexOf('id="agent-inspect-composer"');
  const frontierAt = html.indexOf('id="frontier-pane"');
  assert.ok(bodyAt >= 0 && composerAt > bodyAt, 'composer after transcript body');
  // agent-inspect block is after frontier-pane in markup (sibling panes).
  assert.ok(composerAt > frontierAt || frontierAt < bodyAt, 'composer is in transcript structure');
  assert.ok(html.indexOf('🎯T251') >= 0 || html.indexOf('T251') >= 0, 'T251 marker');
});

// ── 🎯T275: RHS Transcript send delivers; no silent no-op ───────────────

test('T275 sidebarSendBlockMessage is loud for every block reason', function () {
  assert.ok(AT.sidebarSendBlockMessage('no-selection').indexOf('selected') >= 0);
  assert.ok(AT.sidebarSendBlockMessage('overseer-main-only').indexOf('main chat') >= 0);
  assert.ok(AT.sidebarSendBlockMessage('empty').indexOf('empty') >= 0);
  assert.ok(AT.sidebarSendBlockMessage('observe-only').indexOf('observe-only') >= 0);
  assert.ok(AT.sidebarSendBlockMessage('').indexOf('silent') >= 0 ||
    AT.sidebarSendBlockMessage('').indexOf('unknown') >= 0);
  assert.ok(AT.sidebarSendBlockMessage('custom-x').indexOf('custom-x') >= 0);
});

test('T275 index.html: no silent no-op on sidebar send block/fail', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('function showSidebarSendErr') >= 0, 'loud err helper');
  assert.ok(html.indexOf('sidebarSendBlockMessage') >= 0, 'uses pure block copy');
  assert.ok(html.indexOf('role', 'alert') >= 0 || html.indexOf("setAttribute('role', 'alert')") >= 0 ||
    html.indexOf('setAttribute("role", "alert")') >= 0 || html.indexOf("role', 'alert'") >= 0,
    'err is alert-role');
  // Blocked pre-HTTP path must call showSidebarSendErr (not bare return).
  const sendFn = html.match(/function sendSidebarComposer\([\s\S]*?\nif \(agentInspectInput\)/);
  assert.ok(sendFn, 'sendSidebarComposer capturable');
  assert.ok(sendFn[0].indexOf('showSidebarSendErr') >= 0,
    'sendSidebarComposer surfaces block/fail loudly');
  assert.ok(sendFn[0].indexOf('no-selection') >= 0 ||
    sendFn[0].indexOf('sidebarSendBlockMessage') >= 0,
    'no-selection is loud');
  // Product path still posts to agent send API.
  assert.ok(html.indexOf('/api/agents/') >= 0 && html.indexOf('sidebarSendRequest') >= 0);
  assert.ok(html.indexOf('T275') >= 0 || html.indexOf('🎯T275') >= 0, 'T275 marker');
});

// ── 🎯T265: aside/agent Transcript microcosm of main chat ───────────────

test('T265 mergePaneModelWithLines preserves working chrome', function () {
  const merged = AT.mergePaneModelWithLines(
    {
      title: 'att-x',
      empty: true,
      error: '',
      lines: [],
      working: true,
      sessionId: 's1',
    },
    [{ role: 'user', text: 'hello' }],
  );
  assert.strictEqual(merged.working, true, 'working must survive wire merge');
  assert.strictEqual(merged.empty, false);
  assert.strictEqual(merged.title, 'att-x');
  assert.strictEqual(merged.sessionId, 's1');
  assert.strictEqual(merged.lines.length, 1);
  // Dropping working was the T205 residual that killed in-flight chrome.
  const dropped = AT.mergePaneModelWithLines({ title: 'a', working: false }, []);
  assert.strictEqual(dropped.working, false);
  assert.strictEqual(dropped.empty, true);
});

test('T265 afterSidebarSendOptimistic appends user + opens working', function () {
  const r = AT.afterSidebarSendOptimistic(
    [{ role: 'assistant', text: 'hi' }],
    '  reply please  ',
    { title: 'att-billing' },
  );
  assert.strictEqual(r.model.working, true);
  assert.strictEqual(r.model.title, 'att-billing');
  assert.strictEqual(r.model.empty, false);
  assert.strictEqual(r.lines.length, 2);
  assert.strictEqual(r.lines[1].role, 'user');
  assert.strictEqual(r.lines[1].text, 'reply please');
  // Dedupe consecutive identical owner send.
  const r2 = AT.afterSidebarSendOptimistic(r.lines, 'reply please', { title: 'att-billing' });
  assert.strictEqual(r2.lines.length, 2, 'no double user bubble');
  assert.strictEqual(r2.model.working, true);
});

// ── 🎯T281: one owner submit → one bubble (optimistic + WS reconcile) ──

test('T281 applyInspectLiveFrame: optimistic + live user echo → one bubble', function () {
  // Product path: afterSidebarSendOptimistic then agent_transcript live user.
  const opt = AT.afterSidebarSendOptimistic([], 'do a release.', { title: 'jevons-po' });
  assert.strictEqual(opt.lines.length, 1);
  assert.strictEqual(opt.lines[0].role, 'user');
  assert.strictEqual(opt.lines[0].text, 'do a release.');
  const afterEcho = AT.applyInspectLiveFrame(opt.lines, {
    type: 'user',
    message: { role: 'user', content: 'do a release.' },
  });
  assert.strictEqual(afterEcho.length, 1, 'no double bubble from optimistic+WS');
  assert.strictEqual(afterEcho[0].text, 'do a release.');
});

test('T281 applyInspectLiveFrame: unwrap-aware dedupe for user_query wrapper', function () {
  const opt = AT.afterSidebarSendOptimistic([], 'do a release.', { title: 'po' });
  const wrapped = AT.applyInspectLiveFrame(opt.lines, {
    type: 'user',
    message: {
      role: 'user',
      content: '<user_query>\ndo a release.\n</user_query>',
    },
  });
  assert.strictEqual(wrapped.length, 1, 'wrapped echo must not double plain optimistic');
  // Intentional resend after assistant still paints a second user bubble.
  let lines = AT.applyInspectLiveFrame(wrapped, {
    type: 'assistant',
    message: {
      role: 'assistant',
      content: [{ type: 'text', text: 'ok' }],
      stop_reason: 'end_turn',
    },
  });
  lines = AT.afterSidebarSendOptimistic(lines, 'do a release.', { title: 'po' }).lines;
  assert.strictEqual(
    lines.filter(function (l) { return l.role === 'user'; }).length,
    2,
    'resend after assistant is a new submit',
  );
});

test('T281 isDuplicateInspectUserLine consecutive only', function () {
  assert.ok(AT.isDuplicateInspectUserLine(
    { role: 'user', text: 'hi' },
    'hi',
  ));
  assert.ok(!AT.isDuplicateInspectUserLine(
    { role: 'assistant', text: 'ok' },
    'hi',
  ));
  assert.ok(AT.isDuplicateInspectUserLine(
    { role: 'user', text: '<user_query>hi</user_query>' },
    'hi',
  ));
  assert.strictEqual(AT.inspectUserDedupeKey('  x  '), 'x');
});

test('T281 index.html: RHS live path uses applyInspectLiveFrame; main uses isDuplicateUserEcho', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('applyInspectLiveFrame') >= 0,
    'agent_transcript live frames must go through applyInspectLiveFrame');
  assert.ok(
    html.indexOf('afterSidebarSendOptimistic') >= 0,
    'RHS send paints optimistic owner turn',
  );
  // Main: T279/T281 shared — optimistic paint + echo dedupe (not ad-hoc only).
  assert.ok(
    html.indexOf('isDuplicateUserEcho') >= 0 ||
      /msgHistory\[msgHistory\.length\s*-\s*1\]/.test(html),
    'main user echo must dedupe against last painted',
  );
  assert.ok(
    html.indexOf('paintOptimisticMainUser') >= 0 ||
      html.indexOf('planOptimisticMainUserPaint') >= 0,
    'main optimistic path present (T279/T281 shared)',
  );
});

test('T265 inspectWorkingLabel uses agent name not Jevons', function () {
  assert.strictEqual(AT.inspectWorkingLabel('att-msft'), 'att-msft is working');
  assert.strictEqual(AT.inspectWorkingLabel('jevons'), 'Working');
  assert.ok(AT.inspectWorkingLabel('a'.repeat(40)).indexOf('…') >= 0);
  assert.strictEqual(AT.inspectWorkingLabel(''), 'Working');
});

test('T265 inspectDisplayUserText strips attention wire headers', function () {
  const body = AT.inspectDisplayUserText(
    '[attention:att-x|billing nit]\nbilling body',
  );
  assert.strictEqual(body, 'billing body');
  assert.ok(body.indexOf('[attention:') < 0);
  assert.strictEqual(AT.inspectDisplayUserText('plain owner'), 'plain owner');
  const tgt = AT.inspectDisplayUserText(
    '[target-aside: att-y | title]\nfile this\n\n(Ceremony: bullseye)',
  );
  assert.ok(tgt.indexOf('file this') >= 0);
  assert.ok(tgt.indexOf('[target-aside') < 0);
  assert.ok(tgt.indexOf('Ceremony') < 0);
});

test('T265 inspect pane is conversation-only (no nested fleet/frontier)', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(AT.inspectPaneIsConversationOnly(html),
    'agent-inspect must be conversation surface only');
  // Negative fixture: nested agents host fails oracle.
  assert.strictEqual(
    AT.inspectPaneIsConversationOnly(
      '<div id="agent-inspect"><div id="agents"></div>' +
      '<div id="agent-inspect-body"></div><div id="agent-inspect-composer"></div></div>',
    ),
    false,
  );
});

test('T265 index.html: merge preserves working; send opens working chrome', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('mergePaneModelWithLines') >= 0,
    'mergeModelWithAsideWire must use mergePaneModelWithLines');
  assert.ok(html.indexOf('afterSidebarSendOptimistic') >= 0,
    'sendSidebarComposer must use afterSidebarSendOptimistic');
  assert.ok(html.indexOf('inspectWorkingLabel') >= 0,
    'inspect working chrome uses inspectWorkingLabel');
  assert.ok(/working:\s*!!\(model\s*&&\s*model\.working\)/.test(html) ||
    html.indexOf('mergePaneModelWithLines') >= 0,
    'fallback path still preserves working');
  // Microcosm markers present (no recursive shell in inspect paint path).
  const renderFn = html.match(/function renderAgentInspect\([\s\S]*?\nfunction loadAgentTranscript/);
  assert.ok(renderFn, 'renderAgentInspect present');
  assert.ok(renderFn[0].indexOf('id="agents"') < 0, 'render does not inject fleet tree');
  assert.ok(renderFn[0].indexOf('frontier-table') < 0, 'render does not inject frontier');
  assert.ok(html.indexOf('🎯T265') >= 0 || html.indexOf('T265') >= 0, 'T265 marker');
});

if (failed) {
  console.error('\n' + failed + ' failed');
  process.exit(1);
}
console.log('\nall agent_transcript tests passed');
