// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright real-render smoke for 🎯T366 / 🎯T549: Tab cycles the two message
// boxes (main #input ↔ sidebar #agent-inspect-input) when the sidebar composer
// is visible; when hidden, Tab stays on main and is claimed so chrome is not
// walked. Hermetic static server + mocked agents; no live daemon.
//
// Policy itself is unit-covered in web/scripts/composer_focus_test.js — this
// test exists because focus and preventDefault are browser behaviour, not
// something a DOM-free helper can attest to on its own.
//
//   node scripts/chat-ui-test/t366-composer-tab-cycle-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');
const WORKER = 'jv-t366-tabcycle';

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  const agents = [
    { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer' },
    { name: 'jevons-po', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons', status: 'running', purpose: 'work' },
    { name: WORKER, workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'running', purpose: 'work' },
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
        res.end(JSON.stringify({ version: 't366-tab-cycle-test', ok: true }));
        return;
      }
      if (u.pathname.startsWith('/api/agents/') && u.pathname.endsWith('/transcript')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          name: WORKER,
          turns: [{ role: 'assistant', text: 'worker transcript stub', when: Date.now() - 1000 }],
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

  const activeId = () => page.evaluate(() =>
    (document.activeElement && document.activeElement.id) || '');

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.ComposerFocus !== 'undefined', null, { timeout: 10000 });

    // A window listener runs after the document handler, so it sees whether
    // the cycle claimed the chord (defaultPrevented) or left Tab native.
    await page.evaluate(() => {
      window.__t366 = [];
      window.addEventListener('keydown', (e) => {
        if (e.key === 'Tab') {
          window.__t366.push({ shift: !!e.shiftKey, prevented: e.defaultPrevented });
        }
      });
    });

    // ── Sidebar visible: select a worker, open the Transcript composer ──
    const shown = await page.evaluate((worker) => {
      if (typeof selectAgent !== 'function') return { hasSelect: false };
      selectAgent(worker, { purpose: 'work' });
      const c = document.getElementById('agent-inspect-composer');
      const i = document.getElementById('agent-inspect-input');
      return {
        hasSelect: true,
        visible: !!(c && c.classList.contains('visible') && !c.hidden),
        enabled: !!(i && !i.disabled),
      };
    }, WORKER);
    if (!shown.hasSelect) failures.push('selectAgent not available on page');
    if (!shown.visible || !shown.enabled) {
      failures.push('sidebar composer not visible/enabled after selecting ' + WORKER);
    }

    await page.focus('#input');
    if ((await activeId()) !== 'input') failures.push('could not focus main #input');

    await page.keyboard.press('Tab');
    const toSidebar = await activeId();
    if (toSidebar !== 'agent-inspect-input') {
      failures.push('Tab from main should focus agent-inspect-input, got "' + toSidebar + '"');
    }

    await page.keyboard.press('Tab');
    const backToMain = await activeId();
    if (backToMain !== 'input') {
      failures.push('Tab from sidebar should return to #input, got "' + backToMain + '"');
    }

    // Two-stop cycle is stable: a third press lands on the sidebar again.
    await page.keyboard.press('Tab');
    if ((await activeId()) !== 'agent-inspect-input') {
      failures.push('third Tab should land on the sidebar again (two-stop cycle)');
    }

    // Documented reverse: Shift+Tab from the sidebar also returns to main.
    await page.keyboard.press('Shift+Tab');
    const reverse = await activeId();
    if (reverse !== 'input') {
      failures.push('Shift+Tab from sidebar should return to #input, got "' + reverse + '"');
    }

    const claimed = await page.evaluate(() => window.__t366.slice());
    if (claimed.length !== 4 || claimed.some((r) => !r.prevented)) {
      failures.push('all four cycle presses must be claimed (defaultPrevented): '
        + JSON.stringify(claimed));
    }

    // ── Sidebar hidden: Tab claims and stays on main (T549) ──
    await page.evaluate(() => { window.__t366 = []; });
    const hidden = await page.evaluate((worker) => {
      selectAgent(worker); // toggle selection off → composer hidden
      const c = document.getElementById('agent-inspect-composer');
      const i = document.getElementById('agent-inspect-input');
      return {
        visible: !!(c && c.classList.contains('visible') && !c.hidden),
        disabled: !!(i && i.disabled),
      };
    }, WORKER);
    if (hidden.visible) failures.push('sidebar composer still visible after deselect');

    await page.focus('#input');
    await page.keyboard.press('Tab');
    const afterHidden = await activeId();
    if (afterHidden === 'agent-inspect-input') {
      failures.push('Tab focused the hidden sidebar composer');
    }
    if (afterHidden !== 'input') {
      failures.push('Tab from main with hidden sidebar must stay on #input, got "' + afterHidden + '"');
    }
    const claimedHidden = await page.evaluate(() => window.__t366.slice());
    if (!claimedHidden.length || claimedHidden.some((r) => !r.prevented)) {
      failures.push('Tab must be claimed (defaultPrevented) when sidebar is hidden (T549): '
        + JSON.stringify(claimedHidden));
    }

    if (failures.length) {
      console.error('FAIL t366-composer-tab-cycle-test');
      failures.forEach((f) => console.error('  -', f));
      process.exitCode = 1;
    } else {
      console.log('PASS t366-composer-tab-cycle-test (main↔sidebar cycle; T549 stay-main when hidden)');
    }
  } catch (e) {
    console.error('FAIL t366-composer-tab-cycle-test', e && e.stack || e);
    process.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
})();
