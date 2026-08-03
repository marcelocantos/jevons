// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic test for progressive full-history hydrate (🎯T119) using the
// 🎯T57 /api/history API. Serves the static web/ UI plus a canned
// /api/history that returns SMALL windows, enables hydrate via a
// history_meta event, then asserts:
//   * no "Load earlier" button exists (paging is automatic)
//   * remaining history loads without requiring the owner to scroll
//     for *data* (progressive background fetch)
//   * message count grows to cover the full older range
//   * when the oldest end is reached, the optional sentinel is torn down
//   * jump-to-bottom FAB exists; no jump-to-top control
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

    // Seed recent messages, then enable progressive hydrate: 20 older lines
    // exist, recent window starts at line 20.
    const init = await page.evaluate(() => {
      for (let i = 0; i < 20; i++) window.addMsg('user', 'recent ' + i);
      window.handle({ type: 'history_meta', older: 20, start: 20, total: 40 });
      return {
        button: !!document.querySelector('.load-earlier'),
        jumpTop: !!document.querySelector('#jump-top, .jump-top'),
        jumpBottom: !!document.getElementById('jump-bottom'),
        msgs: document.querySelectorAll('#messages .msg').length,
      };
    });
    if (init.button) failures.push('a "Load earlier" button exists — paging should be automatic');
    if (init.jumpTop) failures.push('jump-to-top control must not exist (🎯T119)');
    if (!init.jumpBottom) failures.push('jump-to-bottom FAB missing');

    // Progressive load must grow the transcript WITHOUT scroll-to-top for data.
    // Wait until all 40 messages are resident (20 recent + 20 older).
    let finalCount = init.msgs;
    for (let i = 0; i < 40; i++) {
      await page.waitForTimeout(100);
      finalCount = await page.evaluate(() => document.querySelectorAll('#messages .msg').length);
      if (finalCount >= 40) break;
    }
    if (finalCount < 40) {
      failures.push(`progressive load did not fetch full history without scroll (got ${finalCount}, want ≥40)`);
    }

    const end = await page.evaluate(() => ({
      sentinel: !!document.querySelector('.history-sentinel'),
      msgs: document.querySelectorAll('#messages .msg').length,
      shells: document.querySelectorAll('#messages .msg.virt-shell').length,
    }));
    if (end.msgs < 40) failures.push(`expected all 40 messages after progressive hydrate, got ${end.msgs}`);
    if (end.sentinel) failures.push('sentinel not torn down after reaching the oldest message');
    // Windowed content: with 40 msgs in a short viewport, some should be shells.
    // (Not a hard fail if estimates keep them all "visible" with buffer — soft.)
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
  console.log('ok - progressive full history hydrate without scroll-for-data; no jump-to-top (🎯T119)');
})();
