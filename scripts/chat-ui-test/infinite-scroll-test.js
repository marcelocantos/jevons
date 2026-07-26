// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic test for infinite-scroll history paging (🎯T57). Serves the
// static web/ UI plus a canned /api/history that returns SMALL windows,
// enables paging via a history_meta event, then scrolls to the top
// repeatedly and asserts:
//   * no "Load earlier" button exists (paging is automatic)
//   * a top sentinel drives the IntersectionObserver
//   * each scroll-to-top prepends an older window (message count grows)
//   * when the oldest end is reached, the sentinel is torn down
//
//   node scripts/chat-ui-test/infinite-scroll-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const WINDOW = 8; // canned server returns at most this many older lines per call

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  return 'application/octet-stream';
}

function startServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/history') {
        const end = parseInt(u.searchParams.get('end') || '0', 10);
        const start = Math.max(0, end - WINDOW);
        const lines = [];
        for (let i = start; i < end; i++) {
          lines.push({ type: 'user', message: { content: 'older message ' + i } });
        }
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ start, total: 999, lines }));
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
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 320 } });
  const consoleErrors = [];
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource|WebSocket connection/i.test(m.text())) consoleErrors.push(m.text()); });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.handle === 'function' && !!window.marked, null, { timeout: 10000 });

    // Seed a few visible messages, then enable paging: 20 older lines exist,
    // the window starts at line 20 (history_meta drives setupInfiniteScroll).
    const init = await page.evaluate(() => {
      for (let i = 0; i < 20; i++) window.addMsg('user', 'recent ' + i);
      window.handle({ type: 'history_meta', older: 20, start: 20, total: 40 });
      return {
        button: !!document.querySelector('.load-earlier'),
        sentinel: !!document.querySelector('.history-sentinel'),
        msgs: document.querySelectorAll('#messages .msg').length,
      };
    });
    if (init.button) failures.push('a "Load earlier" button exists — paging should be automatic');
    if (!init.sentinel) failures.push('no top sentinel installed for infinite scroll');

    // Scroll to the top repeatedly; each should prepend an older window.
    const counts = [init.msgs];
    for (let i = 0; i < 5; i++) {
      await page.evaluate(() => { const m = document.getElementById('messages'); m.scrollTop = m.scrollHeight; });
      await page.waitForTimeout(150);
      await page.evaluate(() => { document.getElementById('messages').scrollTop = 0; });
      await page.waitForTimeout(500);
      counts.push(await page.evaluate(() => document.querySelectorAll('#messages .msg').length));
    }
    if (counts[1] <= counts[0]) failures.push(`first scroll-to-top did not load older messages (counts ${JSON.stringify(counts)})`);
    if (counts[counts.length - 1] <= counts[1]) failures.push(`later scrolls did not keep paging (counts ${JSON.stringify(counts)})`);

    // After exhausting the 20 older lines, the sentinel is torn down.
    const end = await page.evaluate(() => ({
      sentinel: !!document.querySelector('.history-sentinel'),
      msgs: document.querySelectorAll('#messages .msg').length,
    }));
    if (end.msgs < 20 + 20) failures.push(`expected all 40 messages after full paging, got ${end.msgs}`);
    if (end.sentinel) failures.push('sentinel not torn down after reaching the oldest message');
    if (consoleErrors.length) failures.push('console errors: ' + JSON.stringify(consoleErrors.slice(0, 3)));
  } catch (e) {
    failures.push('exception: ' + e.message);
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL infinite-scroll-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - older history auto-pages on scroll-to-top (no button); sentinel torn down at the start');
})();
