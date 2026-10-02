// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Explicit development DOM probe; reads real APIs, selects seats, sends no chat.
// UI_HOST=http://127.0.0.1:13705 node scripts/chat-ui-test/t996-selected-seat-frontier-test.js
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || '../browser-loop-test/node_modules/playwright');
const host = process.env.UI_HOST;
if (!host) throw new Error('Set UI_HOST explicitly to the development surface to verify');
const out = process.env.ARTIFACT_DIR || '/tmp/t996-frontier';
fs.mkdirSync(out, { recursive: true });
(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const agents = await (await page.request.get(host + '/api/agents')).json();
    const names = (process.env.SEATS || 'claudia-po,bullseye-po').split(',');
    const seats = names.map(name => {
      const seat = agents.find(a => a.name === name);
      assert.ok(seat?.workdir, 'seat has workdir: ' + name);
      return seat;
    });
    assert.notEqual(seats[0].workdir, seats[1].workdir);
    await page.goto(host + '/?tab=frontier');
    await page.waitForSelector('.agent-node');
    assert.equal(await page.locator('#frontier-table tr').count(), 0, 'no default frontier');
    for (const seat of seats) {
      const expected = await (await page.request.get(host + '/api/frontier?cwd=' + encodeURIComponent(seat.workdir))).json();
      assert.equal(expected.available, true, 'repo frontier available');
      assert.ok(expected.targets.length, 'use a repo with targets to prove selection');
      const request = page.waitForResponse(r => new URL(r.url()).pathname === '/api/frontier' && new URL(r.url()).searchParams.get('cwd') === seat.workdir);
      await page.locator('.agent-name').filter({ hasText: new RegExp('^' + seat.name + '$') }).click();
      await request;
      const expectedNames = expected.targets.map(t => [t.id, t.name.length > 72 ? t.name.slice(0, 71) + '…' : t.name]);
      await page.waitForFunction(expectedRows => {
        const rows = Array.from(document.querySelectorAll('#frontier-table tr'));
        return rows.length === expectedRows.length && expectedRows.every(([id, name]) => rows.some(r => r.dataset.targetId === id && r.querySelector('.ft-name').textContent === name));
      }, expectedNames);
      await page.screenshot({ path: path.join(out, seat.name + '.png') });
      console.log('PASS selected ' + seat.name + ': ' + expected.targets.length + ' repo targets, ledger=' + expected.ledger_key);
    }
    await page.locator('.agent-name').filter({ hasText: new RegExp('^' + seats[1].name + '$') }).click();
    await page.waitForFunction(() => document.querySelectorAll('#frontier-table tr').length === 0 && document.querySelectorAll('.agent-node.selected').length === 0);
    await page.reload();
    await page.waitForSelector('.agent-node');
    assert.equal(await page.locator('#frontier-table tr').count(), 0, 'deselection survives reload');
    await page.screenshot({ path: path.join(out, 'deselected.png') });
    console.log('PASS deselected: empty frontier, including hard reload');
  } finally { await browser.close(); }
})().catch(err => { console.error(err); process.exitCode = 1; });
