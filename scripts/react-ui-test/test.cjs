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
  const screenshot = values.screenshot || path.join(await fs.mkdtemp(path.join(os.tmpdir(), 'jevons-react-ui-')), 'cockpit.png');
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
    for (const agent of agents) {
      const rows = [];
      const add = (type, text, origin) => {
        const index = rows.length + 1;
        rows.push({ id: `e:${index}`, index, op: 'put', type, event: {
          type, turn_origin: origin,
          message: { role: type, content: [{ type: 'text', text }], stop_reason: type === 'assistant' ? 'end_turn' : undefined },
        } });
      };
      // Long enough that e:1 begins outside the viewport. Distinct canonical
      // owner IDs with equal text, including an agent-origin predecessor,
      // catch mapper errors the standalone composer fixture cannot expose.
      for (let i = 0; i < 12; i++) {
        add('user', `Earlier owner request ${i} for ${agent.name}`, 'owner');
        add('assistant', `I recorded request ${i}. This response belongs to ${agent.name}. The conversation keeps earlier requests available while new replies arrive.`, undefined);
      }
      add('user', `Shared request text for ${agent.name}`, 'agent');
      add('user', `Shared request text for ${agent.name}`, 'owner');
      add('user', `Shared request text for ${agent.name}`, 'owner');
      histories.set(`transcript:${agent.name}`, rows);
    }
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
            message: { role: type, content: [{ type: 'text', text: type === 'user' ? `<user_query>${content}</user_query>` : content }], stop_reason: type === 'assistant' ? 'end_turn' : undefined },
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
    return prompt;
  }

  // Recall is a UI slice only. This does not certify provider rewind/resend;
  // that requires its own correlated live operation and retained-context check.
  async function recallAndCancel({ input, transcript, prompt, agent }) {
    const composer = page.locator(input);
    const draft = `unfinished-${randomUUID()}`;
    await composer.fill(draft);
    await composer.press('Alt+ArrowUp');
    assert.equal(await composer.inputValue(), prompt, 'Alt+Up recalls the selected agent\'s last owner request');
    const selected = page.locator(`${transcript} [data-kind="user"][aria-current="true"]`);
    await selected.waitFor({ state: 'visible' });
    assert.equal(await selected.count(), 1, 'one canonical turn is highlighted');
    assert.equal(await selected.locator('.msg-body').textContent(), prompt, 'highlight matches the recalled request');
    assert(await selected.getAttribute('data-event-id'), 'recall has a canonical event identity');
    if (!live) {
      const oldestInitiallyVisible = await page.locator(transcript).evaluate(el => {
        const oldest = el.querySelector('[data-event-id="e:1"]');
        if (!oldest) return false;
        const row = oldest.getBoundingClientRect(), pane = el.getBoundingClientRect();
        return row.bottom > pane.top && row.top < pane.bottom;
      });
      assert.equal(oldestInitiallyVisible, false, 'older recall begins offscreen');
      await composer.press('Alt+ArrowUp');
      assert.equal(await composer.inputValue(), `Shared request text for ${agent}`);
      assert.equal(await selected.getAttribute('data-event-id'), 'e:27');
      await composer.press('Alt+ArrowUp');
      assert.equal(await composer.inputValue(), `Shared request text for ${agent}`);
      assert.equal(await selected.getAttribute('data-event-id'), 'e:26', 'identical owner text retains distinct identity');
      await composer.press('Alt+ArrowUp');
      assert.equal(await composer.inputValue(), `Earlier owner request 11 for ${agent}`, 'agent-origin row is excluded from owner recall');
      for (let i = 10; i >= 0; i--) await composer.press('Alt+ArrowUp');
      assert.equal(await composer.inputValue(), `Earlier owner request 0 for ${agent}`);
      assert.equal(await selected.getAttribute('data-event-id'), 'e:1');
    }
    await page.waitForFunction(transcript => {
      const pane = document.querySelector(transcript);
      const selected = pane?.querySelector('[aria-current="true"]');
      if (!pane || !selected) return false;
      const rowBox = selected.getBoundingClientRect(), paneBox = pane.getBoundingClientRect();
      return rowBox.bottom > paneBox.top && rowBox.top < paneBox.bottom;
    }, transcript);
    const recallScreenshot = screenshot.replace(/\.png$/, '') + (input === '#input' ? '-recall-main.png' : '-recall-sidebar.png');
    await page.screenshot({ path: recallScreenshot });
    console.log(`Recall screenshot: ${recallScreenshot}`);
    await composer.fill('an edit that must not send on cancel');
    await composer.press('Escape');
    assert.equal(await composer.inputValue(), draft, 'Escape restores the unsent draft');
    assert.equal(await selected.count(), 0, 'Escape clears the selected turn');
    await composer.press('Alt+ArrowUp');
    await composer.press('Alt+ArrowDown');
    assert.equal(await composer.inputValue(), draft, 'Alt+Down past newest restores the unsent draft');
    assert.equal(await selected.count(), 0, 'Alt+Down clears the selected turn');
    await composer.fill('');
  }

  const mainPrompt = await sendAndReload({ input: '#input', button: '#send', transcript: '#messages', agent: 'jevons' });
  await recallAndCancel({ input: '#input', transcript: '#messages', prompt: mainPrompt, agent: 'jevons' });
  if (!live) {
    await page.goto(new URL('/?agent=jevons-po&tab=transcript', base).href);
    await page.locator('#agent-inspect[data-agent-id="jevons-po"]').waitFor({ state: 'visible' });
    const sidePrompt = await sendAndReload({ input: '#agent-inspect-input', button: '#agent-inspect-send', transcript: '#agent-inspect-body', agent: 'jevons-po' });
    await recallAndCancel({ input: '#agent-inspect-input', transcript: '#agent-inspect-body', prompt: sidePrompt, agent: 'jevons-po' });
  }
  assert.deepEqual(errors, [], 'React has no uncaught browser errors');
  await page.screenshot({ path: screenshot });
  console.log(`Screenshot: ${screenshot}`);
  console.log(live ? 'PASS: packaged React owner send, correlated terminal reply, reload and recall/cancel (real provider; rewind not exercised)' : 'PASS: built React main/sidebar send, real query deep link, reload and recall/cancel (mocked transport; not a live journey; rewind not exercised)');
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => {
  await browser?.close();
  if (server) await new Promise(resolve => server.close(resolve));
});
