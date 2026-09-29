// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T789.1: real provider, packaged React, isolate only. Builds a transcript
// taller than 12,000 px with real turns, then sends while the seat is busy
// (escalated since 🎯T903: steered, with a countdown) and cuts in. The
// pane must end within one row of the bottom with no 'Latest' button — the
// 2026-09-22 observation was 12,180 px above the bottom after this sequence.
const assert = require('node:assert/strict');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('../browser-loop-test/node_modules/playwright');
const { values } = parseArgs({ options: {
  host: { type: 'string' }, provider: { type: 'string' }, workdir: { type: 'string' }, aside: { type: 'string' },
  screenshot: { type: 'string' },
} });
const base = new URL(`http://${values.host}`);
assert(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
assert(base.port && !['13705', '13706'].includes(base.port), 'isolate only');
const TALL_PX = 12000;
const ONE_ROW_PX = 80;
const LONG = n => `Write ${n} numbered lines. Each line is one distinct sentence of about fifteen words on a different topic. No tools, no preamble.`;

let browser;
async function main() {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(180000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('https://fonts.**/*', r => r.abort());
  await page.goto(base.href);
  await page.locator('#input').waitFor();
  const gap = () => page.locator('#messages').evaluate(el => ({ fromBottom: Math.round(el.scrollHeight - el.scrollTop - el.clientHeight), scrollHeight: el.scrollHeight, clientHeight: el.clientHeight }));

  // Grow the transcript with real turns until it is taller than TALL_PX.
  for (let turn = 0; turn < 12; turn++) {
    const before = await page.locator('#messages [data-kind="assistant"]').count();
    await page.locator('#input').fill(LONG(150));
    await page.locator('#input').press('Enter');
    // A loaded host can miss a turn; the transcript only needs to get tall.
    const answered = await page.waitForFunction(n => document.querySelectorAll('#messages [data-kind="assistant"]').length > n, before, { timeout: 120000 }).then(() => true, () => false);
    if (!answered) { console.log(`turn ${turn}: no reply within 120s, moving on`); continue; }
    await page.waitForTimeout(500);
    // Wait for the answer to stop growing.
    let last = -1;
    for (let i = 0; i < 240; i++) {
      const g = await gap();
      if (g.scrollHeight === last) break;
      last = g.scrollHeight;
      await page.waitForTimeout(1500);
    }
    const g = await gap();
    console.log(`turn ${turn}: ${JSON.stringify(g)}`);
    if (g.scrollHeight > TALL_PX + 1000) break;
  }
  const tall = await gap();
  assert(tall.scrollHeight > TALL_PX, `transcript is taller than ${TALL_PX}px: ${JSON.stringify(tall)}`);
  assert(tall.fromBottom <= ONE_ROW_PX, `pinned before the send: ${JSON.stringify(tall)}`);

  // Busy seat: start a long answer, then send two more while it runs. Since
  // 🎯T903 a plain Enter to the busy overseer escalates — steered into the
  // turn, the pane counting down to the interrupt — instead of waiting in the
  // queue strip, and Cut in is the ⌘⇧Enter chord.
  await page.locator('#input').fill(LONG(200));
  await page.locator('#input').press('Enter');
  await page.waitForTimeout(3000);
  const held = [`held-${Date.now()}-a`, `held-${Date.now()}-b`];
  for (const text of held) {
    await page.locator('#input').fill(`Reply with exactly: ${text}`);
    await page.locator('#input').press('Enter');
    await page.waitForTimeout(300);
  }
  await page.locator('.escalation-strip').first().waitFor();
  const queued = await gap();
  await page.locator('#input').fill(`Reply with exactly: cut-${held[0]}`);
  await page.locator('#input').press('Meta+Shift+Enter');
  await page.waitForFunction(text => [...document.querySelectorAll('#messages [data-kind="user"] .msg-body')].some(e => e.textContent.includes(text)), held[0]);
  const samples = [];
  for (let i = 0; i < 12; i++) { await page.waitForTimeout(2500); samples.push(await gap()); }
  const latestShown = await page.locator('#jump-bottom').isVisible();
  const shot = values.screenshot || path.join(values.workdir || '.', 't789-live.png');
  await page.screenshot({ path: shot });
  console.log(`tall ${JSON.stringify(tall)}; queued ${JSON.stringify(queued)}; samples ${JSON.stringify(samples.map(s => s.fromBottom))}; final ${JSON.stringify(samples.at(-1))}; latest visible=${latestShown}; screenshot ${shot}`);
  assert(queued.fromBottom <= ONE_ROW_PX, `pinned with the messages queued: ${JSON.stringify(queued)}`);
  for (const s of samples) assert(s.fromBottom <= ONE_ROW_PX, `pinned after Cut in: ${JSON.stringify(samples)}`);
  assert.equal(latestShown, false, "'Latest' must not be showing");
  assert.deepEqual(errors, []);
  console.log('PASS: T789.1 real composer send and Cut in on a busy seat stays pinned over a transcript taller than 12,000 px');
}
main().catch(e => { console.error(e); process.exitCode = 1; }).finally(() => browser?.close());
