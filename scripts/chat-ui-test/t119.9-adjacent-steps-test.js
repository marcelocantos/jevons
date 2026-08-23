// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Live-path perceptual test for 🎯T119.9: consecutive tool bursts with
// parked assistant text (growAssistant skips turn-slots) must not mint
// two adjacent ⋯ N steps capsules. Drives window.handle — the product
// ingest, not displayFromEvents reload.
//
//   node scripts/chat-ui-test/t119.9-adjacent-steps-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const OUT_DIR = path.join(__dirname, 'artifacts');
fs.mkdirSync(OUT_DIR, { recursive: true });

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
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't119.9-adjacent-steps-test', ok: true }));
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
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      resolve({ srv, port });
    });
    srv.on('error', reject);
  });
}

function ownerReproTape() {
  function tool(name, sid) {
    return {
      type: 'assistant',
      stream_id: sid,
      message: { role: 'assistant', content: [{ type: 'tool_use', name: name }] },
    };
  }
  function text(body, sid) {
    return {
      type: 'assistant',
      stream_id: sid,
      message: { role: 'assistant', content: [{ type: 'text', text: body }] },
    };
  }
  const a = 'sid-t119.9-a';
  const b = 'sid-t119.9-b';
  const tape = [
    { type: 'user', message: { role: 'user', content: 'first' } },
    text('checking the fleet', a),
  ];
  for (let i = 0; i < 5; i++) tape.push(tool('A' + i, a));
  tape.push(text('still working', a));
  for (let i = 0; i < 5; i++) tape.push(tool('B' + i, a));
  tape.push({ type: 'user', message: { role: 'user', content: 'second' } });
  tape.push(text('on it', b));
  for (let i = 0; i < 3; i++) tape.push(tool('C' + i, b));
  tape.push(text('more', b));
  for (let i = 0; i < 3; i++) tape.push(tool('D' + i, b));
  return tape;
}

async function main() {
  const failures = [];
  const { srv, port } = await startStaticServer();
  const base = `http://127.0.0.1:${port}/`;
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });
  const tape = ownerReproTape();

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && typeof window.resetTranscript === 'function',
      null,
      { timeout: 10000 },
    );

    await page.evaluate((events) => {
      for (let i = 0; i < events.length; i++) window.handle(events[i]);
    }, tape);
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));

    const census = await page.evaluate((events) => {
      const rows = (window.__transcriptRows || []).map((r) => ({
        role: r && r.role,
        kind: r && r.kind,
        text: r && r.text,
        h: r && r.height,
      }));
      const adjacent = [];
      for (let i = 1; i < rows.length; i++) {
        if (rows[i - 1].role === 'turn-marker' && rows[i].role === 'turn-marker') {
          adjacent.push(i);
        }
      }
      const markers = Array.from(document.querySelectorAll('#messages-canvas .turn-marker, #messages .turn-marker'));
      const markerTops = markers.map((el) => {
        const b = el.getBoundingClientRect();
        return {
          label: (el._label && el._label.textContent) || el.textContent || '',
          top: Math.round(b.top * 10) / 10,
          height: Math.round(b.height * 10) / 10,
        };
      });
      const replayed = (window.ConversationWidget && window.ConversationWidget.displayFromEvents)
        ? window.ConversationWidget.displayFromEvents(events).map((l) => ({
          k: l.kind || l.role,
          n: (l.items || []).length,
          t: l.text,
        }))
        : null;
      return {
        rows: rows,
        adjacent: adjacent,
        markerCount: markers.length,
        markerTops: markerTops,
        replayed: replayed,
      };
    }, tape);

    if (census.adjacent.length) {
      failures.push('adjacent turn-markers in live tail at ' + JSON.stringify(census.adjacent)
        + ' rows=' + JSON.stringify(census.rows.map((r) => r.role + ':' + r.text)));
    }
    const markerRows = census.rows.filter((r) => r.role === 'turn-marker');
    if (markerRows.length !== 2) {
      failures.push('expected 2 turn-marker rows, got ' + markerRows.length
        + ' ' + JSON.stringify(markerRows));
    } else {
      if (markerRows[0].text !== '⋯ 10 steps') {
        failures.push('first capsule want ⋯ 10 steps, got ' + JSON.stringify(markerRows[0].text));
      }
      if (markerRows[1].text !== '⋯ 6 steps') {
        failures.push('second capsule want ⋯ 6 steps, got ' + JSON.stringify(markerRows[1].text));
      }
    }
    if (census.markerCount !== 2) {
      failures.push('DOM .turn-marker count want 2, got ' + census.markerCount
        + ' ' + JSON.stringify(census.markerTops));
    }
    if (!census.replayed) {
      failures.push('ConversationWidget.displayFromEvents missing');
    } else {
      const replaySlots = census.replayed.filter((l) => l.k === 'turn-slot');
      if (replaySlots.length !== 2
          || replaySlots[0].n !== 10 || replaySlots[1].n !== 6) {
        failures.push('reload fold want two slots 10 then 6, got ' + JSON.stringify(replaySlots));
      }
      const replayAdj = [];
      for (let i = 1; i < census.replayed.length; i++) {
        if (census.replayed[i - 1].k === 'turn-slot' && census.replayed[i].k === 'turn-slot') {
          replayAdj.push(i);
        }
      }
      if (replayAdj.length) {
        failures.push('reload fold has adjacent turn-slots at ' + JSON.stringify(replayAdj));
      }
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't119.9-adjacent-steps.png'), fullPage: true });

    if (failures.length) {
      console.error('FAIL t119.9-adjacent-steps-test');
      for (const f of failures) console.error(' -', f);
      process.exitCode = 1;
    } else {
      console.log('ok - T119.9 live handle: one capsule per unbroken burst, no adjacent ⋯ N (🎯T119.9)');
    }
  } finally {
    await browser.close();
    srv.close();
  }
}

main().catch((err) => {
  console.error(err && err.stack || err);
  process.exit(1);
});
