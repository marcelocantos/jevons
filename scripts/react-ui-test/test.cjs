// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Built React interaction checks. --host runs the same owner send/reload slice
// against a real isolated daemon without API or transport mocks.
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('../browser-loop-test/node_modules/playwright');

const { values } = parseArgs({ options: {
  host: { type: 'string' }, provider: { type: 'string' }, screenshot: { type: 'string' },
} });
const dist = path.resolve(__dirname, '../../ui/dist');
const live = Boolean(values.host);
let server;
let browser;

async function main() {
  let base;
  if (live) {
    base = new URL(`http://${values.host}`);
    assert(['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'journey requires a local isolate');
    assert(base.port && base.port !== '13705' && base.port !== '13706', 'refusing development/comparison ports');
  } else {
    await fs.access(path.join(dist, 'index.html'));
    server = http.createServer(async (req, res) => {
      try {
        const url = new URL(req.url, 'http://127.0.0.1');
        const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
        assert(file.startsWith(dist + path.sep));
        const data = await fs.readFile(file);
        res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)] || 'application/octet-stream');
        res.end(data);
      } catch {
        res.writeHead(404).end();
      }
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    base = new URL(`http://127.0.0.1:${server.address().port}`);
  }

  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(live ? 90000 : 10000);
  const errors = [];
  const received = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => socket.on('framereceived', ({ payload }) => {
    try { received.push(JSON.parse(String(payload))); } catch { /* non-JSON protocol traffic */ }
  }));
  // Fonts are presentation dependencies, not agent/backend substitutes.
  await page.route('https://fonts.**/*', route => route.abort());

  if (!live) {
    const histories = new Map();
    const agents = ['jevons', 'jevons-po'].map(name => ({ name, parent: name === 'jevons-po' ? 'jevons' : '', purpose: name === 'jevons-po' ? 'po' : 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' }));
    await page.route('**/api/**', async route => {
      const pathname = new URL(route.request().url()).pathname;
      const body = pathname === '/api/agents' ? agents : pathname === '/api/plan-usage' ? { backends: [] } : pathname === '/api/frontier' ? [] : {};
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });
    await page.routeWebSocket('**/ws/mux', socket => {
      const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
      const replay = ch => {
        const rows = histories.get(ch) || [];
        for (const row of rows) send(ch, 'frame', row);
        send(ch, 'meta', { lo: 1, hi: 0, n: rows.length, start: 1, older: 0, total: rows.length, following: true, working: false, phase: { phase: 'idle' }, overseer_down: '' });
      };
      socket.onMessage(payload => {
        const frame = JSON.parse(String(payload));
        if (frame.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
        if (frame.t === 'open' || frame.t === 'window') return replay(frame.ch);
        if (frame.t !== 'send') return;
        const text = frame.body.text;
        const rows = histories.get(frame.ch) || [];
        const answer = text.replace(/^Reply with exactly: /, '');
        for (const [type, content] of [['user', text], ['assistant', answer]]) {
          const index = rows.length + 1;
          rows.push({ id: `e:${index}`, index, op: 'put', type, event: {
            type, turn_origin: type === 'user' ? 'owner' : undefined,
            message: { role: type, content: [{ type: 'text', text: content }], stop_reason: type === 'assistant' ? 'end_turn' : undefined },
          } });
        }
        histories.set(frame.ch, rows);
        setImmediate(() => replay(frame.ch));
      });
    });
  }

  await page.goto(base.href);
  if (live) {
    const agents = await page.evaluate(async () => (await fetch('/api/agents')).json());
    const root = agents.find(agent => agent.name === 'jevons');
    assert(root?.provider, 'effective overseer provider is unavailable');
    if (values.provider) assert.equal(root.provider, values.provider, 'effective provider differs from the journey request');
    console.log(`Effective provider: ${root.provider}`);
  }

  async function sendAndReload({ input, button, transcript, agent }) {
    const token = `react-${randomUUID()}`;
    const prompt = `Reply with exactly: ${token}`;
    await page.locator(input).fill(prompt);
    await page.locator(button).click();
    await page.waitForFunction(({ transcript, token }) => [...document.querySelectorAll(`${transcript} [data-kind="assistant"] .msg-body`)].some(el => el.textContent.includes(token)), { transcript, token });
    assert.equal(await page.locator(`${transcript} [data-kind="user"] .msg-body`).filter({ hasText: prompt }).count(), 1, 'owner turn appears exactly once');
    assert.equal(await page.locator(`${transcript} [data-kind="assistant"] .msg-body`).filter({ hasText: token }).count(), 1, 'request-specific reply appears exactly once');
    if (live) {
      // A token can paint before the provider seals the turn. Wait for the
      // correlated terminal frame rather than racing the first visible delta.
      const deadline = Date.now() + 90000;
      const terminal = () => received.find(frame => frame.ch === `transcript:${agent}` && frame.t === 'frame' && frame.body?.event?.type === 'assistant' && JSON.stringify(frame.body.event.message?.content || []).includes(token) && ['end_turn', 'stop_sequence'].includes(frame.body.event.message?.stop_reason));
      while (!terminal() && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 100));
      assert(terminal(), 'matching response has no terminal provider evidence');
      console.log(`Correlated terminal: ${JSON.stringify(terminal())}`);
    }
    await page.waitForFunction(selector => document.querySelector(selector)?.value === '', input);
    await page.reload();
    await page.waitForFunction(({ transcript, token }) => [...document.querySelectorAll(`${transcript} [data-kind="assistant"] .msg-body`)].some(el => el.textContent.includes(token)), { transcript, token });
    assert.equal(await page.locator(`${transcript} [data-kind="user"] .msg-body`).filter({ hasText: prompt }).count(), 1, 'reload preserves one owner turn');
    assert.equal(await page.locator(`${transcript} [data-kind="assistant"] .msg-body`).filter({ hasText: token }).count(), 1, 'reload preserves one matching reply');
    assert.equal(await page.locator(input).inputValue(), '', 'sent draft remains cleared after reload');
    assert.equal(await page.locator(`${transcript} [data-kind="diagnostic"]`).count(), 0, 'send has no diagnostic failure');
  }

  await sendAndReload({ input: '#input', button: '#send', transcript: '#messages', agent: 'jevons' });
  if (!live) {
    await page.goto(new URL('/?agent=jevons-po&tab=transcript', base).href);
    await page.locator('#agent-inspect[data-agent-id="jevons-po"]').waitFor({ state: 'visible' });
    await sendAndReload({ input: '#agent-inspect-input', button: '#agent-inspect-send', transcript: '#agent-inspect-body', agent: 'jevons-po' });
  }
  assert.deepEqual(errors, [], 'React has no uncaught browser errors');
  const screenshot = values.screenshot || path.join(await fs.mkdtemp(path.join(os.tmpdir(), 'jevons-react-ui-')), 'cockpit.png');
  await page.screenshot({ path: screenshot });
  console.log(`Screenshot: ${screenshot}`);
  console.log(live ? 'PASS: packaged React owner send, correlated terminal reply and reload (real provider)' : 'PASS: built React main/sidebar send, real query deep link and reload (mocked transport; not a live journey)');
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => {
  await browser?.close();
  if (server) await new Promise(resolve => server.close(resolve));
});
