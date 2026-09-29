// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T789: the transcript stays pinned to the latest message through an owner
// send while the seat is busy and a Cut in. Since 🎯T903 a busy send escalates:
// the daemon steers it and the pane grows an escalation strip above the
// composer (it used to grow the queue strip), and Cut in is ⌘⇧Enter. Drives
// the REAL composer against the built bundle over a mocked mux transport; a
// passive pane cannot reproduce this (it stays pinned while idle).
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('../browser-loop-test/node_modules/playwright');

const dist = path.resolve(__dirname, '../../ui/dist');
const ROW_TEXT = 'A long agent report paragraph. '.repeat(40);

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
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('https://fonts.**/*', r => r.abort());
    const agents = [{ name: 'jevons', parent: '', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'streaming' }];
    await page.route('**/api/**', async route => {
      const p = new URL(route.request().url()).pathname;
      const body = p === '/api/agents' ? agents : p === '/api/plan-usage' ? { backends: [] } : p === '/api/frontier' ? [] : {};
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
    });
    const rows = [];
    const add = (type, text, origin) => {
      const index = rows.length + 1;
      rows.push({ id: `e:${index}`, index, op: 'put', type, event: { type, turn_origin: origin,
        message: { role: type, content: [{ type: 'text', text: type === 'user' ? `<user_query>${text}</user_query>` : text }], stop_reason: type === 'assistant' ? 'end_turn' : undefined } } });
    };
    for (let i = 0; i < 40; i++) { add('user', `Earlier request ${i}`, 'owner'); add('assistant', `${i}: ${ROW_TEXT}`); }
    let phase = 'streaming';
    await page.routeWebSocket('**/ws/mux', socket => {
      const send = (ch, t, body) => socket.send(JSON.stringify({ v: 1, ch, t, body }));
      const replay = ch => {
        for (const row of rows) send(ch, 'frame', row);
        send(ch, 'meta', { lo: 1, hi: 0, n: rows.length, start: 1, older: 0, total: rows.length, following: true, working: phase !== 'idle', phase: { phase }, overseer_down: '' });
      };
      socket.onMessage(payload => {
        const f = JSON.parse(String(payload));
        if (f.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
        if (f.t === 'open' || f.t === 'window') return replay(f.ch);
        if (f.t !== 'send') return;
        add('user', f.body.text, 'owner');
        if (f.body.mode === 'interrupt') {
          // Cut in: the interrupt lands the owner turn and the seat starts answering.
          phase = 'thinking';
        } else {
          // A plain send to the busy seat: steered, as the daemon answers it.
          send(f.ch, 'status', { id: f.body.id, status: 'steered', mode: 'submit', mechanism: 'steer', interrupt_after_ms: 60000, message: 'steered' });
        }
        setImmediate(() => replay(f.ch));
      });
    });
    await page.goto(base);
    await page.locator('#messages [data-kind="assistant"]').last().waitFor();
    const gap = () => page.locator('#messages').evaluate(el => ({ fromBottom: el.scrollHeight - el.scrollTop - el.clientHeight, scrollHeight: el.scrollHeight }));
    const settle = async () => { await page.waitForTimeout(1500); return gap(); };
    const atLoad = await settle();
    assert(atLoad.fromBottom <= 8, `pinned at load: ${JSON.stringify(atLoad)}`);
    assert(atLoad.scrollHeight > 6000, 'fixture is a long transcript');

    // Several sends while busy: each escalates and the escalation strip grows
    // above the composer, so a pane that is not re-pinned when it appears
    // reads as a user leave.
    for (const text of ['first held message', 'second held message', 'third held message']) {
      await page.locator('#input').fill(text);
      await page.locator('#input').press('Enter');
      await page.waitForTimeout(200);
    }
    await page.locator('.escalation-strip').first().waitFor();
    const queued = await settle();
    await page.locator('#input').fill('please cut in with this');
    await page.locator('#input').press('Meta+Shift+Enter');
    await page.waitForFunction(() => [...document.querySelectorAll('#messages [data-kind="user"] .msg-body')].some(e => /cut in/.test(e.textContent)));
    const after = await settle();
    const latestShown = await page.locator('#jump-bottom').isVisible();
    console.log(`at load ${JSON.stringify(atLoad)}; queued ${JSON.stringify(queued)}; after cut in ${JSON.stringify(after)}; latest button visible=${latestShown}`);
    assert(queued.fromBottom <= 8, `pinned with the escalation strip showing: ${JSON.stringify(queued)}`);
    assert(after.fromBottom <= 8, `pinned after Cut in: ${JSON.stringify(after)}`);
    assert.equal(latestShown, false, '↓ Latest must not be showing');
    assert.deepEqual(errors, []);
    console.log('PASS: T789 pinned through an escalated send and Cut in');
  } finally {
    await browser.close();
    server.close();
  }
}
main().catch(e => { console.error(e); process.exitCode = 1; });
