// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1224: packaged React cockpit + mocked mux (never :13705).
// 1) unreadable → cached healthy mux frame (no refresh=1)
// 2) tab visible forces bounded refresh=1 recovery
// Blackholed OPEN socket recovery is in ui/src/mux/client.test.ts (pong watchdog).
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('./playwright.cjs')();

const dist = path.resolve(__dirname, '../../ui/dist');
const PLAN_CH = 'plan-usage';

const goodMux = {
  backends: [
    { provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 40, used_percent: 60 }] },
    { provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 8, used_percent: 92 }] },
  ],
};

const badMux = {
  backends: [
    { provider: 'claude', status: 'unavailable', reason: 'meter 429', windows: [] },
    { provider: 'grok', status: 'unavailable', reason: 'meter 429', windows: [] },
  ],
};

const forcedAfterTab = {
  backends: [
    { provider: 'claude', status: 'available', windows: [{ name: 'weekly', remaining_percent: 33, used_percent: 67 }] },
    { provider: 'grok', status: 'available', windows: [{ name: 'weekly', remaining_percent: 6, used_percent: 94 }] },
  ],
};

async function waitFor(cond, label, ms = 5000) {
  const deadline = Date.now() + ms;
  while (Date.now() < deadline) {
    if (cond()) return;
    await new Promise((r) => setTimeout(r, 50));
  }
  throw new Error(`Timed out: ${label}`);
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

  let forcedBody = forcedAfterTab;
  let refreshCalls = 0;
  let pushPlanFrame;

  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    page.setDefaultTimeout(15000);
    await page.route('https://fonts.**/*', (r) => r.abort());
    const agents = [
      { name: 'jevons', parent: '', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' },
    ];
    await page.route('**/api/**', async (route) => {
      const url = new URL(route.request().url());
      const p = url.pathname;
      if (p === '/api/plan-usage') {
        if (url.searchParams.get('refresh') === '1') {
          refreshCalls++;
          await route.fulfill({ contentType: 'application/json', body: JSON.stringify(forcedBody) });
          return;
        }
        await route.fulfill({ contentType: 'application/json', body: JSON.stringify(goodMux) });
        return;
      }
      const body =
        p === '/api/agents'
          ? agents
          : p === '/api/frontier'
            ? []
            : p === '/api/plan-usage/decisions'
              ? { decisions: [] }
              : p === '/api/plan-usage/auth'
                ? { plans: [] }
                : {};
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });

    await page.routeWebSocket('**/ws/mux', (socket) => {
      const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
      pushPlanFrame = (body) => send(PLAN_CH, 'frame', body);
      socket.onMessage((payload) => {
        const f = JSON.parse(String(payload));
        if (f.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
        if (f.ch === PLAN_CH && f.t === 'open') send(PLAN_CH, 'frame', badMux);
      });
    });

    await page.goto(base);
    await page.locator('#plan-ticker').waitFor({ state: 'visible' });
    await page.waitForFunction(() => document.querySelector('#plan-ticker')?.textContent?.includes('usage unavailable'));
    assert.equal(refreshCalls, 0, 'cached mux recovery must not require refresh=1 on load');

    pushPlanFrame(goodMux);
    await page.waitForSelector('[data-provider="claude"] .plan-bar-fill');
    assert.equal(refreshCalls, 0, 'healthy cached mux frame must repaint without forced collection');

    pushPlanFrame(badMux);
    await page.waitForFunction(() => document.querySelector('[data-provider="claude"] .plan-nodata'));

    const beforeTab = refreshCalls;
    forcedBody = forcedAfterTab;
    await page.evaluate(() => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    await waitFor(() => refreshCalls > beforeTab, 'visibility refresh=1');
    await page.waitForFunction(() => {
      const el = document.querySelector('[data-provider="claude"] .plan-bar-fill');
      return el && el.style.width === '67%';
    });

    console.log(
      `PASS T1224 plan-usage tab recovery (refresh=${refreshCalls}, isolate port ${server.address().port})`,
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
