// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T895 (🎯T562.5) live isolate probe: real provider, packaged React, real
// mux wire. Drives the real composer (#agent-inspect-input /
// #agent-inspect-send) and asserts the per-send correlated outcome landed
// on the wire, not just in the hermetic Go/vitest suites cited by fd8f384c:
//   1. ack — the send frame the composer puts on /ws/mux carries a
//      client-generated id; the daemon's status reply echoes that same id;
//      the composer draft clears only once that id-matched status arrives.
//   2. definite failure — a send to an unregistered agent name gets an error
//      reply echoing the same id, and the composer leaves the draft
//      untouched (no silent swallow of the owner's text).
const assert = require('node:assert/strict');
const { parseArgs } = require('node:util');
const { chromium } = require('./playwright.cjs')();
const { values } = parseArgs({ options: {
  host: { type: 'string' }, agent: { type: 'string' }, screenshot: { type: 'string' },
} });
const base = new URL(`http://${values.host}`);
assert(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
assert(base.port && !['13705', '13706'].includes(base.port), 'isolate only');
assert(values.agent);
// Never registered on this isolate daemon — the definite-failure half sends
// to this name directly, no kill/deregister choreography required.
const unregistered = 'j36-unregistered-' + Date.now();

let browser;
async function main() {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(90000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const sent = [];
  const received = [];
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => {
      try { const f = JSON.parse(String(payload)); sent.push(f); } catch { /* not JSON */ }
    });
    socket.on('framereceived', ({ payload }) => {
      try { const f = JSON.parse(String(payload)); received.push(f); } catch { /* not JSON */ }
    });
  });
  await page.route('https://fonts.**/*', route => route.abort());
  await page.goto(base.href);

  const input = '#agent-inspect-input', button = '#agent-inspect-send';

  // --- Phase 1: ack — real send, real id-matched status reply, draft clears. ---
  await page.goto(new URL(`/?agent=${encodeURIComponent(values.agent)}&tab=transcript`, base).href);
  await page.waitForFunction(() => {
    const el = document.querySelector('#agent-inspect-input');
    return !!el && !el.disabled;
  });

  const ch1 = `transcript:${values.agent}`;
  const ACK_TEXT = 'ack-probe-' + Date.now();
  const sentBefore = sent.length;
  await page.locator(input).fill(ACK_TEXT);
  await page.locator(button).click();

  // The frame the composer actually put on /ws/mux for this text.
  let ackID;
  await (async () => {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      const f = sent.slice(sentBefore).find(f => f.t === 'send' && f.ch === ch1 && typeof f.body?.text === 'string' && f.body.text.includes(ACK_TEXT));
      if (f) { ackID = f.body.id; break; }
      await new Promise(r => setTimeout(r, 100));
    }
  })();
  assert.ok(ackID, 'composer send frame on the wire must carry a client-generated id');

  // The daemon's status (ack) reply must echo that exact id.
  await (async () => {
    const deadline = Date.now() + 30000;
    while (Date.now() < deadline) {
      const f = received.find(f => f.t === 'status' && f.ch === ch1 && f.body?.id === ackID);
      if (f) return;
      await new Promise(r => setTimeout(r, 100));
    }
    throw new Error('status (ack) reply never echoed the send id ' + ackID);
  })();

  // The draft clears only once that id-matched status arrived.
  await page.waitForFunction(({ sel }) => {
    const el = document.querySelector(sel);
    return !!el && el.value === '';
  }, { sel: input }, { timeout: 15000 });

  // --- Phase 2: definite failure — a real send to a name the daemon never registered. ---
  await page.goto(new URL(`/?agent=${encodeURIComponent(unregistered)}&tab=transcript`, base).href);
  await page.waitForFunction(() => {
    const el = document.querySelector('#agent-inspect-input');
    return !!el && !el.disabled;
  });

  const ch2 = `transcript:${unregistered}`;
  const FAIL_TEXT = 'fail-probe-' + Date.now();
  const sentBefore2 = sent.length;
  await page.locator(input).fill(FAIL_TEXT);
  await page.locator(button).click();

  let failID;
  await (async () => {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      const f = sent.slice(sentBefore2).find(f => f.t === 'send' && f.ch === ch2 && typeof f.body?.text === 'string' && f.body.text.includes(FAIL_TEXT));
      if (f) { failID = f.body.id; break; }
      await new Promise(r => setTimeout(r, 100));
    }
  })();
  assert.ok(failID, 'second composer send frame must also carry a client-generated id');
  assert.notEqual(failID, ackID, 'each send gets its own correlation id');

  await (async () => {
    const deadline = Date.now() + 30000;
    while (Date.now() < deadline) {
      const f = received.find(f => f.t === 'error' && f.ch === ch2 && f.body?.id === failID);
      if (f) return;
      await new Promise(r => setTimeout(r, 100));
    }
    throw new Error('error (definite failure) reply never echoed the send id ' + failID + ' for a send to an unregistered agent name');
  })();

  // Give the composer's frame handler a moment, then assert the draft was
  // NOT cleared — a definite failure must never silently swallow the text.
  await page.waitForTimeout(500);
  const draftVal = await page.locator(input).inputValue();
  assert.equal(draftVal, FAIL_TEXT, 'a definite-failure send must leave the composer draft untouched');

  if (values.screenshot) await page.screenshot({ path: values.screenshot });
  assert.deepEqual(errors, [], 'browser errors');
  console.log(`PASS: real composer send to ${values.agent} got an id-matched status (ack) and cleared the draft; a send to unregistered name ${unregistered} got an id-matched error (definite failure) and left the draft untouched`);
}
main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => { await browser?.close(); });
