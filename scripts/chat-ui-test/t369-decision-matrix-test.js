// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright real-render smoke for 🎯T369: an assistant A/B/C choice table
// paints as a selectable decision card the owner can click and send, while an
// ordinary comparison table stays a table. Hermetic static server; no daemon.
//
// The model is unit-covered in web/scripts/decision_matrix_test.js — this test
// exists because "the owner gets a UX to inspect and select" is a claim about
// a real render and real clicks, which a DOM-free helper cannot attest to.
//
//   node scripts/chat-ui-test/t369-decision-matrix-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');

const CHOICE_MD = [
  'Three ways to reach the control plane — pick one:',
  '',
  '| Option | Approach | Cost | Recommended |',
  '| --- | --- | --- | --- |',
  '| A | CLI-first entry | low | |',
  '| B | control-plane allowlist | medium | ✅ |',
  '| C | force-pause main session | low | |',
  '',
  'And an ordinary comparison, which must stay a table:',
  '',
  '| Approach | Cost |',
  '| --- | --- |',
  '| Polling | low |',
  '| Push | high |',
].join('\n');

const EXPECTED_REPLY =
  'Decision — Three ways to reach the control plane — pick one: '
  + '**B** — control-plane allowlist (your recommended option).';

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end('[]');
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't369-decision-matrix-test', ok: true }));
        return;
      }
      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` }));
    srv.on('error', reject);
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });

  const ready = () => page.waitForFunction(
    () => typeof window.addMsg === 'function'
      && typeof window.DecisionMatrix !== 'undefined'
      && !!window.marked,
    null, { timeout: 10000 });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await ready();
    await page.evaluate((k) => localStorage.removeItem(k),
      await page.evaluate(() => window.DecisionMatrix.STORAGE_KEY));

    await page.evaluate((md) => { window._t369 = window.addMsg('jevons', md); }, CHOICE_MD);

    // ── the choice table became a card; the plain table did not ──
    const shape = await page.evaluate(() => {
      const body = window._t369._body;
      const card = body.querySelector('.decision-matrix');
      const opts = card ? Array.from(card.querySelectorAll('.dm-option')) : [];
      return {
        cards: body.querySelectorAll('.decision-matrix').length,
        // Only the plain comparison table should remain outside a card.
        looseTables: Array.from(body.querySelectorAll('table'))
          .filter((t) => !t.closest('.decision-matrix')).length,
        keys: opts.map((b) => (b.querySelector('.dm-key') || {}).textContent),
        labels: opts.map((b) => (b.querySelector('.dm-label') || {}).textContent),
        recommended: opts.filter((b) => b.querySelector('.dm-rec'))
          .map((b) => b.getAttribute('data-option-key')),
        details: opts[0] ? Array.from(opts[0].querySelectorAll('.dm-cell')).map((s) => s.textContent) : [],
        rawHidden: !!(card && card.querySelector('.dm-raw').hidden),
        rawHasTable: !!(card && card.querySelector('.dm-raw table')),
        status: card ? card.querySelector('.dm-status').textContent : '',
        sendDisabled: card ? card.querySelector('.dm-send').disabled : null,
      };
    });
    if (shape.cards !== 1) failures.push('expected exactly 1 decision card, got ' + shape.cards);
    if (shape.looseTables !== 1) {
      failures.push('the plain comparison table must stay a table (loose tables: '
        + shape.looseTables + ')');
    }
    if (JSON.stringify(shape.keys) !== JSON.stringify(['A', 'B', 'C'])) {
      failures.push('option keys: ' + JSON.stringify(shape.keys));
    }
    if (shape.labels[1] !== 'control-plane allowlist') {
      failures.push('option labels: ' + JSON.stringify(shape.labels));
    }
    if (JSON.stringify(shape.recommended) !== JSON.stringify(['B'])) {
      failures.push('recommended chip should be on B alone, got ' + JSON.stringify(shape.recommended));
    }
    if (!shape.details.some((d) => /Cost\s*low/i.test(d))) {
      failures.push('non-chrome columns should survive as detail cells: ' + JSON.stringify(shape.details));
    }
    if (!shape.rawHidden || !shape.rawHasTable) {
      failures.push('source table must be kept but hidden (hidden=' + shape.rawHidden
        + ', present=' + shape.rawHasTable + ')');
    }
    if (shape.status !== 'Pick one of 3 options') failures.push('initial status: ' + shape.status);
    if (shape.sendDisabled !== true) failures.push('Send must be disabled before a pick');

    // The card must actually occupy the bubble, not collapse to a sliver.
    const box = await page.locator('.decision-matrix').first().boundingBox();
    if (!box || box.width < 200 || box.height < 100) {
      failures.push('decision card box looks wrong: ' + JSON.stringify(box));
    }
    const optBox = await page.locator('.dm-option[data-option-key="B"]').boundingBox();
    if (!optBox || optBox.height < 20 || optBox.width < 150) {
      failures.push('option row box looks wrong: ' + JSON.stringify(optBox));
    }

    // ── click B: selected, durable, Send enabled ──
    await page.click('.dm-option[data-option-key="B"]');
    const picked = await page.evaluate(() => {
      const card = window._t369._body.querySelector('.decision-matrix');
      const store = window.DecisionMatrix.parseStore(
        localStorage.getItem(window.DecisionMatrix.STORAGE_KEY));
      return {
        checked: Array.from(card.querySelectorAll('.dm-option'))
          .filter((b) => b.getAttribute('aria-checked') === 'true')
          .map((b) => b.getAttribute('data-option-key')),
        status: card.querySelector('.dm-status').textContent,
        sendDisabled: card.querySelector('.dm-send').disabled,
        stored: window.DecisionMatrix.choiceFor(store, card.getAttribute('data-matrix-id')),
      };
    });
    if (JSON.stringify(picked.checked) !== JSON.stringify(['B'])) {
      failures.push('exactly B should read as checked, got ' + JSON.stringify(picked.checked));
    }
    if (picked.status !== 'Selected B — control-plane allowlist · not sent yet') {
      failures.push('status after pick: ' + picked.status);
    }
    if (picked.sendDisabled) failures.push('Send should be enabled after a pick');
    if (!picked.stored || picked.stored.key !== 'B' || picked.stored.sent) {
      failures.push('selection must be recorded durably: ' + JSON.stringify(picked.stored));
    }

    // ── Send choice: composer carries an agent-actionable reply ──
    await page.evaluate(() => {
      window.__sent = [];
      window.__realSend = window.send;
      window.send = function () { window.__sent.push(document.getElementById('input').value); };
    });
    await page.click('.decision-matrix .dm-send');
    const sent = await page.evaluate(() => {
      const card = window._t369._body.querySelector('.decision-matrix');
      window.send = window.__realSend;
      return {
        wire: window.__sent.slice(),
        status: card.querySelector('.dm-status').textContent,
        sendDisabled: card.querySelector('.dm-send').disabled,
      };
    });
    if (sent.wire.length !== 1) failures.push('Send choice should send exactly once: ' + JSON.stringify(sent.wire));
    if (sent.wire[0] !== EXPECTED_REPLY) {
      failures.push('reply text mismatch:\n    got:  ' + sent.wire[0] + '\n    want: ' + EXPECTED_REPLY);
    }
    if (!/· sent$/.test(sent.status)) failures.push('status after send: ' + sent.status);
    if (!sent.sendDisabled) failures.push('Send should be spent after sending (no double-send)');

    // ── Table toggle reveals the source table ──
    await page.click('.decision-matrix .dm-table-toggle');
    const revealed = await page.evaluate(() => {
      const raw = window._t369._body.querySelector('.decision-matrix .dm-raw');
      return { hidden: raw.hidden, rows: raw.querySelectorAll('tbody tr').length };
    });
    if (revealed.hidden || revealed.rows !== 3) {
      failures.push('Table toggle should reveal the 3-row source table: ' + JSON.stringify(revealed));
    }

    // ── durability: reload, re-render the same matrix, selection survives ──
    await page.reload({ waitUntil: 'domcontentloaded' });
    await ready();
    await page.evaluate((md) => { window._t369b = window.addMsg('jevons', md); }, CHOICE_MD);
    const after = await page.evaluate(() => {
      const card = window._t369b._body.querySelector('.decision-matrix');
      return {
        checked: Array.from(card.querySelectorAll('.dm-option'))
          .filter((b) => b.getAttribute('aria-checked') === 'true')
          .map((b) => b.getAttribute('data-option-key')),
        status: card.querySelector('.dm-status').textContent,
      };
    });
    if (JSON.stringify(after.checked) !== JSON.stringify(['B'])) {
      failures.push('selection must survive reload + re-render, got ' + JSON.stringify(after.checked));
    }
    if (!/· sent$/.test(after.status)) failures.push('status after reload: ' + after.status);

    if (failures.length) {
      console.error('FAIL t369-decision-matrix-test');
      failures.forEach((f) => console.error('  -', f));
      process.exitCode = 1;
    } else {
      console.log('PASS t369-decision-matrix-test (A/B/C card; pick, send, durable; plain table untouched)');
    }
  } catch (e) {
    console.error('FAIL t369-decision-matrix-test', (e && e.stack) || e);
    process.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
})();
