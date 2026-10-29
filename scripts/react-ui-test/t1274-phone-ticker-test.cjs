// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1274: measured React header at phone width, not a CSS source grep.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { chromium } = require('../browser-loop-test/node_modules/playwright');
const dist = path.resolve(__dirname, '../../ui/dist');
const now = Date.now();
const windows = (names, remaining) => names.map(name => ({
  name, remaining_percent: remaining, used_percent: 100 - remaining,
  resets_at: new Date(now + 3600_000).toISOString(), limit_window_seconds: 7 * 86400,
}));
const snapshot = { backends: [
  { provider: 'claude', status: 'available', windows: windows(['session', 'weekly'], 45) },
  { provider: 'codex', status: 'available', windows: windows(['weekly'], 71) },
  { provider: 'grok', status: 'available', windows: windows(['weekly'], 32) },
  { provider: 'cursor', status: 'available', windows: windows(['weekly'], 0) },
] };

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
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({ headless: true });
  try {
    for (const width of [390, 820, 1280]) {
      const page = await browser.newPage({ viewport: { width, height: 844 } });
      await page.route('https://fonts.**/*', route => route.abort());
      await page.route('**/api/**', route => route.fulfill({ contentType: 'application/json', body: JSON.stringify(
        new URL(route.request().url()).pathname === '/api/plan-usage' ? snapshot : []
      ) }));
      await page.routeWebSocket('**/ws/mux', socket => {
        socket.onMessage(payload => {
          const frame = JSON.parse(String(payload));
          if (frame.type === 'ping') socket.send(JSON.stringify({ type: 'pong' }));
          if (frame.ch === 'plan-usage' && frame.t === 'open')
            socket.send(JSON.stringify({ v: 1, ch: 'plan-usage', t: 'frame', body: snapshot }));
          if (frame.ch?.startsWith('transcript:') && (frame.t === 'open' || frame.t === 'window')) {
            const rows = Array.from({ length: 16 }, (_, i) => {
              const kind = i % 2 ? 'assistant' : 'user';
              return { id: `e:${i+1}`, index: i+1, op: 'put', type: kind, event: {
                type: kind, turn_origin: kind === 'user' ? 'owner' : undefined,
                message: { role: kind, content: [{ type: 'text', text: kind === 'user'
                  ? `<user_query>Review plan availability and the next step for request ${i / 2 + 1}.</user_query>`
                  : `The plan readings are visible in the header. We can review the next step with the available providers. This reply is part of the conversation.` }], stop_reason: 'end_turn' },
              } };
            });
            for (const row of rows) socket.send(JSON.stringify({ v: 1, ch: frame.ch, t: 'frame', body: row }));
            socket.send(JSON.stringify({ v: 1, ch: frame.ch, t: 'meta', body: {
              lo: 1, hi: 0, n: rows.length, start: 1, older: 0, total: rows.length,
              following: true, working: false, phase: { phase: 'idle' }, overseer_down: '',
            } }));
          }
        });
      });
      await page.goto(`http://127.0.0.1:${server.address().port}/`);
      // The mux may not connect in an isolate. Inject its usage frame through
      // the real React subscription by fulfilling the query when it falls back.
      await page.locator('#plan-ticker .plan-group').first().waitFor({ timeout: 15000 });
      await page.waitForFunction(() => document.querySelectorAll('#plan-ticker .plan-group').length === 4);
      const geometry = await page.evaluate(() => {
        const rect = el => { const r = el.getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom }; };
        return {
          groups: [...document.querySelectorAll('#plan-ticker .plan-group')].map(g => ({ provider: g.dataset.provider, ...rect(g) })),
          buttons: ['#settings-button', '#theme-toggle'].map(s => rect(document.querySelector(s))),
          scrollWidth: document.documentElement.scrollWidth,
          ticker: rect(document.querySelector('#plan-ticker')),
        };
      });
      assert.deepEqual(geometry.groups.map(g => g.provider), ['claude', 'codex', 'grok', 'cursor']);
      if (width === 390) {
        assert(geometry.scrollWidth <= width, `horizontal page scroll: ${JSON.stringify(geometry)}`);
        for (const group of geometry.groups) {
          assert(group.left >= 0 && group.right <= width, `${group.provider} outside viewport: ${JSON.stringify(geometry)}`);
          for (const button of geometry.buttons) {
            assert(group.right <= button.left || group.left >= button.right || group.bottom <= button.top || group.top >= button.bottom,
              `${group.provider} intersects header button: ${JSON.stringify(geometry)}`);
          }
        }
        assert(geometry.groups.every(g => g.top >= geometry.buttons[0].bottom), 'phone ticker must sit under controls');
      } else {
        assert(geometry.groups.every(g => g.top < geometry.buttons[0].bottom), `${width}px desktop/tablet ticker moved to new row`);
      }
      if (width === 390) await page.locator('#messages [data-kind="assistant"]').last().waitFor();
      const shot = path.join(os.tmpdir(), `jevons-t1274-${width}.png`);
      await page.screenshot({ path: shot });
      console.log(`ok T1274 ${width}x844 ${JSON.stringify(geometry)} screenshot=${shot}`);
      await page.close();
    }
  } finally { await browser.close(); server.close(); }
}
main().catch(e => { console.error(e); process.exitCode = 1; });
