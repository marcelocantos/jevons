// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
// Transport-only journey exception: real isolated HTTP/mux + tracked React bundle;
// controlled readings do NOT prove provider collection reliability or incident cause.
const assert = require('node:assert/strict');
const { chromium } = require('./playwright.cjs')();
const base = process.env.T1224_URL;
if (!base) throw new Error('T1224_URL required');
async function main() {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    page.setDefaultTimeout(12000);
    await page.route('https://fonts.**/*', r => r.abort());
    let navigations = 0;
    let forcedReads = 0;
    page.on('request', req => { if (req.url().includes('/api/plan-usage?refresh=')) forcedReads++; });
    page.on('framenavigated', frame => { if (frame === page.mainFrame()) navigations++; });
    let sockets = 0;
    let blackhole = false;
    let resubscribed = false;
    let replacementUsageFrame = false;
    await page.routeWebSocket('**/ws/mux', socket => {
      sockets++;
      const n = sockets;
      const server = socket.connectToServer();
      socket.onMessage(message => {
        if (n > 1 && String(message).includes('"ch":"plan-usage"') && String(message).includes('"t":"open"')) resubscribed = true;
        server.send(message);
      });
      server.onMessage(message => {
        if (n > 1) {
          const frame = JSON.parse(String(message));
          if (frame.ch === 'plan-usage' && frame.t === 'frame' &&
              frame.body?.backends?.some(b => b.provider === 'claude' &&
                b.windows?.some(w => w.remaining_percent === 24))) {
            replacementUsageFrame = true;
          }
        }
        if (!(blackhole && n === 1)) socket.send(message);
      });
    });
    const api = (p, opts) => page.request.fetch(base + p, opts).then(async r => r.json().catch(() => null));
    await page.goto(base);
    await page.waitForSelector('[data-provider="claude"] .plan-bar-fill');
    const baselineNavigations = navigations;
    assert(baselineNavigations >= 1, 'initial navigation must have completed');
    await api('/fixture/state', { method: 'POST' });
    await page.waitForSelector('[data-provider="claude"].plan-stale .plan-bar-fill');
    assert.match(await page.locator('#plan-ticker').innerText(), /last known/);
    // T681: tooltip keeps the old reading as explicitly aged, never green current.
    await page.locator('#plan-ticker').hover();
    await page.waitForFunction(() => /no reading.*meter 429.*last .*ago.*59%/s.test(document.body.textContent));
    await page.screenshot({ path: '/tmp/t1224-usage-stale.png' });
    await page.evaluate(() => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
      for (let i = 0; i < 5; i++) document.dispatchEvent(new Event('visibilitychange'));
    });
    assert.equal(forcedReads, 0, 'visibility must not trigger collection');
    await api('/fixture/release', { method: 'POST' });
    await page.waitForFunction(() => document.querySelector('[data-provider="claude"] .plan-bar-fill')?.style.width === '67%');
    blackhole = true;
    // A different cached source value is fanned while the first server→client
    // leg is silent. Only the replacement socket's actual open can paint it.
    await api('/fixture/reconnect-value', { method: 'POST' });
    assert.equal(await page.locator('[data-provider="claude"] .plan-bar-fill').evaluate(el => el.style.width), '67%', 'blackholed socket must not paint new value');
    // The first socket still looks OPEN to browser; watchdog must fence it and resubscribe.
    const deadline = Date.now() + 45000;
    while ((!resubscribed || !replacementUsageFrame || sockets < 2) && Date.now() < deadline) await new Promise(r => setTimeout(r, 100));
    assert(sockets >= 2 && resubscribed, `blackhole recovery sockets=${sockets} subscribed=${resubscribed}`);
    assert(replacementUsageFrame, 'replacement socket must receive the distinct 24%-remaining mux frame');
    await page.waitForFunction(() => document.querySelector('[data-provider="claude"] .plan-bar-fill')?.style.width === '76%');
    assert.equal(navigations, baselineNavigations, 'recovery must repaint without main-frame reload');
    assert.equal(forcedReads, 0, 'recovery must use the subscription');
    await page.screenshot({ path: '/tmp/t1224-usage-recovered.png' });
    console.log('PASS T1224 real sqldeep/HTTP/mux transport isolate');
  } finally { await browser.close(); }
}
main().catch(err => { console.error(err); process.exitCode = 1; });
