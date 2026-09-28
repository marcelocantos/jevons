// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T540.7.1.1: READ-ONLY observation of the MAIN view on the development
// surface. Nothing is typed or sent. Pages older history into #messages until
// an existing assistant / owner / assistant run is painted, asserts DOM order,
// hard-reloads, pages back, and asserts the same order. Screenshots per pass.
const assert = require('node:assert/strict');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { chromium } = require('./playwright.cjs')();
const { values } = parseArgs({ options: {
  host: { type: 'string', default: 'localhost:13705' },
  a: { type: 'string' }, owner: { type: 'string' }, b: { type: 'string' },
  out: { type: 'string' },
} });
assert(values.a && values.owner && values.b && values.out, '--a --owner --b --out required');
const base = new URL(`http://${values.host}`);
assert(['localhost', '127.0.0.1'].includes(base.hostname));

// The listener must exist before navigation: the mux socket opens during load.
function watchArrival(page) {
  const seen = { arrived: false };
  // Only a history page that carries the owner-origin event counts: a live frame
  // that merely quotes the text (a brief, a notification) must not stop paging.
  page.on('websocket', ws => ws.on('framereceived', f => {
    const raw = String(f.payload);
    if (!raw.includes(values.owner)) return;
    let m;
    try { m = JSON.parse(raw); } catch { return; }
    if (m.ch !== 'transcript:jevons' || m.t !== 'page') return;
    if ((m.body?.lines || []).some(l => l.event?.turn_origin === 'owner' && JSON.stringify(l.event).includes(values.owner))) seen.arrived = true;
  }));
  return seen;
}

async function pageUntilOrdered(page, seen) {
  const probe = ({ a, owner, b }) => {
    const rows = [...document.querySelectorAll('#messages [data-kind]')];
    const idx = (kind, text) => rows.findIndex(el => el.getAttribute('data-kind') === kind && (el.querySelector('.msg-body')?.textContent || '').includes(text));
    const ia = idx('assistant', a), iu = idx('user', owner), ib = idx('assistant', b);
    return { rows: rows.length, ia, iu, ib, ok: ia >= 0 && iu > ia && ib > iu };
  };
  // Older pages arrive over /ws/mux. Stop paging the moment the owner text
  // has arrived, so the run is still near the top of the window when probed.
  const deadline = Date.now() + 720000;
  let last;
  try {
    while (Date.now() < deadline) {
      if (seen.arrived) {
        const user = page.locator('#messages [data-kind="user"]', { hasText: values.owner }).first();
        if (await user.count()) {
          await user.scrollIntoViewIfNeeded();
          await page.waitForTimeout(500);
          last = await page.evaluate(probe, values);
          if (last.ok) return last;
        }
      }
      // A scroll event only fires on change: nudge off the top, then back, so each older page is requested.
      if (!seen.arrived) {
        await page.evaluate(() => { const m = document.querySelector('#messages'); m.scrollTop = 400; });
        await page.waitForTimeout(150);
        await page.evaluate(() => { const m = document.querySelector('#messages'); m.scrollTop = 0; });
      }
      await page.waitForTimeout(1000);
    }
  } finally { /* listener lives with the page */ }
  throw new Error(`ordered run never painted: arrived=${seen.arrived} ${JSON.stringify(last)}`);
}

(async () => {
  const browser = await chromium.launch();
  try {
    const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage();
    const seen = watchArrival(page);
    await page.goto(base.href, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#messages [data-kind]');
    const first = await pageUntilOrdered(page, seen);
    await page.locator(`#messages [data-kind="user"]:has-text("${values.owner}")`).first().scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(values.out, 'main-first-load.png') });
    console.log('FIRST', JSON.stringify(first));
    seen.arrived = false;
    await page.reload({ waitUntil: 'domcontentloaded' }); // hard reload: new document, no in-memory state
    await page.waitForSelector('#messages [data-kind]');
    const second = await pageUntilOrdered(page, seen);
    await page.locator(`#messages [data-kind="user"]:has-text("${values.owner}")`).first().scrollIntoViewIfNeeded();
    await page.screenshot({ path: path.join(values.out, 'main-after-reload.png') });
    console.log('RELOAD', JSON.stringify(second));
    console.log('PASS: main view order assistant, user, assistant on', base.host, 'first load and after hard reload');
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exit(1); });
