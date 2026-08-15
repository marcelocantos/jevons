// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright perceptual smoke for 🎯T309.1: main + RHS both mount one
// ConversationWidget (density comfortable | compact). Hermetic static
// server + mocked agents; no live daemon.
//
//   node scripts/chat-ui-test/t309.1-conversation-widget-test.js [--headed]

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

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  const agents = [
    { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer' },
    { name: 'jv-t309.1-worker', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons-po', status: 'running', purpose: 'work' },
    { name: 'jevons-po', workdir: '/Users/x/work/github.com/marcelocantos/jevons', parent: 'jevons', status: 'running', purpose: 'work' },
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
        res.end(JSON.stringify({ version: 't309.1-widget-test', ok: true }));
        return;
      }
      // Agent send / transcript stubs (compact composer path).
      if (u.pathname.startsWith('/api/agents/') && u.pathname.endsWith('/send') && req.method === 'POST') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ status: 'sent' }));
        return;
      }
      if (u.pathname.startsWith('/api/agents/') && u.pathname.endsWith('/transcript')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({
          name: 'jv-t309.1-worker',
          turns: [
            { role: 'user', text: 'hello worker', when: Date.now() - 5000 },
            { role: 'assistant', text: 'hi from worker', when: Date.now() - 4000 },
          ],
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

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.ConversationWidget !== 'undefined', null, { timeout: 10000 });

    // Main host: comfortable density conversation widget (adopts #messages/#input).
    const main = await page.evaluate(() => {
      const pane = document.getElementById('chat-pane');
      return {
        hasClass: !!(pane && pane.classList.contains('conversation-widget')),
        density: pane && (pane.dataset.density
          || (pane.classList.contains('density-comfortable') ? 'comfortable' : '')),
        messages: !!document.getElementById('messages'),
        input: !!document.getElementById('input'),
        send: !!document.getElementById('send'),
        scriptLoaded: typeof ConversationWidget !== 'undefined',
      };
    });
    if (!main.hasClass) failures.push('main #chat-pane missing conversation-widget class');
    if (main.density !== 'comfortable') failures.push('main density expected comfortable, got ' + main.density);
    if (!main.messages || !main.input || !main.send) failures.push('main messages/input/send missing');
    if (!main.scriptLoaded) failures.push('ConversationWidget script not loaded');

    // RHS host markup: compact density.
    const rhs = await page.evaluate(() => {
      const el = document.getElementById('agent-inspect');
      return {
        hasClass: !!(el && el.classList.contains('conversation-widget')),
        density: el && (el.dataset.density || (el.classList.contains('density-compact') ? 'compact' : '')),
        body: !!document.getElementById('agent-inspect-body'),
        composer: !!document.getElementById('agent-inspect-composer'),
        input: !!document.getElementById('agent-inspect-input'),
        send: !!document.getElementById('agent-inspect-send'),
        widgetApi: typeof ConversationWidget !== 'undefined'
          && typeof ConversationWidget.mount === 'function'
          && typeof ConversationWidget.classifyComposerKey === 'function',
      };
    });
    if (!rhs.hasClass) failures.push('RHS #agent-inspect missing conversation-widget class');
    if (rhs.density !== 'compact') failures.push('RHS density expected compact, got ' + rhs.density);
    if (!rhs.body || !rhs.composer || !rhs.input || !rhs.send) failures.push('RHS inspect body/composer missing');
    if (!rhs.widgetApi) failures.push('ConversationWidget API incomplete');

    // Compact key policy is density-param, not a second classifier implementation.
    const key = await page.evaluate(() => {
      return {
        send: ConversationWidget.classifyComposerKey({ key: 'Enter' }, { density: 'compact' }),
        nl: ConversationWidget.classifyComposerKey({ key: 'Enter', shiftKey: true }, { density: 'compact' }),
      };
    });
    if (key.send !== 'send') failures.push('compact Enter should send, got ' + key.send);
    if (key.nl !== 'newline') failures.push('compact Shift+Enter should newline, got ' + key.nl);

    // 🎯T480 main: size-only clip via addMsg → widget layoutSizeClip.
    const t480Main = await page.evaluate(() => {
      const wall = '<user_info>\n'
        + Array.from({ length: 80 }, (_, i) =>
          'Agents.md line ' + i + ': ' + 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx').join('\n')
        + '\n</rules>';
      const tallAsst = '### reply\n' + Array.from({ length: 40 }, (_, i) =>
        '- item_' + i + ' with enough padding to exceed the 14rem clip').join('\n');
      function snap(el) {
        if (!el) return { missing: true };
        return {
          clipped: el.classList.contains('msg-clipped'),
          tab: !!(el._expandBtn || el.querySelector('.msg-expand-tab')),
          bodyH: el._body ? el._body.scrollHeight : 0,
        };
      }
      const msgs = document.getElementById('messages');
      if (msgs) msgs.innerHTML = '';
      const mainShort = window.addMsg ? window.addMsg('user', 'Short.') : null;
      const mainWall = window.addMsg ? window.addMsg('user', wall) : null;
      const mainAsst = window.addMsg ? window.addMsg('jevons', tallAsst) : null;
      return {
        api: typeof ConversationWidget !== 'undefined'
          && typeof ConversationWidget.layoutSizeClip === 'function',
        mainShort: snap(mainShort),
        mainWall: snap(mainWall),
        mainAsst: snap(mainAsst),
      };
    });
    if (!t480Main.api) failures.push('T480: ConversationWidget.layoutSizeClip missing');
    if (t480Main.mainShort.tab || t480Main.mainShort.clipped) {
      failures.push('T480 main short sprouted tab/clip ' + JSON.stringify(t480Main.mainShort));
    }
    // Main T66/T261 may keep in-view tall bubbles expanded — tab is the signal.
    if (!t480Main.mainWall.tab) {
      failures.push('T480 main <user_info> wall missing pocket tab ' + JSON.stringify(t480Main.mainWall));
    }
    if (!t480Main.mainAsst.tab) {
      failures.push('T480 main tall assistant missing pocket tab ' + JSON.stringify(t480Main.mainAsst));
    }

    // Drive selectAgent directly (avoids depending on fleet paint timing).
    const afterSelect = await page.evaluate(() => {
      if (typeof selectAgent === 'function') {
        selectAgent('jv-t309.1-worker', { purpose: 'work' });
      }
      const composer = document.getElementById('agent-inspect-composer');
      const tab = document.getElementById('rhs-tab-transcript');
      return {
        selected: typeof selectedAgent !== 'undefined' ? selectedAgent : null,
        composerVisible: !!(composer && composer.classList.contains('visible') && !composer.hidden),
        transcriptTab: !!(tab && tab.classList.contains('active')),
        inspectActive: !!(document.getElementById('agent-inspect')
          && document.getElementById('agent-inspect').classList.contains('active')),
        hasSelect: typeof selectAgent === 'function',
      };
    });
    if (!afterSelect.hasSelect) {
      failures.push('selectAgent not available on page');
    } else {
      if (afterSelect.selected !== 'jv-t309.1-worker') {
        failures.push('selectAgent did not select worker, got ' + afterSelect.selected);
      }
      if (!afterSelect.composerVisible) failures.push('compact composer not visible after worker select');
      if (!afterSelect.transcriptTab && !afterSelect.inspectActive) {
        failures.push('Transcript tab / inspect pane not active after worker select');
      }
    }

    // 🎯T480 inspect: same size clip after the pane has width (post-select).
    const t480Side = await page.evaluate(() => {
      const wall = '<user_info>\n'
        + Array.from({ length: 80 }, (_, i) =>
          'Agents.md line ' + i + ': ' + 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx').join('\n')
        + '\n</rules>';
      const tallAsst = '### reply\n' + Array.from({ length: 40 }, (_, i) =>
        '- item_' + i + ' with enough padding to exceed the 14rem clip').join('\n');
      function snap(el) {
        if (!el) return { missing: true };
        return {
          clipped: el.classList.contains('msg-clipped'),
          tab: !!(el._expandBtn || el.querySelector('.msg-expand-tab')),
          bodyH: el._body ? el._body.scrollHeight : 0,
          w: el.clientWidth || 0,
        };
      }
      if (typeof renderAgentInspect === 'function') {
        renderAgentInspect({
          title: 'jv-t309.1-worker',
          lines: [
            { role: 'user', text: 'Short.', when: Date.now() - 3000 },
            { role: 'user', text: wall, when: Date.now() - 2000 },
            { role: 'assistant', text: tallAsst, when: Date.now() - 1000 },
          ],
        });
      }
      const body = document.getElementById('agent-inspect-body');
      const sideMsgs = body ? Array.prototype.filter.call(body.children || [], function (c) {
        return c.classList && c.classList.contains('msg');
      }) : [];
      return {
        inspectHasRender: typeof renderAgentInspect === 'function',
        sideCount: sideMsgs.length,
        sideNuggets: body ? body.querySelectorAll('.inject-nugget').length : -1,
        bodyW: body ? body.clientWidth : 0,
        sideShort: snap(sideMsgs[0]),
        sideWall: snap(sideMsgs[1]),
        sideAsst: snap(sideMsgs[2]),
      };
    });
    if (!t480Side.inspectHasRender) failures.push('T480: renderAgentInspect not on page');
    if (t480Side.sideCount !== 3) {
      failures.push('T480 inspect expected 3 bubbles, got ' + t480Side.sideCount);
    }
    if (t480Side.sideNuggets !== 0) {
      failures.push('T480 <user_info> must stay a bubble, not a nugget (count=' + t480Side.sideNuggets + ')');
    }
    if (t480Side.sideShort.tab || t480Side.sideShort.clipped) {
      failures.push('T480 inspect short sprouted tab/clip ' + JSON.stringify(t480Side.sideShort));
    }
    if (!t480Side.sideWall.clipped || !t480Side.sideWall.tab) {
      failures.push('T480 inspect <user_info> wall not clipped+tab ' + JSON.stringify(t480Side.sideWall));
    }
    if (!t480Side.sideAsst.clipped || !t480Side.sideAsst.tab) {
      failures.push('T480 inspect tall assistant not clipped+tab ' + JSON.stringify(t480Side.sideAsst));
    }

    if (failures.length) {
      console.error('FAIL t309.1-conversation-widget-test');
      failures.forEach((f) => console.error('  -', f));
      process.exitCode = 1;
    } else {
      console.log('PASS t309.1-conversation-widget-test (main comfortable + RHS compact mounts)');
    }
  } catch (e) {
    console.error('FAIL t309.1-conversation-widget-test', e && e.stack || e);
    process.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
    srv.close();
  }
})();
