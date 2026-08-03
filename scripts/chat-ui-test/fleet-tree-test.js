// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for RHS fleet who-started-whom tree (🎯T68) and list
// completeness wiring (🎯T72.1 client side). Stubs /api/agents, drives
// refreshAgents/renderTree, asserts hierarchy + stable sibling name order.
//
//   node scripts/chat-ui-test/fleet-tree-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer(agentsPayload) {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(agentsPayload()));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 'fleet-tree-test', ok: true }));
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
  // Deliberately unsorted feed — UI must still show stable name order under each parent.
  // Fan-out workers share PO workdir (typical); zeta keeps a distinct path.
  const poRepo = '/Users/x/work/github.com/org/po-repo';
  let agents = [
    { name: 'zeta-worker', workdir: '/Users/x/work/github.com/org/other', parent: 'po', status: 'running', progress: 'working · xcodebuild' },
    { name: 'alpha-worker', workdir: poRepo, parent: 'po', status: 'stopped', progress: 'stopped' },
    { name: 'po', workdir: poRepo, parent: 'jevons', status: 'running' },
    // 🎯T115: root overseer state-dir home must not render as path chrome.
    { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running' },
    // 🎯T118: same-workdir leaf under po → progress secondary, not path.
    { name: 'mid-worker', workdir: poRepo, parent: 'po', status: 'running', phase: 'working', step: 'Bash: go test', progress: 'working · Bash: go test' },
    // Aside-purpose row: 💡 title, no path element (description must not bleed).
    { name: 'att-billing', description: 'billing nit', purpose: 'aside', workdir: '/Users/x/.jevons/threads/att-billing', parent: 'jevons', status: 'running' },
  ];
  const { srv, base } = await startStaticServer(() => agents);
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.refreshAgents === 'function' || typeof refreshAgents === 'function', null, { timeout: 10000 });
    await page.evaluate(() => {
      try { refreshAgents(); } catch (_) { window.refreshAgents && window.refreshAgents(); }
    });
    await page.waitForTimeout(200);

    const tree = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('#agents .agent-node')];
      return nodes.map(n => ({
        name: n.dataset.agent,
        parent: n.dataset.parent || '',
        depth: (() => {
          let d = 0, el = n.parentElement;
          while (el) {
            if (el.classList && el.classList.contains('agent-children')) d++;
            el = el.parentElement;
          }
          return d;
        })(),
        indent: n.closest('.agent-children') ? true : false,
      }));
    });

    if (tree.length !== 6) {
      failures.push(`expected 6 agent nodes (completeness), got ${tree.length}: ${JSON.stringify(tree)}`);
    }
    const names = tree.map(t => t.name);
    if (!names.includes('zeta-worker') || !names.includes('alpha-worker') || !names.includes('jevons') || !names.includes('att-billing')) {
      failures.push(`missing agents in panel: ${names.join(',')}`);
    }
    // Top-level roots first: only jevons has empty parent in this fixture.
    const roots = tree.filter(t => t.parent === '');
    if (roots.length !== 1 || roots[0].name !== 'jevons') {
      failures.push(`roots = ${JSON.stringify(roots)}, want single jevons`);
    }
    // Under po: alpha, mid, zeta in locale name order.
    const underPo = tree.filter(t => t.parent === 'po').map(t => t.name);
    const wantSiblings = ['alpha-worker', 'mid-worker', 'zeta-worker'];
    if (JSON.stringify(underPo) !== JSON.stringify(wantSiblings)) {
      failures.push(`sibling order under po = ${JSON.stringify(underPo)}, want ${JSON.stringify(wantSiblings)}`);
    }
    // po must nest under jevons (parent attr).
    const po = tree.find(t => t.name === 'po');
    if (!po || po.parent !== 'jevons') {
      failures.push(`po parent=${po && po.parent}, want jevons`);
    }

    // 🎯T115: overseer omits ~/.jevons/jevons path; aside has 💡 and no path.
    const chrome = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('#agents .agent-node')];
      function row(name) {
        const n = nodes.find(el => el.dataset.agent === name);
        if (!n) return null;
        const dir = n.querySelector('.agent-dir');
        return {
          nameText: (n.querySelector('.agent-name') || {}).textContent || '',
          hasDir: !!dir,
          dirText: dir ? (dir.textContent || '') : '',
          isAsideClass: n.classList.contains('agent-aside'),
        };
      }
      return { jevons: row('jevons'), aside: row('att-billing'), po: row('po') };
    });
    if (!chrome.jevons) {
      failures.push('T115: jevons row missing');
    } else {
      if (chrome.jevons.hasDir || /\.jevons|jevons\/jevons/i.test(chrome.jevons.dirText)) {
        failures.push('T115: overseer must omit state-dir path: ' + JSON.stringify(chrome.jevons));
      }
      if (chrome.jevons.nameText !== 'jevons') {
        failures.push('T115: overseer title: ' + JSON.stringify(chrome.jevons.nameText));
      }
    }
    if (!chrome.aside) {
      failures.push('T115: aside row missing');
    } else {
      if (!/^💡\s*billing nit$/.test(chrome.aside.nameText)) {
        failures.push('T115: aside title wants 💡 billing nit: ' + JSON.stringify(chrome.aside.nameText));
      }
      if (chrome.aside.hasDir || chrome.aside.dirText) {
        failures.push('T115: aside must have no path element: ' + JSON.stringify(chrome.aside));
      }
      if (!chrome.aside.isAsideClass) {
        failures.push('T115: aside missing agent-aside class');
      }
    }
    if (!chrome.po || !chrome.po.hasDir || !/org\/po-repo/.test(chrome.po.dirText)) {
      failures.push('T115: work agent (PO) must keep path chrome: ' + JSON.stringify(chrome.po));
    }

    // 🎯T118: same-workdir worker → non-path progress secondary; distinct workdir keeps path.
    const t118 = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('#agents .agent-node')];
      function row(name) {
        const n = nodes.find(el => el.dataset.agent === name);
        if (!n) return null;
        const dir = n.querySelector('.agent-dir');
        return {
          secondary: n.dataset.secondary || '',
          hasDir: !!dir,
          dirText: dir ? (dir.textContent || '') : '',
          dirClass: dir ? dir.className : '',
        };
      }
      return { mid: row('mid-worker'), alpha: row('alpha-worker'), zeta: row('zeta-worker'), po: row('po') };
    });
    if (!t118.mid) {
      failures.push('T118: mid-worker row missing');
    } else {
      if (t118.mid.secondary !== 'progress') {
        failures.push('T118: mid-worker secondaryKind want progress: ' + JSON.stringify(t118.mid));
      }
      if (!/working/.test(t118.mid.dirText) || !/Bash/.test(t118.mid.dirText)) {
        failures.push('T118: mid-worker progress text: ' + JSON.stringify(t118.mid));
      }
      if (/po-repo|github\.com/.test(t118.mid.dirText)) {
        failures.push('T118: mid-worker must not show path as secondary: ' + JSON.stringify(t118.mid));
      }
      if (!/agent-progress/.test(t118.mid.dirClass)) {
        failures.push('T118: mid-worker missing agent-progress class: ' + JSON.stringify(t118.mid));
      }
    }
    if (!t118.alpha || t118.alpha.secondary !== 'progress' && t118.alpha.secondary !== 'status') {
      // stopped + progress field → progress kind
      if (!t118.alpha || !/stopped/i.test(t118.alpha.dirText) || /po-repo/.test(t118.alpha.dirText || '')) {
        failures.push('T118: alpha-worker should show non-path status/progress: ' + JSON.stringify(t118.alpha));
      }
    }
    if (!t118.zeta || t118.zeta.secondary !== 'path' || !/org\/other/.test(t118.zeta.dirText || '')) {
      failures.push('T118: distinct-workdir worker keeps path: ' + JSON.stringify(t118.zeta));
    }
    if (!t118.po || t118.po.secondary !== 'path') {
      failures.push('T118: PO with children keeps path secondary: ' + JSON.stringify(t118.po));
    }

    // 🎯T71 thin: working indicator picks up tool progress.
    await page.evaluate(() => {
      setWorking(true);
      addTurnItem('tool-use', 'jevons_agent_start: name=worker');
    });
    await page.waitForTimeout(50);
    const work = await page.evaluate(() => {
      const el = document.querySelector('.working-indicator');
      return el ? el.textContent : '';
    });
    if (!/step/i.test(work) || !/jevons_agent_start/i.test(work)) {
      failures.push(`T71 working progress not shown: ${JSON.stringify(work)}`);
    }

    // 🎯T94: idle stuck-watchdog — progress resets idle clock; pure idle fires recover.
    // Stub transport open/close: static hermetic server has no /ws/chat, and
    // onClose would call setWorking(false) and onOpen wipes #messages.
    await page.evaluate(() => {
      if (typeof transport !== 'undefined' && transport) {
        transport.onClose = function () {};
        transport.onOpen = function () {};
      }
      WORKING_STUCK_MS = 150;
      setWorking(true);
    });
    // Mid-turn progress must keep working alive past the bound.
    for (let i = 0; i < 5; i++) {
      await page.waitForTimeout(60);
      await page.evaluate((n) => { updateWorkingProgress('step ' + n + ' · tool_x'); }, i);
    }
    await page.waitForTimeout(50);
    const midHealthy = await page.evaluate(() => !!document.querySelector('.working-indicator'));
    if (!midHealthy) {
      failures.push('T94 false-recovered while progress was still arriving');
    }
    // Stop progress; idle bound should clear working + status note.
    await page.waitForTimeout(250);
    const afterStuck = await page.evaluate(() => ({
      working: !!document.querySelector('.working-indicator'),
      status: [...document.querySelectorAll('.msg.status')].map(e => e.textContent).join(' | '),
    }));
    if (afterStuck.working) {
      failures.push('T94 stuck-watchdog left working indicator on after idle bound');
    }
    if (!/stuck|Recovered|idle/i.test(afterStuck.status)) {
      failures.push('T94 stuck-watchdog status note missing: ' + JSON.stringify(afterStuck.status));
    }
  } catch (e) {
    failures.push(String(e && e.stack || e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL fleet-tree-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('ok - fleet tree hierarchy + stable sibling sort (T68); completeness; T71 working progress; T115 overseer/aside chrome; T118 progress secondary');
})();
