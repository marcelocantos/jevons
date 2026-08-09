// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright real-render smoke for 🎯T370: Cmd+Shift+] / Cmd+Shift+[ cycle
// the fleet tree selection through every visible node, root overseer
// included, showing the selected agent's transcript and focusing that node's
// message box (root → the main composer). Hermetic static server + mocked
// agents; no live daemon.
//
// Policy is unit-covered in web/scripts/fleet_cycle_test.js — this test
// exists because selection paint, transcript swap, focus and preventDefault
// are browser behaviour that a DOM-free helper cannot attest to.
//
//   node scripts/chat-ui-test/t370-fleet-cycle-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');

// The cycle must follow the order the tree actually paints, so the expected
// order is read back from the DOM rather than assumed here (asides sort ahead
// of work agents, which an assumed order gets wrong).
const ROOT = 'jevons';
const PO = 'jevons-po';
const WORKER = 'jv-t370-tree-cycle';
const ASIDE = 'att-t370aside';

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  const agents = [
    { name: ROOT, workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer' },
    { name: PO, workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: ROOT, status: 'running', purpose: 'work' },
    { name: WORKER, workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: PO, status: 'running', purpose: 'work', target_id: 'T370' },
    { name: ASIDE, workdir: '/Users/x/.jevons/asides/att-t370aside', parent: ROOT, status: 'running', purpose: 'aside', aside_kind: 'side', description: 'cycle smoke aside' },
  ];
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(agents));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 't370-fleet-cycle-test', ok: true }));
        return;
      }
      if (u.pathname.startsWith('/api/agents/') && u.pathname.endsWith('/transcript')) {
        const name = decodeURIComponent(u.pathname.split('/')[3] || '');
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          name: name,
          turns: [{ role: 'assistant', text: 'transcript of ' + name, when: Date.now() - 1000 }],
        }));
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

  // Selection state as the owner sees it: the highlighted row, the agent
  // whose transcript the sidebar names, and where the caret actually is.
  // The inspect pane's content arrives over /ws/chat, which this static
  // harness does not serve — so "shows that agent's transcript" is asserted
  // at the binding the product actually sets: the conversation surface's
  // agent id, the composer's bound agent, and the active Transcript tab.
  const snap = () => page.evaluate(() => {
    const sel = document.querySelector('.agent-node.selected');
    const pane = document.getElementById('agent-inspect');
    const side = document.getElementById('agent-inspect-input');
    return {
      selectedRow: sel ? (sel.dataset.agent || '') : '',
      selectedAgent: (typeof selectedAgent !== 'undefined' && selectedAgent) || '',
      inspectAgent: (pane && pane.dataset.agentId) || '',
      boundAgent: (side && side.dataset.boundAgent) || '',
      tab: (typeof rhsBottomTab !== 'undefined' && rhsBottomTab) || '',
      activeId: (document.activeElement && document.activeElement.id) || '',
    };
  });
  const forward = () => page.keyboard.press('Meta+Shift+BracketRight');
  const backward = () => page.keyboard.press('Meta+Shift+BracketLeft');

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.FleetCycle !== 'undefined', null, { timeout: 10000 });
    // Wait for the fleet tree to paint all four agents plus the folder.
    await page.waitForFunction(
      () => document.querySelectorAll('.agent-node').length >= 4,
      null, { timeout: 10000 }
    );
    // The tree paints from a fetch kicked off in an earlier script block, so
    // rows can exist while the block holding selectAgent's `const` sidebar
    // refs is still evaluating. Touching selectAgent before then is a TDZ
    // throw, not a product bug — wait for the bindings to be live.
    await page.waitForFunction(() => {
      try {
        return typeof selectAgent === 'function' && !!agentInspectInput;
      } catch (_) {
        return false; // temporal dead zone: block still evaluating
      }
    }, null, { timeout: 10000 });

    // The cycle order is the painted row order minus virtual folders. Read it
    // back so the test asserts "follows the tree" rather than a guessed list.
    const EXPECTED = await page.evaluate(() =>
      Array.prototype.map.call(document.querySelectorAll('.agent-node'), (n) => ({
        agent: n.dataset.agent || '',
        portfolio: n.classList.contains('agent-portfolio'),
      })).filter((r) => r.agent && !r.portfolio).map((r) => r.agent));

    [ROOT, PO, WORKER, ASIDE].forEach((n) => {
      if (EXPECTED.indexOf(n) < 0) failures.push('painted tree is missing ' + n);
    });
    if (EXPECTED[0] !== ROOT) {
      failures.push('root overseer must be the first cycle stop, got "' + EXPECTED[0] + '"');
    }

    // 🎯T252 auto-selects an aside that wants attention, and its poll re-claims
    // an empty selection — which would fight the cycle every time it rests on
    // root. Drain the queue so this test measures T370 alone; the interaction
    // itself is product behaviour, not a test artifact.
    await page.evaluate(() => {
      try { asideAttentionQueue = []; } catch (_) {}
      if (typeof selectAgent === 'function') selectAgent(null);
    });

    // A window listener runs after the document handler, so it reports
    // whether the cycle claimed the chord.
    await page.evaluate(() => {
      window.__t370 = [];
      window.addEventListener('keydown', (e) => {
        if (e.metaKey && e.shiftKey && /^Bracket/.test(e.code)) {
          window.__t370.push({ code: e.code, prevented: e.defaultPrevented });
        }
      });
    });

    // ── Start at root: nothing selected, main chat is the overseer view ──
    // (Portfolio folders need daemon config the static mock has none of, so
    // skipping them is asserted in web/scripts/fleet_cycle_test.js instead.)
    const start = await snap();
    if (start.selectedAgent) {
      failures.push('expected no selection at root after deselect, got "' + start.selectedAgent + '"');
    }

    // ── Forward lap: root → PO → worker → aside → back to root ──
    for (let i = 1; i < EXPECTED.length; i++) {
      const want = EXPECTED[i];
      await forward();
      const s = await snap();
      if (s.selectedAgent !== want) {
        failures.push('forward step ' + i + ' should select ' + want + ', got "' + s.selectedAgent + '"');
      }
      if (s.selectedRow !== want) {
        failures.push('forward step ' + i + ' should highlight row ' + want + ', got "' + s.selectedRow + '"');
      }
      if (s.inspectAgent !== want) {
        failures.push('sidebar transcript should be bound to ' + want
          + ', got "' + s.inspectAgent + '"');
      }
      if (s.boundAgent !== want) {
        failures.push('sidebar message box should be bound to ' + want
          + ', got "' + s.boundAgent + '"');
      }
      if (s.tab !== 'transcript') {
        failures.push('stepping onto ' + want + ' should show the Transcript tab, got "'
          + s.tab + '"');
      }
      if (s.activeId !== 'agent-inspect-input') {
        failures.push('step onto ' + want + ' should focus the sidebar message box, got "' + s.activeId + '"');
      }
    }

    // Wrap: the step off the last node lands on root, which is a real stop.
    await forward();
    const wrapped = await snap();
    if (wrapped.selectedAgent) {
      failures.push('wrap to root should clear the agent selection, got "' + wrapped.selectedAgent + '"');
    }
    if (wrapped.activeId !== 'input') {
      failures.push('root must focus the MAIN message box, got "' + wrapped.activeId + '"');
    }

    // Root is a stop, not a skip: one more forward resumes at the second stop.
    await forward();
    const afterRoot = await snap();
    if (afterRoot.selectedAgent !== EXPECTED[1]) {
      failures.push('forward from root should select ' + EXPECTED[1]
        + ', got "' + afterRoot.selectedAgent + '"');
    }

    // ── Reverse: back to root, then wrap round to the last node ──
    await backward();
    const backRoot = await snap();
    if (backRoot.selectedAgent) {
      failures.push('reverse to root should clear selection, got "' + backRoot.selectedAgent + '"');
    }
    if (backRoot.activeId !== 'input') {
      failures.push('reverse onto root must focus the main box, got "' + backRoot.activeId + '"');
    }

    const last = EXPECTED[EXPECTED.length - 1];
    const secondLast = EXPECTED[EXPECTED.length - 2];
    await backward();
    const backWrap = await snap();
    if (backWrap.selectedAgent !== last) {
      failures.push('reverse from root should wrap to ' + last + ', got "' + backWrap.selectedAgent + '"');
    }
    if (backWrap.activeId !== 'agent-inspect-input') {
      failures.push('reverse wrap should focus the sidebar box, got "' + backWrap.activeId + '"');
    }

    // Reverse is the inverse of forward: step back onto the previous stop.
    await backward();
    const backPrev = await snap();
    if (backPrev.selectedAgent !== secondLast) {
      failures.push('reverse from ' + last + ' should select ' + secondLast
        + ', got "' + backPrev.selectedAgent + '"');
    }

    // Every chord press must be claimed, or the browser would also act on it.
    const claimed = await page.evaluate(() => window.__t370.slice());
    if (!claimed.length || claimed.some((r) => !r.prevented)) {
      failures.push('all cycle presses must be claimed (defaultPrevented): ' + JSON.stringify(claimed));
    }

    // ── The chord works from inside a message box (typing `[` does not) ──
    await page.focus('#input');
    await page.keyboard.type('draft text [not a chord]');
    const typed = await snap();
    if (typed.selectedAgent !== secondLast) {
      failures.push('typing brackets must not move the fleet selection, got "'
        + typed.selectedAgent + '"');
    }
    await forward();
    const fromComposer = await snap();
    if (fromComposer.selectedAgent !== last) {
      failures.push('chord from the main composer should still cycle, got "'
        + fromComposer.selectedAgent + '"');
    }

    // ── Empty beyond root: the chord still claims and rests on root ──
    await page.evaluate(() => { window.__t370 = []; });
    await page.evaluate(() => {
      const el = document.getElementById('agents');
      if (el) el.innerHTML = '';
      if (typeof selectAgent === 'function') selectAgent(null);
    });
    await forward();
    const empty = await snap();
    if (empty.selectedAgent) {
      failures.push('empty-beyond-root cycle should rest on root, got "' + empty.selectedAgent + '"');
    }
    if (empty.activeId !== 'input') {
      failures.push('empty-beyond-root should focus the main box, got "' + empty.activeId + '"');
    }
    await backward();
    const emptyBack = await snap();
    if (emptyBack.selectedAgent) {
      failures.push('empty-beyond-root reverse should rest on root, got "'
        + emptyBack.selectedAgent + '"');
    }
    const emptyClaimed = await page.evaluate(() => window.__t370.slice());
    if (emptyClaimed.length !== 2 || emptyClaimed.some((r) => !r.prevented)) {
      failures.push('empty-beyond-root presses must still be claimed: '
        + JSON.stringify(emptyClaimed));
    }

    if (failures.length) {
      console.error('FAIL t370-fleet-cycle-test');
      failures.forEach((f) => console.error('  -', f));
      process.exitCode = 1;
    } else {
      console.log('PASS t370-fleet-cycle-test (⌘⇧[/] cycles every node incl root→main composer)');
    }
  } catch (e) {
    console.error('FAIL t370-fleet-cycle-test', e && e.stack || e);
    process.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
})();
