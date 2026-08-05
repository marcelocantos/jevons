// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for 🎯T249: one stream_id paints as a single
// growing bubble throughout streaming (including multi-paragraph / tool
// preamble). Never mid-stream multi-bubble that later coalesces.
// Distinct stream_ids stay separate.
//
//   node scripts/chat-ui-test/t249-stream-bubble-test.js [--headed]

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
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't249-stream-bubble-test', ok: true }));
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

async function main() {
  const failures = [];
  const { srv, port } = await startStaticServer();
  const base = `http://127.0.0.1:${port}/`;
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && typeof window.appendOrAddJevons === 'function',
      null,
      { timeout: 10000 },
    );

    // ── Fixture A: T247-land shape — multi-paragraph tokens, one stream_id ──
    const a = await page.evaluate(async () => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      if (typeof clearOpenStreamHandle === 'function') clearOpenStreamHandle();

      const sid = '0c38c30e-53f3-4783-8e08-dc633e707850';
      const tokens = [
        '**', '🎯', 'T', '247', ' landed', '**', ' —', ' independent', ' check', ' agrees',
        ' (`', '0', 'fb', 'ce', '59', '`,', ' herm', 'etics', ' green', ').',
        '\n\n',
        '**', 'Hard', '-', 'reload', '**', ' so', ' `', 'target', ':`', ' /', ' `', 'aside', ':`',
        ' open', ' the', ' aside', ' immediately', ' (', 'no', ' create', ' chip', ').',
        '\n\n',
        'Still', ' in', ' progress', ':', ' **', 'T', '246', '**', ' and', ' **', 'T', '248', '**.',
      ];

      let maxN = 0;
      const midSnaps = [];
      window.handle({
        type: 'assistant',
        stream_id: sid,
        message: {
          role: 'assistant',
          content: [{ type: 'tool_use', name: 'run_terminal_command', input: {} }],
        },
      });

      for (let i = 0; i < tokens.length; i++) {
        window.handle({
          type: 'assistant',
          stream_id: sid,
          message: { role: 'assistant', content: [{ type: 'text', text: tokens[i] }] },
        });
        const n = document.querySelectorAll('#messages .msg.jevons').length;
        if (n > maxN) maxN = n;
        if (n !== 1 || tokens[i] === '\n\n') {
          midSnaps.push({
            i,
            token: tokens[i],
            n,
            texts: Array.from(document.querySelectorAll('#messages .msg.jevons')).map((el) =>
              (typeof el._streamRaw === 'string' ? el._streamRaw : el._layoutText || '').slice(0, 60),
            ),
          });
        }
        // Yield occasionally so rAF stream paint runs mid-stream (T249 paint path).
        if (i % 12 === 11) {
          await new Promise((r) => requestAnimationFrame(r));
        }
      }

      window.handle({
        type: 'assistant',
        stream_id: sid,
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));

      const final = Array.from(document.querySelectorAll('#messages .msg.jevons')).map((el) => ({
        text: el._layoutText || (el._body && el._body.textContent) || '',
        open: typeof el._streamRaw,
        streamId: el._streamId || null,
      }));
      return { maxN, midSnaps, final };
    });

    if (a.maxN !== 1) {
      failures.push(
        `A: max mid-stream jevons bubbles = ${a.maxN}, want 1; snaps=${JSON.stringify(a.midSnaps)}`,
      );
    }
    if (a.final.length !== 1) {
      failures.push(`A: final bubble count ${a.final.length}, want 1: ${JSON.stringify(a.final)}`);
    } else {
      const body = a.final[0].text || '';
      if (!/independent check agrees/i.test(body)) {
        failures.push(`A: missing first paragraph: ${JSON.stringify(body.slice(0, 120))}`);
      }
      if (!/Hard-reload|Hard/i.test(body)) {
        failures.push(`A: missing Hard-reload paragraph: ${JSON.stringify(body.slice(0, 160))}`);
      }
      if (!/Still in progress/i.test(body)) {
        failures.push(`A: missing third paragraph: ${JSON.stringify(body.slice(0, 200))}`);
      }
      if (a.final[0].open === 'string') {
        failures.push('A: stream still open after end_turn');
      }
    }

    // ── Fixture B: re-home after disconnect — still one bubble, no text loss ──
    const b = await page.evaluate(async () => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      if (typeof clearOpenStreamHandle === 'function') clearOpenStreamHandle();

      const sid = 'rehome-sid';
      window.handle({
        type: 'assistant',
        stream_id: sid,
        message: { role: 'assistant', content: [{ type: 'text', text: 'Part one.' }] },
      });
      const first = document.querySelector('#messages .msg.jevons');
      if (first) first.remove(); // disconnect while map still holds it
      window.handle({
        type: 'assistant',
        stream_id: sid,
        message: { role: 'assistant', content: [{ type: 'text', text: '\n\nPart two.' }] },
      });
      window.handle({
        type: 'assistant',
        stream_id: sid,
        message: { role: 'assistant', content: [{ type: 'text', text: '\n\nPart three.' }] },
      });
      window.handle({
        type: 'assistant',
        stream_id: sid,
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const els = Array.from(document.querySelectorAll('#messages .msg.jevons'));
      return {
        n: els.length,
        texts: els.map((el) => el._layoutText || el.textContent || ''),
      };
    });
    if (b.n !== 1) {
      failures.push(`B: re-home after disconnect → ${b.n} bubbles, want 1: ${JSON.stringify(b.texts)}`);
    } else if (!/Part one/.test(b.texts[0] || '') || !/Part three/.test(b.texts[0] || '')) {
      failures.push(`B: re-home lost body: ${JSON.stringify(b.texts[0])}`);
    }

    // ── Fixture C: distinct stream_ids → two bubbles ──
    const c = await page.evaluate(async () => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      if (typeof clearOpenStreamHandle === 'function') clearOpenStreamHandle();

      window.handle({
        type: 'assistant',
        stream_id: 'sid-a',
        message: { role: 'assistant', content: [{ type: 'text', text: 'alpha' }] },
      });
      window.handle({
        type: 'assistant',
        stream_id: 'sid-a',
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
      window.handle({
        type: 'assistant',
        stream_id: 'sid-b',
        message: { role: 'assistant', content: [{ type: 'text', text: 'beta' }] },
      });
      window.handle({
        type: 'assistant',
        stream_id: 'sid-b',
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const els = Array.from(document.querySelectorAll('#messages .msg.jevons'));
      return {
        n: els.length,
        texts: els.map((el) => (el._layoutText || el.textContent || '').trim()),
      };
    });
    if (c.n !== 2) {
      failures.push(`C: distinct stream_ids → ${c.n} bubbles, want 2`);
    } else if (c.texts[0] !== 'alpha' || c.texts[1] !== 'beta') {
      failures.push(`C: wrong texts ${JSON.stringify(c.texts)}`);
    }

    // ── Fixture D: unlabeled open then labeled same turn adopts one bubble ──
    const d = await page.evaluate(async () => {
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      if (typeof clearOpenStreamHandle === 'function') clearOpenStreamHandle();

      // Legacy unlabeled first fragment
      window.handle({
        type: 'assistant',
        message: { role: 'assistant', content: [{ type: 'text', text: 'Unlabeled start.' }] },
      });
      // Later fragments carry stream_id — must adopt, not split (T249)
      window.handle({
        type: 'assistant',
        stream_id: 'late-id',
        message: { role: 'assistant', content: [{ type: 'text', text: '\n\nLabeled continue.' }] },
      });
      window.handle({
        type: 'assistant',
        stream_id: 'late-id',
        message: { role: 'assistant', content: [], stop_reason: 'end_turn' },
      });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const els = Array.from(document.querySelectorAll('#messages .msg.jevons'));
      return {
        n: els.length,
        texts: els.map((el) => el._layoutText || el.textContent || ''),
      };
    });
    if (d.n !== 1) {
      failures.push(`D: unlabeled→labeled adopt → ${d.n} bubbles, want 1: ${JSON.stringify(d.texts)}`);
    } else if (!/Unlabeled start/.test(d.texts[0] || '') || !/Labeled continue/.test(d.texts[0] || '')) {
      failures.push(`D: adopt lost body: ${JSON.stringify(d.texts[0])}`);
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't249-stream-bubble.png'), fullPage: true });

    if (failures.length) {
      console.error('FAIL t249-stream-bubble-test');
      for (const f of failures) console.error(' -', f);
      process.exitCode = 1;
    } else {
      console.log(
        'ok - T249 stream bubble: multi-paragraph one bubble mid-stream; re-home; distinct ids separate; unlabeled adopt',
      );
    }
  } finally {
    await browser.close();
    srv.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
