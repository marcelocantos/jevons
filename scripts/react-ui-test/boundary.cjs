// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Real provider, packaged React, indexed mux: owner speaks while a response is
// held inside an actual tool call. No seeded assistant events or socket mocks.
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('../browser-loop-test/node_modules/playwright');
const { values } = parseArgs({ options: {
  host: { type: 'string' }, provider: { type: 'string' }, workdir: { type: 'string' }, aside: { type: 'string' },
  'daemon-log': { type: 'string' }, 'sweep-deadline-ms': { type: 'string' },
} });
const base = new URL(`http://${values.host}`);
assert(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
assert(base.port && !['13705', '13706'].includes(base.port), 'isolate only');
assert(values.workdir && values.aside && values.provider);
assert(values['daemon-log'], 'actual daemon sweep evidence is required');
const sweepDeadline = Number(values['sweep-deadline-ms']);
assert(Number.isSafeInteger(sweepDeadline) && sweepDeadline > 0);
let browser;
const frames = [];
const releases = [];
let asideCreated = false;
const content = body => {
  const c = body?.event?.message?.content;
  return typeof c === 'string' ? c : (c || []).filter(b => b.type === 'text' || !b.type).map(b => b.text || '').join('');
};
const terminal = body => ['end_turn', 'stop_sequence'].includes(body?.event?.message?.stop_reason);
async function until(check, label, timeout = 90000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const found = await check();
    if (found) return found;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(`Timed out: ${label}`);
}
async function mcp(name, args) {
  const response = await fetch(new URL('/mcp', base), {
    method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' },
    body: JSON.stringify({ jsonrpc: '2.0', id: randomUUID(), method: 'tools/call', params: { name, arguments: args } }),
  });
  assert(response.ok, `${name}: HTTP ${response.status}`);
  const data = await response.json();
  assert(!data.error && !data.result?.isError, `${name}: ${JSON.stringify(data)}`);
}
async function main() {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(90000);
  const errors = [];
  const checked = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => socket.on('framereceived', ({ payload }) => {
    try { frames.push(JSON.parse(String(payload))); } catch { /* heartbeat */ }
  }));
  await page.route('https://fonts.**/*', route => route.abort());
  await page.goto(base.href);
  const agents = await (await fetch(new URL('/api/agents', base))).json();
  assert.equal(agents.find(a => a.name === 'jevons')?.provider, values.provider);
  const sideWork = await fs.mkdtemp(path.join(values.workdir, 'boundary-aside-'));
  await mcp('jevons_thread_spawn', { id: values.aside, provider: values.provider, workdir: sideWork, description: 'isolated owner-boundary check' });
  asideCreated = true;

  for (const name of ['jevons', values.aside]) {
    const main = name === 'jevons';
    const input = main ? '#input' : '#agent-inspect-input';
    const button = main ? '#send' : '#agent-inspect-send';
    const transcript = main ? '#messages' : '#agent-inspect-body';
    if (!main) await page.goto(new URL(`/?agent=${encodeURIComponent(name)}&tab=transcript`, base).href);
    const work = await fs.mkdtemp(path.join(main ? values.workdir : sideWork, 'boundary-'));
    const nonce = randomUUID();
    const pre = `PRE-${nonce}`, post = `POST-${nonce}`, ack = `ACK-${nonce}`;
    const ready = path.join(work, 'ready'), release = path.join(work, 'release'), completed = path.join(work, 'completed');
    releases.push(release);
    // This independent bounded helper is the agent's real tool workload. Its
    // clock is a failure bound; release occurs only after the observed owner echo.
    const helper = path.join(work, 'wait.cjs');
    const holdLimit = main ? 90000 : sweepDeadline + 90000;
    await fs.writeFile(helper, `const fs = require('node:fs');\nfs.writeFileSync(${JSON.stringify(ready)}, ${JSON.stringify(nonce)});\nconst limit = setTimeout(() => { console.error('owner boundary was not released'); process.exit(2); }, ${holdLimit});\nconst poll = setInterval(() => { if (fs.existsSync(${JSON.stringify(release)})) { clearInterval(poll); clearTimeout(limit); fs.writeFileSync(${JSON.stringify(completed)}, ${JSON.stringify(nonce)}); console.log('released'); } }, 25);\n`);
    const prompt = `Perform this short interaction check in order. First emit ${pre} as a visible commentary message, not just tool input. Then run exactly this shell command using your tool: node ${helper}. Wait for it to finish. Then reply with exactly ${post}. Do not read or change the helper or its files; the test harness releases it. Do not spawn agents.`;
    const owner = `Reply with exactly: ${ack}`;
    const start = frames.length;
    const events = () => frames.slice(start).filter(f => f.ch === `transcript:${name}` && f.t === 'frame');
    const assistants = () => events().filter(f => f.body?.event?.type === 'assistant');
    await page.locator(input).fill(prompt);
    await page.locator(button).click();
    try {
      const first = await until(() => assistants().find(f => content(f.body).includes(pre)), 'nonterminal PRE');
      assert(!terminal(first.body), 'PRE must precede provider terminal');
      await until(async () => { try { return await fs.readFile(ready, 'utf8') === nonce; } catch { return false; } }, 'actual tool ready marker');
      assert(!assistants().some(f => terminal(f.body)), 'first response ended before owner interleaving');
      await page.waitForFunction(({ transcript, pre }) => [...document.querySelectorAll(`${transcript} [data-kind="assistant"] .msg-body`)].some(el => el.textContent.includes(pre)), { transcript, pre });
      await page.locator(input).fill(owner);
      await page.locator(button).click();
      const echo = await until(() => events().find(f => f.body?.event?.turn_origin === 'owner' && content(f.body).includes(owner)), 'interleaved canonical owner echo');
      assert(echo.body.index > first.body.index, 'owner must follow PRE');
      assert(!assistants().some(f => terminal(f.body)), 'first response ended before second owner echo');
      if (!main) {
        const identity = async () => {
          const records = JSON.parse(await fs.readFile(path.join(values.workdir, 'agents.json'), 'utf8'));
          assert(Array.isArray(records), 'registry snapshot shape');
          return records.find(a => a.name === name);
        };
        const running = async () => (await (await fetch(new URL('/api/agents', base))).json()).find(a => a.name === name)?.running;
        const before = await identity();
        assert(before?.session_id, 'the running aside must have a session identity');
        assert.equal(await running(), true, 'the aside must be running before cleanup');
        const queued = async () => {
          try {
            const queue = JSON.parse(await fs.readFile(path.join(values.workdir, 'sendq', `${name}.json`), 'utf8'));
            assert.equal(queue.agent, name);
            assert(Array.isArray(queue.entries));
            return queue.entries.find(entry => entry.text.includes(ack));
          } catch (error) {
            if (error.code === 'ENOENT') return undefined;
            throw error;
          }
        };
        const obligation = await until(queued, 'durable queued follow-up');
        assert(obligation.id, 'the queued request must have a durable identity');
        const logStart = (await fs.readFile(values['daemon-log'], 'utf8')).length;
        await until(async () => {
          const lines = (await fs.readFile(values['daemon-log'], 'utf8')).slice(logStart).split('\n');
          const members = (line, key) => {
            const field = line.match(new RegExp(`${key}=("[^\"]*"|\\[[^\\]]*\\])`))?.[1] || '';
            return field.replaceAll('"', '').replace(/^\[|\]$/g, '').split(' ');
          };
          for (const line of lines.filter(line => line.includes('idle thread sweep completed'))) {
            assert(!members(line, 'reaped').includes(name), 'cleanup stopped the active aside');
            if (['kept_unknown', 'kept_busy'].some(key => members(line, key).includes(name))) return true;
          }
          return false;
        }, 'actual periodic cleanup evaluated and kept the aside', sweepDeadline);
        const after = await identity();
        assert.equal(after?.session_id, before.session_id, 'cleanup must preserve session identity');
        assert.equal(await running(), true, 'cleanup must preserve the active process');
        assert.equal((await queued())?.id, obligation.id, 'cleanup must preserve the queued obligation');
        assert(!assistants().some(f => terminal(f.body)), 'tool hold must span the cleanup cycle');
        console.log(`PASS ${name}: actual periodic sweep retained the held turn and queued follow-up`);
      }
      await fs.writeFile(release, nonce);
      const later = await until(() => assistants().find(f => content(f.body).includes(post)), 'continuation POST');
      assert(later.body.index > echo.body.index, 'POST must have a new index below the owner');
      assert.notEqual(later.body.id, first.body.id, 'POST cannot rewrite the PRE bubble');
      assert(!content(later.body).includes(pre), 'POST cannot absorb the PRE bubble');
      await until(() => assistants().find(f => content(f.body).includes(ack) && terminal(f.body)), 'second request correlated terminal ACK');
      assert.equal(await fs.readFile(completed, 'utf8'), nonce);
      const assertPaint = async () => {
        await page.waitForFunction(({ transcript, pre, post, owner, ack }) => {
          const rows = [...document.querySelectorAll(`${transcript} [data-kind]`)];
          const find = (kind, text) => rows.findIndex(el => el.getAttribute('data-kind') === kind && el.querySelector('.msg-body')?.textContent.includes(text));
          const a = find('assistant', pre), u = find('user', owner), b = find('assistant', post), c = find('assistant', ack);
          return a >= 0 && u > a && b > u && c >= b && !rows[a].textContent.includes(post);
        }, { transcript, pre, post, owner, ack });
      };
      await assertPaint();
      checked.push({ name, start, pre, post, ack });
      await page.reload();
      await assertPaint();
      const screenshot = path.join(work, `${main ? 'main' : 'sidebar'}-reload.png`);
      await page.screenshot({ path: screenshot });
      console.log(`PASS ${name}: nonterminal ${first.body.id}, owner ${echo.body.id}, continuation ${later.body.id}; live and reload ordered. Screenshot: ${screenshot}`);
    } finally {
      await fs.writeFile(release, nonce);
    }
  }
  // A later notification or boot recovery can re-execute a completed request.
  // Count canonical IDs, not streaming snapshots of the same response.
  for (const { name, start, pre, post, ack } of checked) {
    const replies = new Map();
    for (const frame of frames.slice(start)) {
      if (frame.ch === `transcript:${name}` && frame.t === 'frame' && frame.body?.event?.type === 'assistant') replies.set(frame.body.id, content(frame.body));
    }
    for (const token of [pre, post, ack]) assert.equal([...replies.values()].filter(text => text.includes(token)).length, 1, `${name}: delayed duplicate reply ${token}`);
  }
  assert.deepEqual(errors, [], 'browser errors');
  console.log('PASS: packaged shared main/sidebar owner boundary with real provider tool interleaving');
}
main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => {
  for (const release of releases) await fs.writeFile(release, 'cleanup');
  if (asideCreated) await mcp('jevons_thread_remove', { id: values.aside }).catch(error => { console.error(error); process.exitCode = 1; });
  await browser?.close();
});
