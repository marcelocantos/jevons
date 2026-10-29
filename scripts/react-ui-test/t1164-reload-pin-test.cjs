// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1164: after a hard reload the main chat must pin to the live end, not
// leave #messages at scrollTop=0 with ↓ Latest visible on a tall transcript.
// Hermetic isolate — mocked mux on a throwaway static server, never :13705.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('./playwright.cjs')();

const dist = path.resolve(__dirname, '../../ui/dist');
let server;
let browser;

function buildHistory() {
  const rows = [];
  const add = (type, text, origin) => {
    const index = rows.length + 1;
    rows.push({
      id: `e:${index}`,
      index,
      op: 'put',
      type,
      event: {
        type,
        turn_origin: origin,
        message: {
          role: type,
          content: [{ type: 'text', text }],
          stop_reason: type === 'assistant' ? 'end_turn' : undefined,
        },
      },
    });
  };
  for (let i = 0; i < 80; i++) {
    add('user', `Earlier owner request ${i} for reload-pin oracle`, 'owner');
    add(
      'assistant',
      `Reply ${i}: a long enough assistant turn that the virtualized canvas is many viewports tall after hard reload, so scrollTop=0 with ↓ Latest is unmistakably wrong.`,
      undefined,
    );
  }
  return rows;
}

async function main() {
  await fs.access(path.join(dist, 'index.html'));
  server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://127.0.0.1');
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader(
        'Content-Type',
        { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)] ||
          'application/octet-stream',
      );
      res.end(data);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = new URL(`http://127.0.0.1:${server.address().port}`);
  const rows = buildHistory();
  const histories = new Map([['transcript:jevons', rows]]);
  const agents = [{ name: 'jevons', parent: '', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' }];

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.setDefaultTimeout(15000);
  await page.route('https://fonts.**/*', route => route.abort());
  await page.route('**/api/**', async route => {
    const pathname = new URL(route.request().url()).pathname;
    const body =
      pathname === '/api/agents'
        ? agents
        : pathname === '/api/plan-usage'
          ? { backends: [] }
          : pathname === '/api/frontier'
            ? []
            : {};
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
  });
  await page.routeWebSocket('**/ws/mux', socket => {
    const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
    const replay = ch => {
      const hist = histories.get(ch) || [];
      for (const row of hist) send(ch, 'frame', row);
      send(ch, 'meta', {
        lo: 1,
        hi: 0,
        n: hist.length,
        start: 1,
        older: 0,
        total: hist.length,
        following: true,
        working: false,
        phase: { phase: 'idle' },
        overseer_down: '',
      });
    };
    socket.onMessage(payload => {
      const frame = JSON.parse(String(payload));
      if (frame.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
      if (frame.t === 'open' || frame.t === 'window') return replay(frame.ch);
    });
  });

  await page.goto(base.href);
  await page.locator('#messages').waitFor({ state: 'visible' });
  await page.waitForFunction(() => {
    const el = document.querySelector('#messages');
    if (!el) return false;
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
    return el.scrollHeight > el.clientHeight * 3 && gap <= 16;
  });

  const beforeReload = await page.locator('#messages').evaluate(el => ({
    scrollHeight: el.scrollHeight,
    fromBottom: el.scrollHeight - el.scrollTop - el.clientHeight,
  }));
  assert(beforeReload.scrollHeight > 0, 'fixture transcript should load');
  assert(beforeReload.fromBottom <= 16, `expected pinned before reload, fromBottom=${beforeReload.fromBottom}`);

  await page.reload({ waitUntil: 'domcontentloaded' });
  await page.locator('#messages').waitFor({ state: 'visible' });

  const pinnedAfterReload = await page
    .waitForFunction(
      () => {
        const el = document.querySelector('#messages');
        if (!el || el.scrollHeight <= el.clientHeight * 3) return false;
        const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
        return gap <= 16;
      },
      { timeout: 5000 },
    )
    .catch(() => null);
  assert(pinnedAfterReload, 'hard reload never pinned to the live end on a tall transcript (T1164)');

  const afterReload = await page.locator('#messages').evaluate(el => ({
    scrollTop: el.scrollTop,
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
    fromBottom: el.scrollHeight - el.scrollTop - el.clientHeight,
    latestHidden: document.querySelector('#jump-bottom')?.hasAttribute('hidden') ?? true,
  }));

  assert(afterReload.scrollHeight > afterReload.clientHeight * 3, 'tall transcript after reload');
  assert(afterReload.scrollTop > 0, `hard reload left scrollTop=0 on a ${afterReload.scrollHeight}px canvas (T1164)`);
  assert(afterReload.fromBottom <= 16, `hard reload not pinned to live end: fromBottom=${afterReload.fromBottom}`);
  assert(afterReload.latestHidden, '↓ Latest must stay hidden while tracking after hard reload');

  assert.deepEqual(errors, [], 'React has no uncaught browser errors');
  console.log('PASS: T1164 hard-reload pin — scrollTop at live end, ↓ Latest hidden');
}

main()
  .catch(error => {
    console.error(error);
    process.exitCode = 1;
  })
  .finally(async () => {
    await browser?.close();
    if (server) await new Promise(resolve => server.close(resolve));
  });
