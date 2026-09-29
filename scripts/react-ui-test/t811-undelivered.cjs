// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T811: isolate only. The daemon refuses the first owner delivery with the
// broker's real not_owner text (fault seam file). The cockpit must paint
// "not delivered: <reason>" with Resend in the owner's own row; Resend must
// deliver the SAME message once (one owner row, one reply).
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('../browser-loop-test/node_modules/playwright');
const { values } = parseArgs({ options: {
  host: { type: 'string' }, provider: { type: 'string' }, workdir: { type: 'string' }, aside: { type: 'string' },
  fault: { type: 'string' }, screenshot: { type: 'string' }, 'overseer-provider': { type: 'string' },
} });
const base = new URL(`http://${values.host}`);
assert(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
assert(base.port && !['13705', '13706'].includes(base.port), 'isolate only');
assert(values.fault, '--fault <state-dir>/fault-owner-not-owner is required');

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
  const token = `t811-${Date.now()}`;
  fs.writeFileSync(values.fault, '1');
  await page.locator('#input').fill(`Reply with exactly: ${token}`);
  await page.locator('#input').press('Enter');

  const row = page.locator(`#messages [data-kind="user"]:has-text("${token}")`);
  const state = row.locator('.msg-delivery[data-state="undelivered"]');
  await state.waitFor();
  assert.match(await state.textContent(), /not delivered: .*not_owner/);
  const shot = values.screenshot || path.join(values.workdir || '.', 't811-undelivered.png');
  await page.screenshot({ path: shot });
  assert.equal(await page.locator(`#messages [data-kind="assistant"]:has-text("${token}")`).count(), 0, 'no reply before delivery');

  await row.locator('.msg-resend').click();
  await row.locator('.msg-delivery[data-state="delivered"]').waitFor();
  await page.locator(`#messages [data-kind="assistant"]:has-text("${token}")`).first().waitFor();
  await page.waitForTimeout(1500);
  assert.equal(await page.locator(`#messages [data-kind="user"]:has-text("${token}")`).count(), 1, 'one owner row');
  assert.equal(await page.locator(`#messages [data-kind="assistant"]:has-text("${token}")`).count(), 1, 'one reply: delivered once');
  assert.equal(await page.locator('.msg-resend').count(), 0, 'Resend is gone once delivered');
  assert.deepEqual(errors, []);
  console.log(`PASS: T811 not_owner refusal painted undelivered with Resend; Resend delivered once; screenshot ${shot}`);
}
main().catch(e => { console.error(e); process.exitCode = 1; }).finally(() => browser?.close());
