// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic test: WS-style chronological history replay must not scroll-chase
// through messages (🎯T119.1). After beginHistoryReplay + N messages +
// history_meta / endHistoryReplayAndPin, intermediate scrollTops must not
// progressively climb; final dist-to-bottom ≈ 0.
//
//   node scripts/chat-ui-test/replay-scroll-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');

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
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ start: 0, total: 0, lines: [] }));
        return;
      }
      if (u.pathname === '/api/log') {
        res.writeHead(204);
        res.end();
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
  const page = await browser.newPage({ viewport: { width: 1000, height: 400 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && typeof window.beginHistoryReplay === 'function' && !!window.marked,
      null,
      { timeout: 10000 },
    );

    const result = await page.evaluate(async () => {
      const m = document.getElementById('messages');
      const samples = [];
      // Simulate wipe+replay as transport.onOpen does.
      window.beginHistoryReplay();
      for (let i = 0; i < 25; i++) {
        if (typeof window.addMsg === 'function') {
          window.addMsg('user', 'replay user line ' + i + ' — padding so bubbles have height.');
        } else if (typeof window.handle === 'function') {
          window.handle({
            type: 'user',
            message: { content: 'replay user line ' + i + ' — padding so bubbles have height.' },
          });
        }
        // Sample mid-burst scrollTop (should not climb toward bottom).
        if (i % 5 === 4) {
          samples.push({
            i,
            scrollTop: m.scrollTop,
            dist: m.scrollHeight - m.clientHeight - m.scrollTop,
            replay: !!window.historyReplayActive,
          });
        }
        // Yield a frame so rAF scroll would fire if not suppressed.
        await new Promise((r) => requestAnimationFrame(r));
      }
      // End of recent window (server history_meta).
      window.handle({ type: 'history_meta', older: 0, start: 0, total: 25 });
      await new Promise((r) => requestAnimationFrame(r));
      await new Promise((r) => requestAnimationFrame(r));
      const dist = m.scrollHeight - m.clientHeight - m.scrollTop;
      return {
        samples,
        finalScrollTop: m.scrollTop,
        finalDist: dist,
        finalReplay: !!window.historyReplayActive,
        msgCount: m.querySelectorAll('.msg').length,
        maxMidScrollTop: samples.reduce((a, s) => Math.max(a, s.scrollTop), 0),
      };
    });

    if (result.msgCount < 10) {
      failures.push('expected many messages after replay, got ' + result.msgCount);
    }
    // During burst, stick-to-bottom must not chase: mid samples stay near top.
    if (result.maxMidScrollTop > 80) {
      failures.push(
        'scroll parade during replay: max mid scrollTop=' + result.maxMidScrollTop +
        ' samples=' + JSON.stringify(result.samples),
      );
    }
    // After history_meta: one pin to bottom.
    if (result.finalReplay) {
      failures.push('historyReplayActive still true after history_meta');
    }
    if (result.finalDist > 24) {
      failures.push('after pin, dist-to-bottom=' + result.finalDist + ' want ≈0');
    }
  } catch (e) {
    failures.push('exception: ' + e.message);
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL replay-scroll-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - history replay has no scroll parade; single pin at end (🎯T119.1)');
})();
