// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T404.1: open owner questions surface — list contract fields, mark
// STALLS, answer closed-form in place, POST routes back to the asker seat.
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('./playwright.cjs')();

const { values } = parseArgs({ options: { screenshot: { type: 'string' } } });
const dist = path.resolve(__dirname, '../../ui/dist');
const STALLS = 'STALLS — asker stated no default; silence blocks this seat (🎯T404)';
const reviews = [
  {
    schema_version: 2,
    target_lookup: 'missing',
    evidence: { commit: { status: 'missing' }, gate: { status: 'missing' }, diff: { status: 'missing' }, report: { status: 'missing' }, screenshots: [] },
    id: 'rev-closed',
    url: '/api/reviews/rev-closed',
    repository: 'jevons',
    target: 'T404.1',
    ask_id: 'surface',
    version: '1',
    question: 'Cockpit panel or Slack thread for owner answers?',
    asker: 'jv-t404-auto',
    answer_route: 'jv-t404-auto',
    default_on_silence: 'proceeds on default: cockpit unless you say otherwise',
    options: ['cockpit', 'slack'],
    state: 'open',
    readiness: 'unspecified',
  },
  {
    schema_version: 2,
    target_lookup: 'missing',
    evidence: { commit: { status: 'missing' }, gate: { status: 'missing' }, diff: { status: 'missing' }, report: { status: 'missing' }, screenshots: [] },
    id: 'rev-stall',
    url: '/api/reviews/rev-stall',
    repository: 'jevons',
    target: 'T404.1B',
    ask_id: 'stall',
    version: '1',
    question: 'Pick a color with no default?',
    asker: 'jv-t404-stall',
    answer_route: 'jv-t404-stall',
    default_on_silence: STALLS,
    state: 'open',
    readiness: 'unspecified',
  },
];

let server;
let browser;
let routed = [];

async function main() {
  await fs.access(path.join(dist, 'index.html'));
  server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://127.0.0.1');
    if (url.pathname === '/api/reviews') {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify(reviews));
      return;
    }
    const answerMatch = /^\/api\/reviews\/([^/]+)\/answer$/.exec(url.pathname);
    if (answerMatch && req.method === 'POST') {
      let body = '';
      for await (const chunk of req) body += chunk;
      const parsed = JSON.parse(body);
      routed.push({ id: answerMatch[1], answer: parsed.answer });
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ status: 'answered', routed: 'routed: answered to jv-t404-auto (sent)' }));
      return;
    }
    if (url.pathname === '/api/owner-cockpit/session') {
      res.writeHead(204);
      res.end();
      return;
    }
    try {
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' }[path.extname(file)] || 'application/octet-stream');
      res.end(data);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = new URL(`http://127.0.0.1:${server.address().port}`);

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.setDefaultTimeout(15000);
  await page.route('https://fonts.**/*', (route) => route.abort());
  await page.route('**/api/agents', (route) => route.fulfill({ contentType: 'application/json', body: '[]' }));
  await page.route('**/api/frontier', (route) => route.fulfill({ contentType: 'application/json', body: '[]' }));
  await page.route('**/api/plan-usage', (route) => route.fulfill({ contentType: 'application/json', body: '{"backends":[]}' }));

  await page.goto(base.href);
  await page.locator('#rhs-tab-review').click();
  await page.locator('.review-card[data-review-id="rev-closed"]').waitFor();
  const closed = page.locator('.review-card[data-review-id="rev-closed"]');
  await closed.locator('dt', { hasText: 'Asker' }).waitFor();
  await closed.getByText('cockpit unless you say otherwise').waitFor();
  const stall = page.locator('.review-card[data-review-id="rev-stall"]');
  await stall.locator('.review-stalls-mark').waitFor();
  await stall.getByText(STALLS).waitFor();

  await closed.getByRole('button', { name: 'slack' }).click();
  await (async () => {
    const deadline = Date.now() + 10000;
    while (Date.now() < deadline) {
      if (routed.length === 1) return;
      await new Promise((r) => setTimeout(r, 50));
    }
    throw new Error('answer POST did not land');
  })();
  assert.equal(routed[0].id, 'rev-closed');
  assert.equal(routed[0].answer, 'slack');

  const prose = `prose-${randomUUID()}`;
  await stall.locator('.review-prose-input').fill(prose);
  await stall.getByRole('button', { name: 'Submit answer' }).click();
  await (async () => {
    const deadline = Date.now() + 10000;
    while (Date.now() < deadline) {
      if (routed.length === 2 && routed[1].answer === prose) return;
      await new Promise((r) => setTimeout(r, 50));
    }
    throw new Error('prose answer POST did not land');
  })();
  assert.equal(routed[1].id, 'rev-stall');

  const shot = values.screenshot || path.join(await fs.mkdtemp(path.join(os.tmpdir(), 'jevons-t404-')), 'open-questions.png');
  await page.screenshot({ path: shot, fullPage: false });
  console.log('T404.1 open-questions UI oracle GREEN; screenshot:', shot);
}

main()
  .catch((err) => {
    console.error(err);
    process.exit(1);
  })
  .finally(async () => {
    if (browser) await browser.close();
    if (server) server.close();
  });
