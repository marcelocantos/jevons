// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for 🎯T159: one assistant bubble per terminal
// stop_reason. Tool rounds and intervening non-stream .msg chrome must not
// seal or open a second bubble.
//
//   node scripts/chat-ui-test/t159-seal-test.js [--headed]

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
        res.end(JSON.stringify({ version: 't159-seal-test', ok: true }));
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
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      resolve({ srv, port });
    });
    srv.on('error', reject);
  });
}

async function main() {
  const failures = [];
  const { srv, port } = await startStaticServer();
  const base = `http://127.0.0.1:${port}/`;
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && typeof window.appendOrAddJevons === 'function',
      null,
      { timeout: 10000 },
    );

    // ── Fixture A: multi-chunk + tool_use + text + end_turn → one bubble ──
    await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      if (typeof clearOpenStreamHandle === 'function') clearOpenStreamHandle();

      window.handle({ type: 'user', message: { role: 'user', content: 'clean asides' } });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: '| Bubble | Content |\n' }] },
      });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: '|--------|---------|\n' }] },
      });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: '| 1 | table |\n' }] },
      });
      // tool_use with stop_reason tool_use — must NOT seal
      window.handle({
        type: 'assistant',
        message: {
          role: 'assistant',
          stop_reason: 'tool_use',
          content: [{ type: 'tool_use', name: 'use_tool', input: {} }],
        },
      });
      window.handle({
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [{ type: 'tool_use', name: 'run_terminal_command', input: {} }],
        },
      });
      // Intervening status .msg (stuck-watchdog / voice) must not open a second bubble.
      if (typeof addStatusMsg === 'function') {
        addStatusMsg('synthetic mid-turn status chrome');
      }
      window.handle({
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [{ type: 'text', text: '\n**Note:** daily daemon needs the binary.\n' }],
        },
      });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
    });

    // Flush rAF stream paints
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    await page.waitForTimeout(50);

    const a = await page.evaluate(() => {
      const jevons = Array.from(document.querySelectorAll('#messages .msg.jevons'));
      return {
        count: jevons.length,
        texts: jevons.map((el) => (el._layoutText != null ? el._layoutText : (el._body && el._body.textContent) || el.textContent || '')),
        openRaw: jevons.map((el) => typeof el._streamRaw),
      };
    });

    if (a.count !== 1) {
      failures.push(`A: expected 1 jevons bubble after tools+status, got ${a.count}: ${JSON.stringify(a.texts)}`);
    } else {
      const raw = a.texts[0] || '';
      if (!raw.includes('| Bubble |') || !raw.includes('**Note:**')) {
        failures.push(`A: single bubble missing table or note: ${JSON.stringify(raw.slice(0, 200))}`);
      }
      if (a.openRaw[0] === 'string') {
        failures.push('A: stream still open after end_turn (not sealed)');
      }
    }

    // ── Fixture B: two end_turns → two bubbles (no continuity guess) ──
    await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      if (typeof clearOpenStreamHandle === 'function') clearOpenStreamHandle();

      window.handle({ type: 'user', message: { role: 'user', content: 'two ends' } });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: 'first' }] },
      });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: 'second' }] },
      });
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
    });
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    await page.waitForTimeout(50);

    const b = await page.evaluate(() => {
      const jevons = Array.from(document.querySelectorAll('#messages .msg.jevons'));
      return {
        count: jevons.length,
        texts: jevons.map((el) => (el._layoutText != null ? el._layoutText : el.textContent || '').trim()),
      };
    });
    if (b.count !== 2) {
      failures.push(`B: expected 2 bubbles for two end_turns, got ${b.count}: ${JSON.stringify(b.texts)}`);
    } else if (b.texts[0] !== 'first' || b.texts[1] !== 'second') {
      failures.push(`B: wrong bubble texts: ${JSON.stringify(b.texts)}`);
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't159-seal.png'), fullPage: true });

    if (failures.length) {
      console.error('FAIL t159-seal-test');
      for (const f of failures) console.error(' -', f);
      process.exitCode = 1;
    } else {
      console.log('ok - T159 seal: multi-chunk+tool+status → one bubble; two end_turns → two (🎯T159)');
    }
  } finally {
    await browser.close();
    srv.close();
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
