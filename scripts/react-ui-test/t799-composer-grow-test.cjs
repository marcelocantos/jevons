// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T799: the composer textarea grows with a long draft up to a cap (40% of the
// viewport), then scrolls inside; it shrinks back once the draft is cleared.
// Real render against the built bundle over a mocked mux transport.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('../browser-loop-test/node_modules/playwright');

const dist = path.resolve(__dirname, '../../ui/dist');
const VIEWPORT_H = 900;
const CAP_FRACTION = 0.4;

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
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: VIEWPORT_H } });
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('https://fonts.**/*', r => r.abort());
    const agents = [{ name: 'jevons', parent: '', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' }];
    await page.route('**/api/**', async route => {
      const p = new URL(route.request().url()).pathname;
      const body = p === '/api/agents' ? agents : p === '/api/plan-usage' ? { backends: [] } : p === '/api/frontier' ? [] : {};
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });
    const rows = [];
    for (let i = 0; i < 6; i++) {
      const index = rows.length + 1;
      const type = i % 2 ? 'assistant' : 'user';
      rows.push({ id: `e:${index}`, index, op: 'put', type, event: { type, turn_origin: 'owner',
        message: { role: type, content: [{ type: 'text', text: type === 'user' ? `<user_query>request ${i}</user_query>` : `reply ${i}` }], stop_reason: type === 'assistant' ? 'end_turn' : undefined } } });
    }
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
    const input = page.locator('#input');
    const box = () => input.evaluate(el => ({ h: Math.round(el.getBoundingClientRect().height), scrolls: el.scrollHeight > el.clientHeight + 1, overflowY: getComputedStyle(el).overflowY }));
    const paragraphs = n => Array.from({ length: n }, (_, i) => `Paragraph ${i + 1}: a longer thought about how the composer should behave when the draft is long.`).join('\n\n');

    const empty = await box();
    await input.fill(paragraphs(3)); // 5 lines
    const medium = await box();
    await input.fill(paragraphs(40));
    const capped = await box();
    const cap = VIEWPORT_H * CAP_FRACTION;
    await input.fill('');
    const cleared = await box();
    console.log(`empty ${JSON.stringify(empty)}; 5 lines ${JSON.stringify(medium)}; long ${JSON.stringify(capped)} (cap ${cap}); cleared ${JSON.stringify(cleared)}`);

    assert(medium.h > empty.h + 60, `grows past two lines for a 5-line draft: ${JSON.stringify({ empty, medium })}`);
    assert(!medium.scrolls, 'no inner scroll below the cap');
    assert(capped.h <= cap + 1 && capped.h >= cap - 24, `stops at the cap (${cap}): ${JSON.stringify(capped)}`);
    assert(capped.scrolls && capped.overflowY === 'auto', `scrolls inside at the cap: ${JSON.stringify(capped)}`);
    assert.equal(cleared.h, empty.h, 'shrinks back after clearing');
    assert.deepEqual(errors, []);
    console.log('PASS: T799 composer grows to the cap, scrolls inside, shrinks back');
  } finally {
    await browser.close();
    server.close();
  }
}
main().catch(e => { console.error(e); process.exitCode = 1; });
