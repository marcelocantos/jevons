// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T949: loading a transcript whose rows measure differently from the
// virtualizer's estimate must not raise "ResizeObserver loop completed with
// undelivered notifications". The owner cockpit journaled that message three
// times on 2026-09-29 (window.onerror, ?agent=jevons-po&tab=frontier) and the
// RSI coach clustered it as a lifecycle_error.
//
// Mechanism: @tanstack/virtual-core measures rows inside its ResizeObserver
// callback. A row above the fold that measures off-estimate adjusts scrollTop
// synchronously, and react-virtual answers with flushSync(rerender) — so React
// rewrites the canvas height (a shallower observed node) while the browser is
// still delivering observations. That resize cannot be delivered in the same
// frame, and the browser reports it as a window error event. Playwright's
// pageerror never sees it (it is not an exception), so this suite listens for
// the ErrorEvent itself and for the cockpit's own /api/log report.
//
// Real render against the built bundle over a mocked mux transport.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('../browser-loop-test/node_modules/playwright');

const dist = path.resolve(__dirname, '../../ui/dist');
const ROWS = 90;
const LOADS = 3;
const SETTLE_MS = 2500;

function transcript() {
  const rows = [];
  for (let i = 0; i < ROWS; i++) {
    const index = rows.length + 1;
    const type = i % 2 ? 'assistant' : 'user';
    // Heights vary well away from the 72px estimate in both directions, the
    // way a real overseer transcript does.
    const paras = type === 'user' ? 1 : 1 + (i % 7) * 2;
    const body = Array.from({ length: paras }, (_, p) => `Paragraph ${p + 1} of reply ${i}: the transcript row must measure its real height without looping the observer.`).join('\n\n');
    const text = type === 'user' ? `<user_query>request ${i}</user_query>` : body;
    rows.push({ id: `e:${index}`, index, op: 'put', type, event: { type, turn_origin: 'owner',
      message: { role: type, content: [{ type: 'text', text }], stop_reason: type === 'assistant' ? 'end_turn' : undefined } } });
  }
  return rows;
}

async function main() {
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://127.0.0.1');
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)] || 'application/octet-stream');
      res.end(data);
    } catch { res.writeHead(404).end(); }
  });
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({ headless: true });
  const rows = transcript();
  const windowErrors = [];
  const journaled = [];
  try {
    for (let load = 0; load < LOADS; load++) {
      const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
      const pageErrors = [];
      page.on('pageerror', e => pageErrors.push(e.message));
      await page.addInitScript(() => {
        window.__t949 = [];
        window.addEventListener('error', e => window.__t949.push(e.message));
      });
      await page.route('https://fonts.**/*', r => r.abort());
      const agents = [{ name: 'jevons', parent: '', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' }];
      await page.route('**/api/**', async route => {
        const p = new URL(route.request().url()).pathname;
        if (p === '/api/log') {
          journaled.push(route.request().postData());
          return route.fulfill({ status: 204, body: '' });
        }
        const body = p === '/api/agents' ? agents : p === '/api/plan-usage' ? { backends: [] } : p === '/api/frontier' ? [] : {};
        await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
      });
      await page.routeWebSocket('**/ws/mux', socket => {
        const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
        const replay = ch => {
          for (const row of rows) send(ch, 'frame', row);
          send(ch, 'meta', { lo: 1, hi: 0, n: rows.length, start: 1, older: 0, total: rows.length, following: true, working: false, phase: { phase: 'idle' }, overseer_down: '' });
        };
        socket.onMessage(payload => {
          const f = JSON.parse(String(payload));
          if (f.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
          if (f.t === 'open' || f.t === 'window') replay(f.ch);
        });
      });
      await page.goto(base);
      await page.locator('#messages [data-kind="assistant"]').last().waitFor();
      await page.waitForTimeout(SETTLE_MS);
      // Scroll back through the transcript so rows mount and measure above
      // the fold, then return to the live end.
      const scroller = page.locator('#messages');
      for (let step = 0; step < 6; step++) {
        await scroller.evaluate(el => { el.scrollTop = Math.max(0, el.scrollTop - el.clientHeight * 1.5); });
        await page.waitForTimeout(150);
      }
      await scroller.evaluate(el => { el.scrollTop = el.scrollHeight; });
      await page.waitForTimeout(500);
      const seen = await page.evaluate(() => window.__t949);
      windowErrors.push(...seen);
      assert.deepEqual(pageErrors, [], `load ${load}: no page exceptions`);
      await page.close();
    }
    console.log(`window error events: ${JSON.stringify(windowErrors)}; journaled: ${journaled.length}`);
    const loops = windowErrors.filter(m => /ResizeObserver loop/.test(m));
    assert.deepEqual(loops, [], `ResizeObserver loop errors over ${LOADS} loads of a ${ROWS}-row transcript`);
    assert.deepEqual(journaled, [], 'the cockpit journaled no window.onerror');
    assert.deepEqual(windowErrors, [], 'no window error events at all');
    console.log(`PASS: T949 ${LOADS} loads of a ${ROWS}-row transcript raised no ResizeObserver loop error and journaled nothing`);
  } finally {
    await browser.close();
    server.close();
  }
}
main().catch(e => { console.error(e); process.exitCode = 1; });
