// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for the owner-vs-notification split (🎯T63).
// Drives window.handle() with wire events and asserts:
//   * a genuine user event renders as an owner bubble
//   * an agent_note event does NOT render as a bubble — it folds into the
//     turn's activity strip as a readable "<agent> → <gist>" step
//   * a tool_use step shows the real tool name (not a bare "tool_use:")
//   * no bubble anywhere contains the raw "[Agent … responded]" text
//
//   node scripts/chat-ui-test/agent-note-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 'agent-note-test', ok: true }));
        return;
      }
      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` }));
    srv.on('error', reject);
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.handle === 'function' && !!window.marked, null, { timeout: 10000 });

    await page.evaluate(() => {
      // A genuine owner turn (server already elides the [user] prefix).
      window.handle({ type: 'user', message: { role: 'user', content: 'start the po and brief it' } });
      // An injected worker reply — must NOT become a bubble.
      window.handle({ type: 'agent_note', text: '[Agent jevons-po responded]\nPONG-T63 done' });
      // A real tool step (server normalised the ACP tool_call to a name).
      window.handle({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'tool_use', name: 'search_tool' }] } });
      // The overseer's own reply — a normal bubble.
      window.handle({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: 'Briefed it.' }], stop_reason: 'end_turn' } });
    });
    await page.waitForTimeout(150);

    const state = await page.evaluate(() => {
      const bubbles = Array.from(document.querySelectorAll('#messages .msg'));
      const bubbleText = bubbles.map(b => b.textContent);
      const tip = document.querySelector('#messages .turn-tip');
      const items = tip ? Array.from(tip.querySelectorAll('.turn-item')).map(i => i.textContent) : [];
      return {
        userBubbles: bubbles.filter(b => b.classList.contains('user')).map(b => b.textContent),
        anyBubbleHasNote: bubbleText.some(t => /\[Agent .* responded\]/.test(t)),
        jevonsBubbles: bubbles.filter(b => b.classList.contains('jevons')).map(b => b.textContent.trim()),
        stepItems: items,
        hasMarker: !!document.querySelector('#messages .turn-marker'),
      };
    });

    if (!state.userBubbles.some(t => t.includes('start the po and brief it'))) {
      failures.push('owner message did not render as a user bubble');
    }
    if (state.anyBubbleHasNote) {
      failures.push('an [Agent … responded] notification leaked into a chat bubble');
    }
    if (!state.hasMarker) failures.push('no activity marker created for the notification/tool steps');
    if (!state.stepItems.some(t => /jevons-po\s*→\s*PONG-T63/.test(t))) {
      failures.push(`agent_note not shown as a readable step; steps=${JSON.stringify(state.stepItems)}`);
    }
    if (!state.stepItems.some(t => t.includes('search_tool'))) {
      failures.push(`tool step lacks the real tool name; steps=${JSON.stringify(state.stepItems)}`);
    }
    if (state.stepItems.some(t => /^tool_use:?$/.test(t.trim()))) {
      failures.push('a bare "tool_use:" step is still present');
    }
    if (!state.jevonsBubbles.some(t => t.includes('Briefed it.'))) {
      failures.push('overseer reply did not render as a bubble');
    }

    await page.screenshot({ path: path.join(OUT_DIR, 'agent-note.png'), fullPage: true });
  } catch (e) {
    failures.push('exception: ' + e.message);
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL agent-note-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - owner bubbles only; notifications + tools fold into readable activity steps');
})();
