// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic 🎯T56: long transcript → off-screen bodies dematerialised.
//   node scripts/chat-ui-test/virtual-list-test.js

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

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
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 900, height: 400 } });
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.addMsg === 'function' && typeof window.virtualizeMessages === 'function', null, { timeout: 10000 });

    const stats = await page.evaluate(() => {
      const N = 80;
      for (let i = 0; i < N; i++) {
        window.addMsg('user', 'message number ' + i + ' with enough text to have height ' + 'x'.repeat(40));
        window.addMsg('jevons', 'reply ' + i + '\n\n' + 'paragraph '.repeat(20));
      }
      // Pin top so lower half is off-screen.
      document.getElementById('messages').scrollTop = 0;
      window.virtualizeMessages();
      const msgs = [...document.querySelectorAll('#messages > .msg')];
      const shells = msgs.filter(m => m.classList.contains('virt-shell'));
      const heavy = msgs.filter(m => !m.classList.contains('virt-shell'));
      // Bodies of shells should be empty.
      const emptyBodies = shells.filter(m => m._body && m._body.innerHTML === '').length;
      return {
        total: msgs.length,
        shells: shells.length,
        heavy: heavy.length,
        emptyBodies,
        hasVirtualList: typeof window.VirtualList !== 'undefined',
      };
    });

    if (!stats.hasVirtualList) failures.push('VirtualList not loaded');
    if (stats.total < 100) failures.push('expected many msgs, got ' + stats.total);
    if (stats.shells < 20) failures.push('expected many virt-shells, got ' + stats.shells);
    if (stats.heavy > 40) failures.push('too many heavy nodes: ' + stats.heavy);
    if (stats.emptyBodies !== stats.shells) failures.push('shells without empty bodies');

    // Scroll to bottom: shells near top should remain dematerialised; bottom rematerialise.
    const after = await page.evaluate(() => {
      const el = document.getElementById('messages');
      el.scrollTop = el.scrollHeight;
      window.virtualizeMessages();
      const msgs = [...document.querySelectorAll('#messages > .msg')];
      const last = msgs[msgs.length - 1];
      return {
        lastShell: last && last.classList.contains('virt-shell'),
        shells: msgs.filter(m => m.classList.contains('virt-shell')).length,
      };
    });
    if (after.lastShell) failures.push('latest msg should be materialised at bottom');
    if (after.shells < 10) failures.push('still expect off-screen shells at bottom, got ' + after.shells);

    console.log(JSON.stringify({ stats, after, failures }, null, 2));
    if (failures.length) {
      console.error('FAIL', failures);
      process.exitCode = 1;
    } else {
      console.log('PASS virtual-list-test');
    }
  } finally {
    await browser.close();
    srv.close();
  }
})().catch(e => { console.error(e); process.exit(1); });
