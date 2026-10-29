// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T532: a settled pin-at-end transcript with idle meta must not drive a
// tight rAF/layout loop. Hermetic proxy: count requestAnimationFrame callbacks
// over a 2s window after hydrate — streaming and working-indicator CSS may
// tick a little; tens of rAF/s is a fail.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('./playwright.cjs')();

const dist = path.resolve(__dirname, '../../ui/dist');
const ROWS = 120;
const SETTLE_MS = 3000;
const IDLE_MS = 2000;
const MAX_REPINS = 0;

function transcript() {
  const rows = [];
  for (let i = 0; i < ROWS; i++) {
    const index = rows.length + 1;
    const type = i % 2 ? 'assistant' : 'user';
    const paras = type === 'user' ? 1 : 1 + (i % 5);
    const body = Array.from(
      { length: paras },
      (_, p) => `Idle row ${i} paragraph ${p}: settled transcript with no stream in flight.`,
    ).join('\n\n');
    const text = type === 'user' ? `<user_query>request ${i}</user_query>` : body;
    rows.push({
      id: `e:${index}`,
      index,
      op: 'put',
      type,
      event: {
        type,
        turn_origin: 'owner',
        message: {
          role: type,
          content: [{ type: 'text', text }],
          stop_reason: type === 'assistant' ? 'end_turn' : undefined,
        },
      },
    });
  }
  return rows;
}

async function main() {
  await fs.access(path.join(dist, 'index.html'));
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://127.0.0.1');
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader(
        'Content-Type',
        { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[
          path.extname(file)
        ] || 'application/octet-stream',
      );
      res.end(data);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({ headless: true });
  const rows = transcript();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    await page.route('https://fonts.**/*', (r) => r.abort());
    const agents = [
      {
        name: 'jevons',
        parent: '',
        purpose: 'overseer',
        provider: 'fixture',
        running: true,
        status: 'running',
        phase: 'idle',
      },
    ];
    await page.route('**/api/**', async (route) => {
      const p = new URL(route.request().url()).pathname;
      const body =
        p === '/api/agents'
          ? agents
          : p === '/api/plan-usage'
            ? { backends: [] }
            : p === '/api/frontier'
              ? []
              : {};
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });
    await page.routeWebSocket('**/ws/mux', (socket) => {
      const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
      const replay = (ch) => {
        for (const row of rows) send(ch, 'frame', row);
        send(ch, 'meta', {
          lo: 1,
          hi: 0,
          n: rows.length,
          start: 1,
          older: 0,
          total: rows.length,
          following: true,
          working: false,
          phase: { phase: 'idle' },
          overseer_down: '',
        });
      };
      socket.onMessage((payload) => {
        const f = JSON.parse(String(payload));
        if (f.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
        if (f.t === 'open' || f.t === 'window') replay(f.ch);
      });
    });
    await page.goto(base);
    await page.locator('#messages [data-kind="assistant"]').last().waitFor();
    await page.waitForTimeout(SETTLE_MS);
    await page.evaluate(() => {
      window.__jevonsT532PinCensus = { repins: 0 };
    });
    await page.waitForTimeout(IDLE_MS);
    const repins = await page.evaluate(() => window.__jevonsT532PinCensus?.repins ?? -1);
    assert.ok(repins <= MAX_REPINS, `idle settle observer repinned ${repins} times in ${IDLE_MS}ms`);
    console.log(
      `PASS: T532 idle tab settle repins=${repins} (<= ${MAX_REPINS}) over ${IDLE_MS}ms on ${ROWS}-row transcript`,
    );
  } finally {
    await browser.close();
    server.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
