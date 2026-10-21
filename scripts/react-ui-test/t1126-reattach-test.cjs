// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T1126 isolate live check: the packaged React cockpit, served from a
// throwaway local HTTP server with a mocked /ws/mux transport — never the
// real development daemon on :13705. Reproduces the owner's complaint at
// both desktop and mobile widths: scroll away from the live end, send a
// message, confirm the view re-attaches (sent bubble + reply visible, no
// ↓ Latest button left behind).
//
// Deliberately standalone from scripts/react-ui-test/test.cjs's --host mode
// (🎯T1120 lesson: never point simulated input at a real daemon, development
// or otherwise). This fixture is fully synthetic: a static dist server plus
// an in-process WebSocket mock, so no real provider/session is touched.
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
let server;
let browser;

async function main() {
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
  const base = new URL(`http://127.0.0.1:${server.address().port}`);
  console.log(`Isolate base: ${base.href} (throwaway port, not :13705)`);

  browser = await chromium.launch({ headless: true });
  const screenshotBase = values.screenshot || path.join(await fs.mkdtemp(path.join(os.tmpdir(), 'jevons-t1126-')), 'cockpit');
  const errors = [];

  async function checkAtWidth({ width, height, label }) {
    const page = await browser.newPage({ viewport: { width, height } });
    page.on('pageerror', error => errors.push(`[${label}] ${error.message}`));
    page.setDefaultTimeout(10000);
    await page.route('https://fonts.**/*', route => route.abort());

    // 24 earlier rows so #messages starts well above one viewport's worth
    // of content — the scroll-away this bug needs to reproduce.
    const rows = [];
    const add = (type, text, origin) => {
      const index = rows.length + 1;
      rows.push({ id: `e:${index}`, index, op: 'put', type, event: {
        type, turn_origin: origin,
        message: { role: type, content: [{ type: 'text', text }], stop_reason: type === 'assistant' ? 'end_turn' : undefined },
      } });
    };
    for (let i = 0; i < 12; i++) {
      add('user', `Earlier owner request ${i} — ${label}`, 'owner');
      add('assistant', `Reply ${i}: a long enough earlier assistant turn so the transcript canvas is taller than the ${label} viewport and the owner can scroll meaningfully away from the live end.`, undefined);
    }
    const histories = new Map([[`transcript:jevons`, rows]]);
    const agents = [{ name: 'jevons', parent: '', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' }];
    await page.route('**/api/**', async route => {
      const pathname = new URL(route.request().url()).pathname;
      const body = pathname === '/api/agents' ? agents : pathname === '/api/plan-usage' ? { backends: [] } : pathname === '/api/frontier' ? [] : {};
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });
    await page.routeWebSocket('**/ws/mux', socket => {
      const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
      const replay = ch => {
        const hist = histories.get(ch) || [];
        for (const row of hist) send(ch, 'frame', row);
        send(ch, 'meta', { lo: 1, hi: 0, n: hist.length, start: 1, older: 0, total: hist.length, following: true, working: false, phase: { phase: 'idle' }, overseer_down: '' });
      };
      socket.onMessage(payload => {
        const frame = JSON.parse(String(payload));
        if (frame.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
        if (frame.t === 'open' || frame.t === 'window') return replay(frame.ch);
        if (frame.t !== 'send') return;
        const text = frame.body.text;
        const hist = histories.get(frame.ch) || [];
        const answer = text.replace(/^Reply with exactly: /, '');
        for (const [type, content] of [['user', text], ['assistant', answer]]) {
          const index = hist.length + 1;
          hist.push({ id: `e:${index}`, index, op: 'put', type, event: {
            type, turn_origin: type === 'user' ? 'owner' : undefined,
            message: { role: type, content: [{ type: 'text', text: type === 'user' ? `<user_query>${content}</user_query>` : content }], stop_reason: type === 'assistant' ? 'end_turn' : undefined },
          } });
        }
        histories.set(frame.ch, hist);
        setImmediate(() => replay(frame.ch));
      });
    });

    await page.goto(base.href);
    await page.locator('#messages').waitFor({ state: 'visible' });
    // Wait for hydrate to settle at the live end before driving a scroll —
    // a scroll fired mid-hydrate-pin is the pin's own write, not a leave
    // (see ui/src/components/t603_repin_after_settle.test.tsx).
    await page.waitForTimeout(300);

    // Scroll well away from the live end.
    await page.locator('#messages').evaluate(el => { el.scrollTop = Math.max(0, el.scrollTop - 4000); });
    await page.dispatchEvent('#messages', 'scroll');
    await page.waitForSelector('#jump-bottom:not([hidden])', { timeout: 5000 });
    const beforeShot = `${screenshotBase}-${label}-before-send.png`;
    await page.screenshot({ path: beforeShot });
    console.log(`[${label}] ↓ Latest visible after scroll-away. Screenshot: ${beforeShot}`);

    // Owner sends while scrolled up.
    const token = `reattach-${label}-${randomUUID()}`;
    const prompt = `Reply with exactly: ${token}`;
    await page.locator('#input').fill(prompt);
    await page.locator('#send').click();

    await page.waitForFunction(token => [...document.querySelectorAll('#messages [data-kind="assistant"] .msg-body')].some(el => el.textContent.includes(token)), token);

    // No ↓ Latest remains, and both the sent bubble and the reply are
    // within #messages' visible viewport (not merely in the DOM).
    const afterShot = `${screenshotBase}-${label}-after-send.png`;
    await page.screenshot({ path: afterShot });
    const hidden = await page.locator('#jump-bottom').getAttribute('hidden');
    assert.notEqual(hidden, null, `[${label}] ↓ Latest button remains after send`);

    const visible = await page.locator('#messages').evaluate((el, { prompt, token }) => {
      const paneBox = el.getBoundingClientRect();
      const within = (box) => box.bottom > paneBox.top && box.top < paneBox.bottom;
      const userBubble = [...el.querySelectorAll('[data-kind="user"] .msg-body')].find(n => n.textContent.includes(prompt));
      const assistantBubble = [...el.querySelectorAll('[data-kind="assistant"] .msg-body')].find(n => n.textContent.includes(token));
      return {
        userFound: !!userBubble, userVisible: userBubble ? within(userBubble.getBoundingClientRect()) : false,
        assistantFound: !!assistantBubble, assistantVisible: assistantBubble ? within(assistantBubble.getBoundingClientRect()) : false,
      };
    }, { prompt, token });
    assert(visible.userFound, `[${label}] sent owner bubble not found in DOM`);
    assert(visible.userVisible, `[${label}] sent owner bubble is in the DOM but off-screen`);
    assert(visible.assistantFound, `[${label}] reply bubble not found in DOM`);
    assert(visible.assistantVisible, `[${label}] reply bubble is in the DOM but off-screen`);
    console.log(`[${label}] PASS: ↓ Latest gone, sent bubble + reply both on-screen. Screenshot: ${afterShot}`);
    await page.close();
  }

  await checkAtWidth({ width: 1440, height: 1000, label: 'desktop' });
  await checkAtWidth({ width: 390, height: 844, label: 'mobile' });

  assert.deepEqual(errors, [], 'React has no uncaught browser errors');
  console.log('PASS: T1126 owner-send reattach, verified at desktop and mobile widths, on an isolated fixture (no real daemon touched)');
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => {
  await browser?.close();
  if (server) await new Promise(resolve => server.close(resolve));
});
