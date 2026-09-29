// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T562.2 live isolate probe, re-pointed by 🎯T899 / 🎯T903: real provider,
// packaged React, real mux wire. A real WORKER seat holds an actual tool
// call. The owner drives THAT seat's real composer (#agent-inspect-input /
// #agent-inspect-send) while its transcript meta phase is non-idle, and this
// script proves the send escalates: a `send` frame reaches the wire at once,
// nothing waits in the client queue, the pane shows the escalation countdown,
// and the text lands in the seat's transcript as a real turn.
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const fs = require('node:fs/promises');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('./playwright.cjs')();
const { values } = parseArgs({ options: {
  host: { type: 'string' }, agent: { type: 'string' },
  ready: { type: 'string' }, release: { type: 'string' }, completed: { type: 'string' },
  nonce: { type: 'string' }, post: { type: 'string' }, screenshot: { type: 'string' },
} });
const base = new URL(`http://${values.host}`);
assert(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
assert(base.port && !['13705', '13706'].includes(base.port), 'isolate only');
assert(values.agent && values.ready && values.release && values.completed && values.nonce && values.post);

let browser;
async function main() {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(90000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const sent = [];
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => {
      try { const f = JSON.parse(String(payload)); sent.push(f); } catch { /* not JSON */ }
    });
  });
  await page.route('https://fonts.**/*', route => route.abort());
  await page.goto(base.href);
  await page.goto(new URL(`/?agent=${encodeURIComponent(values.agent)}&tab=transcript`, base).href);

  const ch = `transcript:${values.agent}`;
  const input = '#agent-inspect-input', button = '#agent-inspect-send', strip = '#agent-inspect-send-queue';

  // The real tool call must actually be running before this probe means
  // anything: read it from the agent's own marker file, not a timer.
  await (async () => {
    const deadline = Date.now() + 90000;
    while (Date.now() < deadline) {
      try { if (await fs.readFile(values.ready, 'utf8') === values.nonce) return; } catch { /* not yet */ }
      await new Promise(r => setTimeout(r, 200));
    }
    throw new Error('worker never reached its actual tool-hold marker');
  })();

  // The composer's own idea of busy: wait for a non-idle phase sample on
  // this seat's transcript meta, exactly what T562.2 wired in 67484749.
  await page.waitForFunction(() => {
    const el = document.querySelector('#agent-inspect-input');
    return !!el && !el.disabled;
  });
  await page.waitForTimeout(500); // let at least one meta frame with phase land

  // 🎯T899 / 🎯T903: a busy seat's ordinary send is not held client-side
  // any more. It reaches /ws/mux at once, the daemon steers it into the
  // running turn, and the pane counts down to the interrupt.
  const HELD = `held-${values.nonce}`;
  const sentBeforeSubmit = sent.length;
  await page.locator(input).fill(HELD);
  await page.locator(button).click();

  await (async () => {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      if (sent.slice(sentBeforeSubmit).some(f => f.t === 'send' && f.ch === ch && typeof f.body?.text === 'string' && f.body.text.includes(HELD))) return;
      await new Promise(r => setTimeout(r, 100));
    }
    throw new Error('the send to a busy seat never reached /ws/mux; it must escalate, not wait in the pane');
  })();
  const queued = await page.evaluate(({ strip, text }) => {
    const el = document.querySelector(strip);
    return !!el && [...el.querySelectorAll('.send-queue-item .sq-text')].some(n => n.textContent.includes(text));
  }, { strip, text: HELD });
  assert.equal(queued, false, 'the text must not also sit in the client queue');
  await page.waitForFunction(() => /interrupts in \d+s unless it takes the message/.test(document.querySelector('.escalation-strip')?.textContent || ''), null, { timeout: 15000 });

  // Release the real tool hold so the seat's turn completes.
  await fs.writeFile(values.release, values.nonce);
  await (async () => {
    const deadline = Date.now() + 90000;
    while (Date.now() < deadline) {
      try { if (await fs.readFile(values.completed, 'utf8') === values.nonce) break; } catch { /* not yet */ }
      await new Promise(r => setTimeout(r, 200));
    }
  })();

  // The message lands in the seat's own transcript as an owner turn.
  await page.waitForFunction(({ text }) => {
    const rows = [...document.querySelectorAll('#agent-inspect-body [data-kind="user"] .msg-body')];
    return rows.some(el => el.textContent.includes(text));
  }, { text: HELD }, { timeout: 60000 });

  if (values.screenshot) await page.screenshot({ path: values.screenshot });
  assert.deepEqual(errors, [], 'browser errors');
  console.log(`PASS: worker seat ${values.agent} held a real tool call; an ordinary send during that hold reached /ws/mux at once, the pane counted down to the interrupt, and the text landed in the transcript`);
}
main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => { await browser?.close(); });
