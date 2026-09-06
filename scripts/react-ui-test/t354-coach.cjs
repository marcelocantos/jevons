// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright product path for 🎯T354: React Coach tab fetches dispositions
// and paints pending / filed / ignored rows. Empty is calm, not an error.
'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('../browser-loop-test/node_modules/playwright');

const dist = path.resolve(__dirname, '../../ui/dist');
const PAYLOAD = {
  total: 3,
  count: 3,
  pending: 1,
  judgments: [
    {
      fingerprint: 'fp-pending',
      name: 'chat gap',
      observation: 'owner asked twice before a reply landed',
      severity: 'medium',
      delivered_at: '2026-08-09T03:00:00Z',
      disposition: 'pending',
      evidence: 'owner_chat:chatlog-2026-08-09 (chat_gap)',
    },
    {
      fingerprint: 'fp-ignored',
      name: 'phrase friction',
      observation: 'repeat phrasing in overseer replies',
      severity: 'low',
      delivered_at: '2026-08-09T02:00:00Z',
      disposition: 'ignore_with_reason',
      reason: 'one-off, no standing pattern',
    },
    {
      fingerprint: 'fp-filed',
      name: 'repair churn',
      observation: 'three follow-up commits on the same file',
      severity: 'high',
      mode: 'retro',
      delivered_at: '2026-08-09T01:00:00Z',
      disposition: 'file',
      target_id: 'T999',
    },
  ],
};

function contentType(file) {
  return ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)]) || 'application/octet-stream';
}

async function main() {
  await fs.access(path.join(dist, 'index.html'));
  let dispositionCalls = 0;
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://127.0.0.1');
      if (url.pathname === '/api/rsi/dispositions') {
        dispositionCalls += 1;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(PAYLOAD));
        return;
      }
      if (url.pathname.startsWith('/api/')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(url.pathname === '/api/agents'
          ? [{ name: 'jevons', purpose: 'overseer', running: true, status: 'running' }]
          : { targets: [], windows: [] }));
        return;
      }
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader('Content-Type', contentType(file));
      res.end(data);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  page.setDefaultTimeout(15000);
  await page.route('https://fonts.**/*', (route) => route.abort());
  await page.routeWebSocket('**/ws/mux', (socket) => {
    socket.onMessage((payload) => {
      try {
        const frame = JSON.parse(String(payload));
        if (frame.type === 'ping') socket.send(JSON.stringify({ type: 'pong' }));
      } catch { /* ignore */ }
    });
  });
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    const before = dispositionCalls;
    await page.locator('#rhs-tab-coach').click();
    await page.waitForFunction(() => document.querySelectorAll('#coach-body .coach-row').length >= 3);
    assert(dispositionCalls > before, 'Coach tab must request GET /api/rsi/dispositions');
    const text = await page.locator('#coach-body').innerText();
    const folded = text.toLowerCase();
    assert(folded.includes('chat gap'), text);
    assert(folded.includes('filed'), text);
    assert(folded.includes('ignored'), text);
    assert(/T999/.test(text), text);
    assert(text.includes('⏮') || folded.includes('repair churn'), text);
    const counts = await page.locator('#coach-counts').innerText();
    assert.equal(counts, '3 judgments · 1 pending');
    assert.equal(await page.locator('.coach-chip.coach-pending').count(), 1);
    assert.equal(await page.locator('#coach-pane .ai-empty').count(), 0);
    console.log('ok - T354 Coach tab lists durable judgments from GET /api/rsi/dispositions');
  } finally {
    await browser.close();
    server.close();
  }
}

main();
