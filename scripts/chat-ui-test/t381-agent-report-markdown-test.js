// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T381 — agent reports render as markdown; only what the owner actually
// typed is painted verbatim.
//
// The owner's screenshot: orthograph-po's 🎯T22 seal report painted as raw
// source in the main transcript — a literal "**Commit:**", a literal
// "## Oracle evidence", an unrendered pipe table — while the 🎯T22 chips
// rendered fine and the long lines ran off the right edge.
//
// The trap this test exists to hold shut: the naive fix is "make the user
// branch render markdown", and that is a REGRESSION, not a pass. Owner input
// is rendered verbatim on purpose. So this drives BOTH directions through the
// real paint path and fails if either one moves:
//
//   1. agent report  (turn_origin: agent) → h2 / strong / table / code render
//   2. owner text    (turn_origin absent) → literal ** ## | survive as text
//   3. target chip   (owner turn naming 🎯T22) → still a .target-hotspot
//   4. no bubble content pushes #messages into a horizontal scroll
//
// Case 2 uses the SAME markdown-shaped body as case 1, so the only thing that
// can separate them is provenance. A body-sniffing implementation — the one
// acceptance criterion 3 rules out — fails here rather than passing quietly.
//
//   node scripts/chat-ui-test/t381-agent-report-markdown-test.js [--headed]

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
        res.end(JSON.stringify({ version: 't381-agent-report-markdown', ok: true }));
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

// The report body is the owner's actual screenshot content, trimmed: a
// heading, bold labels, a pipe table, inline and fenced code, and one
// deliberately unbreakable 200-character token for the wrap criterion.
const LONG_TOKEN = 'x'.repeat(200);
const REPORT = [
  '🎯T22 SEALED — independently verified, not accepted on the worker\'s say-so.',
  '',
  '**Commit:** `bec51ca` "Never advertise a loopback address to a pad (T22)"',
  '',
  '## Oracle evidence',
  '',
  // Wide on purpose: a table is the one markdown construct that refuses to
  // shrink below its min-content width, so a report table is what actually
  // drives the transcript sideways when it has no scroll port of its own.
  '| Criterion | Evidence | Status |',
  '| --- | --- | --- |',
  // The unbreakable token is the load-bearing part. Ordinary prose in a cell
  // wraps on its spaces and the table quietly fits; a long path with nowhere
  // to break sets the cell's min-content width, and a table refuses to lay
  // out narrower than that — which is precisely how a report shoves the whole
  // transcript sideways when nothing gives it a scroll port.
  '| 1 — loopback is never advertised to a pad | /Users/marcelo/work/github.com/marcelocantos/jevons/internal/advertise/loopback_never_advertised_darwin_arm64_test.go | green |',
  '| 2 — the daily daemon serves the fixed path | live probe `curl -sS http://127.0.0.1:13705/api/discovery` returns non-404 | green |',
  '',
  '```sh',
  'go test ./internal/advertise -run TestLoopback',
  '```',
  '',
  'Trace: /very/long/unbreakable/' + LONG_TOKEN,
].join('\n');

// Byte-identical markdown, but the OWNER typed it. Every marker must survive.
const OWNER_TYPED = [
  'Why is UI not rendering markdown in requests?',
  '',
  '**Commit:** should stay literal here',
  '',
  '## and so should this heading',
  '',
  '| a | b |',
  '| --- | --- |',
].join('\n');

const OWNER_CHIP = 'did 🎯T22 actually seal, or is that chip lying to me?';

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' && !!window.marked,
      null, { timeout: 10000 },
    );

    await page.evaluate((fx) => {
      // 1. The owner types markdown-shaped prose. No turn_origin on the wire
      //    is the owner default — this is exactly what /ws/chat echoes.
      window.handle({ type: 'user', message: { role: 'user', content: fx.ownerTyped } });
      // 2. An agent report injected on the user-turn wire, stamped by the server.
      window.handle({
        type: 'user',
        turn_origin: 'agent',
        message: { role: 'user', content: fx.report },
      });
      // 3. An owner turn naming a target — the chip must still decorate.
      window.handle({ type: 'user', message: { role: 'user', content: fx.ownerChip } });
    }, { ownerTyped: OWNER_TYPED, report: REPORT, ownerChip: OWNER_CHIP });

    await page.waitForTimeout(250);

    const state = await page.evaluate(() => {
      const msgs = document.getElementById('messages');
      const bubbles = Array.from(msgs.querySelectorAll('.msg.user'));
      const readBubble = (el) => {
        const body = el.querySelector('.msg-body') || el;
        return {
          text: body.textContent || '',
          html: body.innerHTML || '',
          isAgentReport: el.classList.contains('agent-report'),
          headings: body.querySelectorAll('h1,h2,h3').length,
          strongs: body.querySelectorAll('strong,b').length,
          tables: body.querySelectorAll('table').length,
          codes: body.querySelectorAll('code').length,
          pres: body.querySelectorAll('pre').length,
          chips: body.querySelectorAll('.target-hotspot').length,
          // Does anything inside overflow the bubble's own content box?
          overflowsBubble: Array.from(body.querySelectorAll('*')).some(
            (n) => n.scrollWidth > n.clientWidth + 1 && getComputedStyle(n).overflowX === 'visible',
          ),
        };
      };
      return {
        count: bubbles.length,
        bubbles: bubbles.map(readBubble),
        transcript: {
          scrollWidth: msgs.scrollWidth,
          clientWidth: msgs.clientWidth,
        },
        docScrollWidth: document.documentElement.scrollWidth,
        docClientWidth: document.documentElement.clientWidth,
      };
    });

    if (state.count !== 3) {
      failures.push(`expected 3 user bubbles (owner md, agent report, owner chip), got ${state.count}`);
    }
    const [owner, report, chip] = state.bubbles;

    // ── Direction 1: the agent report RENDERS ───────────────────────────
    if (report) {
      if (!report.isAgentReport) {
        failures.push('agent report bubble is missing the .agent-report class — the turn_origin never reached the paint path');
      }
      if (report.headings < 1) failures.push('agent report: "## Oracle evidence" did not become a heading');
      if (report.strongs < 1) failures.push('agent report: "**Commit:**" did not become bold');
      if (report.tables < 1) failures.push('agent report: the pipe table did not become a <table>');
      if (report.pres < 1) failures.push('agent report: the fenced block did not become a <pre>');
      if (report.codes < 1) failures.push('agent report: `bec51ca` did not become inline code');
      if (report.text.includes('**Commit:**')) {
        failures.push('agent report: literal "**Commit:**" still visible — painted as raw source');
      }
      if (report.text.includes('## Oracle evidence')) {
        failures.push('agent report: literal "## Oracle evidence" still visible — painted as raw source');
      }
      if (report.text.includes('| Criterion |')) {
        failures.push('agent report: literal pipe-table row still visible — painted as raw source');
      }
    }

    // ── Direction 2: owner input stays VERBATIM ─────────────────────────
    // This is the half a careless fix breaks. The body is markdown-shaped on
    // purpose: only provenance can tell it apart from the report above.
    if (owner) {
      if (owner.isAgentReport) {
        failures.push('owner-typed turn was classed as an agent report');
      }
      if (!owner.text.includes('**Commit:** should stay literal here')) {
        failures.push('REGRESSION: owner-typed "**Commit:**" was eaten by the markdown renderer');
      }
      if (!owner.text.includes('## and so should this heading')) {
        failures.push('REGRESSION: owner-typed "## heading" was eaten by the markdown renderer');
      }
      if (!owner.text.includes('| a | b |')) {
        failures.push('REGRESSION: owner-typed pipe table was eaten by the markdown renderer');
      }
      if (owner.headings > 0) failures.push('REGRESSION: owner text produced a heading element');
      if (owner.tables > 0) failures.push('REGRESSION: owner text produced a table element');
      if (owner.strongs > 0) failures.push('REGRESSION: owner text produced a bold element');
    }

    // ── Target chips still work on an owner turn ────────────────────────
    if (chip) {
      if (chip.chips < 1) {
        failures.push('owner turn naming 🎯T22 lost its target chip (.target-hotspot)');
      }
      if (!chip.text.includes('🎯T22')) failures.push('target-chip turn lost its text');
    }

    // ── Criterion 4: wrap, and no horizontal scroll ─────────────────────
    if (state.transcript.scrollWidth > state.transcript.clientWidth + 1) {
      failures.push(
        `transcript scrolls horizontally: #messages scrollWidth=${state.transcript.scrollWidth} > clientWidth=${state.transcript.clientWidth}`,
      );
    }
    if (state.docScrollWidth > state.docClientWidth + 1) {
      failures.push(
        `page scrolls horizontally: document scrollWidth=${state.docScrollWidth} > clientWidth=${state.docClientWidth}`,
      );
    }
    if (report && report.overflowsBubble) {
      failures.push('agent report content overflows its bubble without its own scroll port');
    }

    await page.screenshot({ path: path.join(OUT_DIR, 't381-agent-report-markdown.png'), fullPage: true });
  } catch (e) {
    failures.push('exception: ' + (e && e.stack ? e.stack : e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL 🎯T381 agent-report markdown');
    for (const f of failures) console.error('  ✗ ' + f);
    process.exit(1);
  }
  console.log('ok  - 🎯T381: agent reports render markdown; owner input stays verbatim; no horizontal scroll');
})();
