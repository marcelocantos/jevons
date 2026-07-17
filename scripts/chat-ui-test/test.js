// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright perceptual tests for the Jevons chat UI (🎯T39).
//
// Two modes:
//   hermetic (default) — static web/ + mocked WebSocket; no jevonsd/Grok.
//     Replays the exact token stream from the screenshot regression.
//   live — real jevonsd at --host; costs tokens, needs overseer up.
//
//   node scripts/chat-ui-test/test.js
//   node scripts/chat-ui-test/test.js --live
//   node scripts/chat-ui-test/test.js --headed
//
// Exit 0 only when:
//   * "Jevons is working" appears after send and is gone after the turn
//   * multi-token assistant reply is ONE .msg.jevons bubble (not N)
//   * bubble text matches the full coalesced sentence

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const { pathToFileURL } = require('url');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const argv = process.argv.slice(2);
const opt = (name, def) => {
  const i = argv.indexOf(`--${name}`);
  if (i === -1) return def;
  const next = argv[i + 1];
  return next && !next.startsWith('--') ? next : true;
};
const LIVE = !!opt('live', false);
const HEADED = !!opt('headed', false);
const HOST = opt('host', '127.0.0.1:13705');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

// Screenshot regression tokens (user image: Hello / . / What / …).
const TOKENS = ['Hello', '.', ' What', ' do', ' you', ' need', '?'];
const FULL_REPLY = TOKENS.join('');
const PROMPT = LIVE
  ? 'Reply with exactly this sentence and nothing else: Hello. What do you need?'
  : 'hello';

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 'chat-ui-test', ok: true }));
        return;
      }
      let rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) {
        res.writeHead(403); res.end(); return;
      }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      resolve({ srv, base: `http://127.0.0.1:${port}` });
    });
    srv.on('error', reject);
  });
}

// Mock WebSocket that delivers a Grok-shaped multi-token turn on send.
// Passed to page.addInitScript(fn, arg).
function installMockWebSocket({ tokens }) {
  class MockWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    constructor(url) {
      this.url = url;
      this.readyState = MockWebSocket.CONNECTING;
      this.onopen = null;
      this.onclose = null;
      this.onerror = null;
      this.onmessage = null;
      queueMicrotask(() => {
        this.readyState = MockWebSocket.OPEN;
        if (this.onopen) this.onopen({});
      });
      window.__mockSockets = window.__mockSockets || [];
      window.__mockSockets.push(this);
    }
    send(data) {
      window.__lastChatSend = data;
      if (typeof data !== 'string') return;
      if (data === '{"type":"ping"}') {
        this._emit({ type: 'pong' });
        return;
      }
      if (data.startsWith('{')) return; // control frames
      this._emit({
        type: 'user',
        message: { role: 'user', content: data },
      });
      // Stream tokens with delays so appendOrAddJevons runs per chunk.
      let i = 0;
      const step = () => {
        if (i < tokens.length) {
          const text = tokens[i++];
          this._emit({
            type: 'assistant',
            message: {
              role: 'assistant',
              content: [{ type: 'text', text }],
            },
          });
          setTimeout(step, 15);
          return;
        }
        this._emit({
          type: 'assistant',
          message: {
            role: 'assistant',
            content: [],
            stop_reason: 'end_turn',
          },
        });
      };
      setTimeout(step, 20);
    }
    close() {
      this.readyState = MockWebSocket.CLOSED;
      if (this.onclose) this.onclose({});
    }
    _emit(obj) {
      const payload = typeof obj === 'string' ? obj : JSON.stringify(obj);
      if (this.onmessage) this.onmessage({ data: payload });
    }
  }
  window.WebSocket = MockWebSocket;
}

async function assertChatPercept(page, { expectFull, label }) {
  const failures = [];

  // Wait for connected.
  await page.waitForFunction(
    () => document.getElementById('status-text')?.textContent === 'connected',
    null,
    { timeout: 15000 },
  );

  const beforeJevons = await page.locator('#messages .msg.jevons').count();
  const beforeUser = await page.locator('#messages .msg.user').count();

  await page.locator('#input').fill(PROMPT);
  await page.locator('#send').click();

  // Working indicator must appear (perceptual: user sees "working").
  await page.waitForSelector('.working-indicator', { timeout: 5000 });
  const workingShot = path.join(OUT_DIR, `${label}-working.png`);
  await page.screenshot({ path: workingShot, fullPage: true });

  // Wait for turn to finish: working gone + at least one new jevons bubble.
  await page.waitForFunction(
    (prev) => {
      const working = document.querySelector('.working-indicator');
      const n = document.querySelectorAll('#messages .msg.jevons').length;
      return !working && n > prev;
    },
    beforeJevons,
    { timeout: 90000 },
  );

  const afterJevons = await page.locator('#messages .msg.jevons').count();
  const afterUser = await page.locator('#messages .msg.user').count();
  const newJevons = afterJevons - beforeJevons;
  const newUser = afterUser - beforeUser;

  if (newUser < 1) failures.push(`expected ≥1 new user bubble, got ${newUser}`);
  if (newJevons !== 1) {
    failures.push(
      `expected exactly 1 new jevons bubble (coalesced stream), got ${newJevons} ` +
      `(total ${afterJevons}, was ${beforeJevons}) — token-per-bubble regression`,
    );
  }

  const lastJevons = page.locator('#messages .msg.jevons').last();
  const text = (await lastJevons.innerText()).replace(/\s*now\s*$/i, '').trim();
  // innerText includes timestamp "now"; strip trailing relative time lines.
  const cleaned = text.split('\n').filter(l => l.trim() !== 'now').join('\n').trim();

  if (expectFull) {
    // Normalize whitespace for comparison.
    const got = cleaned.replace(/\s+/g, ' ').trim();
    const want = expectFull.replace(/\s+/g, ' ').trim();
    if (got !== want && !got.includes(want) && !want.includes(got)) {
      failures.push(`jevons bubble text mismatch:\n  got:  ${JSON.stringify(got)}\n  want: ${JSON.stringify(want)}`);
    }
    // Hard fail if the bubble is a single token from the stream (classic bug).
    if (cleaned === 'Hello' || cleaned === '.' || cleaned === 'What' || cleaned === '?') {
      failures.push(`bubble is a single token ${JSON.stringify(cleaned)} — stream not coalesced`);
    }
  } else if (cleaned.length < 2) {
    failures.push(`jevons bubble too short: ${JSON.stringify(cleaned)}`);
  }

  const workingLeft = await page.locator('.working-indicator').count();
  if (workingLeft !== 0) failures.push(`working indicator still present (${workingLeft})`);

  const doneShot = path.join(OUT_DIR, `${label}-done.png`);
  await page.screenshot({ path: doneShot, fullPage: true });

  // Accessibility snapshot counts (agent-level perception).
  const snapshot = {
    label,
    newUser,
    newJevons,
    bubbleText: cleaned,
    workingLeft,
    screenshots: [workingShot, doneShot],
  };
  fs.writeFileSync(path.join(OUT_DIR, `${label}-result.json`), JSON.stringify(snapshot, null, 2));

  return { failures, snapshot };
}

async function runHermetic() {
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  try {
    const page = await browser.newPage({ viewport: { width: 900, height: 800 } });
    await page.addInitScript(installMockWebSocket, { tokens: TOKENS });
    await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
    const { failures, snapshot } = await assertChatPercept(page, {
      expectFull: FULL_REPLY,
      label: 'hermetic',
    });
    if (failures.length) {
      console.error('HERMETIC FAIL:\n - ' + failures.join('\n - '));
      console.error(JSON.stringify(snapshot, null, 2));
      process.exitCode = 1;
    } else {
      console.log('HERMETIC PASS', snapshot);
    }
  } finally {
    await browser.close();
    srv.close();
  }
}

async function runLive() {
  const browser = await chromium.launch({ headless: !HEADED });
  try {
    const page = await browser.newPage({ viewport: { width: 900, height: 800 } });
    await page.goto(`http://${HOST}/`, { waitUntil: 'domcontentloaded' });
    const { failures, snapshot } = await assertChatPercept(page, {
      expectFull: 'Hello. What do you need?',
      label: 'live',
    });
    if (failures.length) {
      console.error('LIVE FAIL:\n - ' + failures.join('\n - '));
      console.error(JSON.stringify(snapshot, null, 2));
      process.exitCode = 1;
    } else {
      console.log('LIVE PASS', snapshot);
    }
  } finally {
    await browser.close();
  }
}

(async () => {
  console.log(LIVE ? `mode=live host=${HOST}` : 'mode=hermetic (mocked WS)');
  if (LIVE) await runLive();
  else await runHermetic();
})().catch(err => {
  console.error(err);
  process.exit(1);
});
