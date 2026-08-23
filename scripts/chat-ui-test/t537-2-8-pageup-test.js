// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T537.2.8: PageUp on the React cockpit scrolls #messages by ~0.8 viewport
// and pin-to-end does not snap back.

'use strict';

const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const NEW_UI = process.env.JEVONS_NEW_UI_URL || 'http://127.0.0.1:5173/';
const FROZEN_NOW = 1_700_000_000_000;

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
    colorScheme: 'dark',
  });
  await context.addCookies([
    { name: 'theme', value: 'dark', url: NEW_UI.replace(/\/$/, '') + '/' },
  ]);
  await context.addInitScript((now) => {
    window.__JEVONS_CLOCK_NOW = now;
    window.__JEVONS_PIXEL_FIXTURE = true;
    try {
      localStorage.setItem(
        'jevons-rhs-layout-v1',
        JSON.stringify({ sidebarWidth: 458, fleetFraction: 0.448 }),
      );
    } catch (_) { /* ignore */ }
  }, FROZEN_NOW);
  const page = await context.newPage();
  await page.goto(NEW_UI, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await page.waitForSelector('#messages', { timeout: 15000 });
  await page.waitForTimeout(500);
  const input = page.locator('#input');
  if (await input.count()) await input.focus();

  const probe = await page.evaluate(() => {
    const msgs = document.getElementById('messages');
    const fake = document.querySelector('.agent-transcript');
    const wrap = document.querySelector('.agent-interaction');
    return {
      messages: !!msgs,
      fakeTranscript: !!fake,
      fakeWrap: !!wrap,
      sh: msgs ? msgs.scrollHeight : 0,
      ch: msgs ? msgs.clientHeight : 0,
      st: msgs ? msgs.scrollTop : -1,
      overflow: msgs ? msgs.scrollHeight - msgs.clientHeight : 0,
    };
  });
  console.log('probe', JSON.stringify(probe));
  if (!probe.messages) {
    console.log('FAIL no #messages');
    await browser.close();
    process.exit(1);
  }
  if (probe.overflow < 8) {
    console.log('FAIL transcript does not overflow, PageUp cannot move');
    await browser.close();
    process.exit(1);
  }

  const before = probe.st;
  await page.keyboard.press('PageUp');
  await page.waitForTimeout(50);
  const afterUp = await page.evaluate(() => {
    const m = document.getElementById('messages');
    return { st: m.scrollTop, ch: m.clientHeight };
  });
  await page.waitForTimeout(200);
  const stayed = await page.evaluate(() => document.getElementById('messages').scrollTop);
  const step = Math.round(afterUp.ch * 0.8);
  const expected = Math.max(0, before - step);
  console.log('pageup', JSON.stringify({ before, after: afterUp.st, stayed, step, expected }));

  const fail = [];
  if (afterUp.st >= before) fail.push('PageUp did not decrease scrollTop (' + before + ' → ' + afterUp.st + ')');
  if (Math.abs(afterUp.st - expected) > 2) {
    fail.push('PageUp delta not ~0.8 viewport (got ' + afterUp.st + ' want ' + expected + ')');
  }
  if (stayed !== afterUp.st) fail.push('pin-to-end snapped back after PageUp (' + afterUp.st + ' → ' + stayed + ')');

  await page.keyboard.press('PageDown');
  await page.waitForTimeout(50);
  const afterDown = await page.evaluate(() => document.getElementById('messages').scrollTop);
  console.log('pagedown', JSON.stringify({ from: stayed, to: afterDown }));
  if (afterDown <= stayed) fail.push('PageDown did not increase scrollTop (' + stayed + ' → ' + afterDown + ')');

  await browser.close();
  if (fail.length) {
    for (const f of fail) console.log('FAIL ' + f);
    process.exit(1);
  }
  console.log('PASS PageUp/PageDown scroll #messages 0.8 viewport and hold');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
