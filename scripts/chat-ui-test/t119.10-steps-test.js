// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Live-path perceptual test for 🎯T119.10: owner tape (thinking + tools +
// rewind/fleet-health notes) — window.handle capsules and WorkingProgress N
// match displayFromEvents of the same tape. No empty-tip ⋯ 1 step. Adjacent
// 2+6 after parked thinking text is a fail.
//
//   node scripts/chat-ui-test/t119.10-steps-test.js [--headed]

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
        res.end(JSON.stringify({ version: 't119.10-steps-test', ok: true }));
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

function ownerTape() {
  const sid = 'sid-t119.10';
  function tool(name) {
    return {
      type: 'assistant',
      stream_id: sid,
      message: { role: 'assistant', content: [{ type: 'tool_use', name: name }] },
    };
  }
  function text(body) {
    return {
      type: 'assistant',
      stream_id: sid,
      message: { role: 'assistant', content: [{ type: 'text', text: body }] },
    };
  }
  return [
    { type: 'agent_note', text: '[Fleet health] auto-spawned jv-t390.1.2-auto' },
    {
      type: 'user',
      message: {
        role: 'user',
        content: 'Which target represents the work we discussed to perform regular entropy audits?',
      },
    },
    {
      type: 'agent_note',
      text: '[Conversation rewound by the owner. The record below is the surviving context.]',
    },
    text("I'll look up the entropy-audit target in the ledger and report its current state."),
    tool('search_tool'),
    tool('search_tool'),
    text("Jevons MCP didn't show up in search, so I'll query it directly."),
    tool('search_tool'),
    tool('grep'),
    tool('grep'),
    tool('grep'),
    tool('search_tool'),
    tool('search_tool'),
  ];
}

async function main() {
  const failures = [];
  const { srv, port } = await startStaticServer();
  const base = `http://127.0.0.1:${port}/`;
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });
  const tape = ownerTape();
  const fatal = [];
  page.on('pageerror', (e) => {
    const msg = e && e.message ? e.message : String(e);
    // WS/API 404s are expected on the static fixture; ReferenceErrors are not.
    if (/is not defined|is not a function|Cannot read/.test(msg)) fatal.push(msg);
  });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function'
        && typeof window.ConversationWidget !== 'undefined'
        && !!window.mainConversation
        && typeof window.setWorking === 'function',
      null,
      { timeout: 15000 },
    );

    // Live tape with chrome closed: onWorkingProgress must STORE fold N.
    // Opening chrome afterwards paints stored / fold-derived N (hydrate shape).
    await page.evaluate((events) => {
      for (let i = 0; i < events.length; i++) window.handle(events[i]);
    }, tape);
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    await page.evaluate(() => { window.setWorking(true); });

    const census = await page.evaluate((events) => {
      const rows = (window.__transcriptRows || []).map((r) => ({
        role: r && r.role,
        kind: r && r.kind,
        text: r && r.text,
        itemN: r && r.items ? r.items.length : 0,
      }));
      const adjacent = [];
      for (let i = 1; i < rows.length; i++) {
        if (rows[i - 1].role === 'turn-marker' && rows[i].role === 'turn-marker') {
          adjacent.push(i);
        }
      }
      const markers = Array.from(document.querySelectorAll('#messages-canvas .turn-marker, #messages .turn-marker'));
      const markerInfo = markers.map((el) => {
        const tipKids = el._items ? el._items.children.length : 0;
        return {
          label: (el._label && el._label.textContent) || '',
          tipN: tipKids,
        };
      });
      const replayed = (window.ConversationWidget && window.ConversationWidget.displayFromEvents)
        ? window.ConversationWidget.displayFromEvents(events).map((l) => ({
          k: l.kind || l.role,
          n: (l.items || []).length,
          t: l.text,
        }))
        : null;
      const strip = (document.querySelector('.working-indicator') || {}).textContent || '';
      return {
        rows: rows,
        adjacent: adjacent,
        markerCount: markers.length,
        markerInfo: markerInfo,
        replayed: replayed,
        strip: strip,
      };
    }, tape);

    for (const f of fatal) failures.push('pageerror: ' + f);

    if (census.adjacent.length) {
      failures.push('adjacent turn-markers at ' + JSON.stringify(census.adjacent)
        + ' rows=' + JSON.stringify(census.rows.map((r) => r.role + ':' + r.text)));
    }
    const markerRows = census.rows.filter((r) => r.role === 'turn-marker');
    if (markerRows.length !== 3) {
      failures.push('expected 3 turn-marker rows (note, note, 8 tools), got ' + markerRows.length
        + ' ' + JSON.stringify(markerRows));
    } else {
      if (markerRows[0].text !== '⋯ 1 step' || markerRows[0].itemN !== 1) {
        failures.push('fleet-health capsule want ⋯ 1 step with 1 item, got ' + JSON.stringify(markerRows[0]));
      }
      if (markerRows[1].text !== '⋯ 1 step' || markerRows[1].itemN !== 1) {
        failures.push('rewind capsule want ⋯ 1 step with 1 item, got ' + JSON.stringify(markerRows[1]));
      }
      if (markerRows[2].text !== '⋯ 8 steps' || markerRows[2].itemN !== 8) {
        failures.push('tools capsule want ⋯ 8 steps, got ' + JSON.stringify(markerRows[2]));
      }
    }
    for (let i = 0; i < census.markerInfo.length; i++) {
      const m = census.markerInfo[i];
      if (m.label && m.label.indexOf('⋯') === 0 && m.tipN === 0) {
        failures.push('empty-tip capsule ' + JSON.stringify(m));
      }
    }
    if (census.markerCount !== 3) {
      failures.push('DOM .turn-marker count want 3, got ' + census.markerCount
        + ' ' + JSON.stringify(census.markerInfo));
    }
    if (!census.replayed) {
      failures.push('ConversationWidget.displayFromEvents missing');
    } else {
      const replaySlots = census.replayed.filter((l) => l.k === 'turn-slot');
      if (replaySlots.length !== 3
          || replaySlots[0].n !== 1 || replaySlots[1].n !== 1 || replaySlots[2].n !== 8) {
        failures.push('reload fold want 1, 1, 8, got ' + JSON.stringify(replaySlots));
      }
    }
    if (census.strip.indexOf('8 steps') < 0) {
      failures.push('WorkingProgress strip want 8 steps, got ' + JSON.stringify(census.strip));
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't119.10-steps.png'), fullPage: true });

    if (failures.length) {
      console.error('FAIL t119.10-steps-test');
      for (const f of failures) console.error(' -', f);
      process.exitCode = 1;
    } else {
      console.log('ok - T119.10 live handle: 8-step capsule, no empty-tip 1-step, strip N matches fold (🎯T119.10)');
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
