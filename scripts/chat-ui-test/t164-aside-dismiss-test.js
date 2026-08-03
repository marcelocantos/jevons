// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic E2E for 🎯T164: filed target-asides leave no owner-visible chrome
// or fleet 💡 row.
//
// Real paint path used in production for live turns: scheduleJevonsRender /
// sealAssistantStream → maybeCloseTargetAside (not history paintBody).
//
// Flow: target: open → POST /api/asides dual-write → inject __TARGET_FILED__
// on live seal path → GET /api/agents has no purpose=aside for that id and
// RHS omits the node. Also asserts historyReplayActive blocks false close.
//
//   node scripts/chat-ui-test/t164-aside-dismiss-test.js [--headed]

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

function startMockServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  // In-memory fleet registry: only what POST/DELETE/GET asides+agents need.
  const agents = new Map();
  agents.set('jevons', {
    name: 'jevons',
    purpose: 'overseer',
    parent: '',
    status: 'running',
    workdir: '/tmp/jevons',
    description: '',
  });
  const deletes = [];
  const posts = [];

  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      const method = req.method || 'GET';

      if (u.pathname === '/api/agents' && method === 'GET') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify([...agents.values()]));
        return;
      }

      if (u.pathname === '/api/asides' && method === 'POST') {
        let body = '';
        req.on('data', (c) => { body += c; });
        req.on('end', () => {
          let parsed = {};
          try { parsed = JSON.parse(body || '{}'); } catch (_) { parsed = {}; }
          const id = String(parsed.id || '').trim();
          const title = String(parsed.title || 'aside');
          if (!id) {
            res.writeHead(400); res.end('id required'); return;
          }
          const created = !agents.has(id);
          agents.set(id, {
            name: id,
            purpose: 'aside',
            parent: 'jevons',
            status: 'stopped',
            workdir: '/tmp/asides/' + id,
            description: title,
          });
          posts.push(id);
          res.writeHead(created ? 201 : 200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({
            name: id,
            purpose: 'aside',
            parent: 'jevons',
            description: title,
            workdir: '/tmp/asides/' + id,
            status: 'stopped',
            created: created,
          }));
        });
        return;
      }

      const delMatch = u.pathname.match(/^\/api\/asides\/([^/]+)$/);
      if (delMatch && method === 'DELETE') {
        const id = decodeURIComponent(delMatch[1]);
        agents.delete(id);
        deletes.push(id);
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ name: id, dismissed: true }));
        return;
      }

      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          version: 't164-aside-dismiss-test',
          ok: true,
          posts: posts.slice(),
          deletes: deletes.slice(),
          agents: [...agents.keys()],
        }));
        return;
      }

      // No real WS — page may fail connect; fine for this oracle.
      if (u.pathname === '/ws/chat' || u.pathname.startsWith('/ws/')) {
        res.writeHead(404); res.end(); return;
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
      resolve({
        srv,
        base: `http://127.0.0.1:${srv.address().port}`,
        agents,
        deletes,
        posts,
      });
    });
    srv.on('error', reject);
  });
}

(async () => {
  const failures = [];
  const { srv, base, agents, deletes, posts } = await startMockServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => {
      return typeof AttentionThreads !== 'undefined' &&
        typeof ensureFleetAside === 'function' &&
        typeof maybeCloseTargetAside === 'function' &&
        typeof dismissFleetAside === 'function';
    }, null, { timeout: 15000 });

    // ── 1) Open target: filing aside + dual-write fleet ─────────────────
    // Assign production `let attentionState` (global lexical), not window.*.
    const opened = await page.evaluate(async () => {
      const AT = AttentionThreads;
      const r = AT.handleComposer(AT.emptyState(), 'target: Reproduce usage reports of harnesses');
      attentionState = r.state;
      if (typeof persistAttention === 'function') persistAttention();
      const id = r.threadId;
      const th = AT.findThread(attentionState, id);
      const title = (th && th.title) || 'Reproduce usage reports';
      // Production dual-write used by composer after target: (index.html).
      await ensureFleetAside(id, title);
      return {
        id,
        purpose: th && th.purpose,
        status: th && th.status,
        openCount: AT.stack(attentionState).filter(function (t) {
          return t.purpose === 'file-target' && t.status === 'open';
        }).length,
      };
    });

    if (!opened.id || opened.id.indexOf('att-') !== 0) {
      failures.push('target: did not mint att- id: ' + JSON.stringify(opened));
    }
    if (opened.purpose !== 'file-target' || opened.status !== 'open') {
      failures.push('expected open file-target, got ' + JSON.stringify(opened));
    }
    if (!posts.includes(opened.id)) {
      failures.push('POST /api/asides never saw ' + opened.id + ' posts=' + posts.join(','));
    }
    if (!agents.has(opened.id)) {
      failures.push('registry missing dual-write aside after ensureFleetAside');
    }

    // Wait for RHS 💡 row.
    await page.waitForFunction((id) => {
      const n = document.querySelector('.agent-node[data-agent="' + id + '"]');
      return !!n;
    }, opened.id, { timeout: 5000 }).catch(() => {
      failures.push('RHS never showed dual-written aside node for ' + opened.id);
    });

    // ── 2) History replay must NOT dismiss while open filing is live ────
    const historySafe = await page.evaluate((asideId) => {
      const AT = AttentionThreads;
      // window.historyReplayActive setter mutates production let (index.html).
      historyReplayActive = true;
      maybeCloseTargetAside('Filed 🎯T153 — old\n__TARGET_FILED__:T153\n');
      const stillOpen = (AT.stack(attentionState) || []).some(function (t) {
        return t.id === asideId && t.purpose === 'file-target' && t.status === 'open';
      });
      historyReplayActive = false;
      return { stillOpen };
    }, opened.id);

    if (!historySafe.stillOpen) {
      failures.push('historyReplayActive=true still closed live open file-target (T164)');
    }
    if (deletes.includes(opened.id)) {
      failures.push('DELETE fired during historyReplayActive for live aside (T164)');
    }

    // ── 3) Live production paint path: seal-equivalent maybeCloseTargetAside ─
    // sealAssistantStream / scheduleJevonsRender call this when !historyReplayActive.
    const closed = await page.evaluate(async (asideId) => {
      const AT = AttentionThreads;
      historyReplayActive = false;
      // Real production live path used on seal / stream rAF.
      maybeCloseTargetAside(
        'Filed 🎯T163 — Reproduce usage reports\n__TARGET_FILED__:T163\n',
      );
      // Allow DELETE promise to settle.
      await new Promise(function (r) { setTimeout(r, 150); });
      try { refreshAgents(); } catch (_) {}
      await new Promise(function (r) { setTimeout(r, 150); });
      const openLeft = (AT.stack(attentionState) || []).filter(function (t) {
        return t.purpose === 'file-target' && t.status === 'open';
      });
      const node = document.querySelector('.agent-node[data-agent="' + asideId + '"]');
      const fleetNames = Array.isArray(lastFleetAgents)
        ? lastFleetAgents.map(function (a) { return a && a.name; })
        : [];
      return {
        openLeft: openLeft.length,
        focusMain: AT.isMainFocus(attentionState),
        nodePresent: !!node,
        selectedAgent: selectedAgent,
        fleetNames: fleetNames,
      };
    }, opened.id);

    if (closed.openLeft !== 0) {
      failures.push('open file-target remained after live filed marker: ' + closed.openLeft);
    }
    if (!closed.focusMain) {
      failures.push('selection/focus did not return to main after filed close');
    }
    if (closed.nodePresent) {
      failures.push('RHS still has .agent-node for dismissed aside ' + opened.id);
    }
    if (closed.selectedAgent === opened.id) {
      failures.push('selectedAgent still the dismissed aside');
    }
    if ((closed.fleetNames || []).includes(opened.id)) {
      failures.push('lastFleetAgents still lists ' + opened.id);
    }
    if (!deletes.includes(opened.id)) {
      failures.push('DELETE /api/asides never saw ' + opened.id + ' deletes=' + deletes.join(','));
    }
    if (agents.has(opened.id)) {
      failures.push('in-memory registry still has purpose=aside after DELETE');
    }

    // ── 4) Zombie residual: local already closed, fleet still has stopped row ─
    // Re-register aside, mark local done, live marker must still DELETE.
    const zombie = await page.evaluate(async () => {
      const AT = AttentionThreads;
      const r = AT.handleComposer(AT.emptyState(), 'target: zombie residual filing');
      attentionState = r.state;
      const id = r.threadId;
      await ensureFleetAside(id, 'zombie residual');
      // Local chrome already closed (desync that left fleet row).
      attentionState = AT.closeTargetAside(attentionState, id);
      if (typeof persistAttention === 'function') persistAttention();
      try { refreshAgents(); } catch (_) {}
      await new Promise(function (res) { setTimeout(res, 100); });
      // Live marker with no open file-target — resolve must still see fleet id.
      historyReplayActive = false;
      maybeCloseTargetAside('Filed 🎯T164 — zombie\n__TARGET_FILED__:T164\n');
      await new Promise(function (res) { setTimeout(res, 150); });
      try { refreshAgents(); } catch (_) {}
      await new Promise(function (res) { setTimeout(res, 100); });
      const node = document.querySelector('.agent-node[data-agent="' + id + '"]');
      return { id, nodePresent: !!node };
    });

    if (!deletes.includes(zombie.id)) {
      failures.push('zombie residual: DELETE never saw ' + zombie.id);
    }
    if (agents.has(zombie.id)) {
      failures.push('zombie residual: registry still has ' + zombie.id);
    }
    if (zombie.nodePresent) {
      failures.push('zombie residual: RHS still shows ' + zombie.id);
    }

    // Health snapshot for debug.
    const health = await page.evaluate(async (b) => {
      const r = await fetch(b + '/health');
      return r.json();
    }, base).catch(() => null);

    if (failures.length) {
      console.error('T164 FAIL:', failures.join('\n  '));
      if (health) console.error('health:', JSON.stringify(health));
      process.exitCode = 1;
    } else {
      console.log('T164 PASS: live filed marker dual-write dismiss + history-safe + zombie residual');
      console.log('  aside ids dual-written then DELETE:', posts.join(', '));
      console.log('  deletes:', deletes.join(', '));
    }
  } catch (err) {
    console.error('T164 ERROR:', err && err.stack ? err.stack : err);
    process.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
})();
