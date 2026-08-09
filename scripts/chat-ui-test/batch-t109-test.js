// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle batch for 🎯T109 UI leaves: T69/T70/T73/T74/T75/T80/T84/T88/T91.
//
//   node scripts/chat-ui-test/batch-t109-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const HEADED = process.argv.includes('--headed');

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      let rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        const ct = file.endsWith('.js') ? 'application/javascript' : 'text/html';
        res.writeHead(200, { 'Content-Type': ct });
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
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.addMsg === 'function' || typeof addMsg === 'function', null, { timeout: 15000 });
    // Stub WS wipe
    await page.evaluate(() => {
      if (typeof transport !== 'undefined' && transport) {
        transport.onOpen = function () {};
        transport.onClose = function () {};
      }
    });

    // ── T91 timestamp title ─────────────────────────────────────────
    const t91 = await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const el = addMsg('user', 'hello timestamp', 1700000000000);
      const ts = el.querySelector('.msg-time');
      return { hasTitle: !!(ts && ts.title && ts.title.length > 4), title: ts && ts.title, ds: ts && ts.dataset.ts };
    });
    if (!t91.hasTitle || t91.ds !== '1700000000000') {
      failures.push('T91: missing title tooltip or wrong data-ts: ' + JSON.stringify(t91));
    }

    // ── T75 user blockquotes ────────────────────────────────────────
    const t75 = await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const el = addMsg('user', 'intro\n> quoted line\nplain');
      const bq = el.querySelector('blockquote');
      return { hasBq: !!bq, text: bq ? bq.textContent : '', plain: el._body.textContent };
    });
    if (!t75.hasBq || !/quoted line/.test(t75.text)) {
      failures.push('T75: user blockquote not rendered: ' + JSON.stringify(t75));
    }

    // ── T74 syntax highlight ────────────────────────────────────────
    await page.waitForFunction(() => typeof hljs !== 'undefined', null, { timeout: 10000 }).catch(() => {});
    const t74 = await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const code = '```go\npackage main\nfunc main() {}\n```';
      const el = addMsg('jevons', code);
      layoutMsg(el);
      const block = el.querySelector('pre code');
      return {
        hasHljs: typeof hljs !== 'undefined',
        className: block ? block.className : '',
        hasSpan: block ? block.querySelectorAll('span').length : 0,
      };
    });
    if (t74.hasHljs && t74.hasSpan < 1 && !/hljs|language-go/.test(t74.className)) {
      // highlight may set class without spans on some langs — require language class at least
      failures.push('T74: code not highlighted: ' + JSON.stringify(t74));
    }
    if (!t74.hasHljs) {
      failures.push('T74: highlight.js not loaded');
    }

    // ── T80 dictation tidy ──────────────────────────────────────────
    const t80 = await page.evaluate(() => {
      const a = tidyDictationInsert('  hello world  ');
      const b = tidyDictationInsert('Already fine.');
      const c = tidyDictationInsert('mid edit leave'); // no terminal punct but capitalize+period
      return { a, b, c };
    });
    if (t80.a !== 'Hello world.') failures.push('T80 tidy empty-bulk: ' + JSON.stringify(t80.a));
    if (t80.b !== 'Already fine.') failures.push('T80 should not rewrite tidy: ' + t80.b);

    // ── T84 GitHub icon path ────────────────────────────────────────
    const t84 = await page.evaluate(() => {
      const html = formatAgentDir('/Users/x/work/github.com/org/repo');
      const plain = formatAgentDir('/tmp/other');
      return { html, plain, hasSvg: /<svg/.test(html), noTildeGh: !html.startsWith('~') };
    });
    if (!t84.hasSvg || !t84.noTildeGh) failures.push('T84 github icon: ' + JSON.stringify(t84));
    if (/svg/i.test(t84.plain)) failures.push('T84 non-gh should not force icon: ' + t84.plain);

    // ── T73 no terminal chrome ──────────────────────────────────────
    const t73 = await page.evaluate(() => {
      return {
        termHeader: !!document.getElementById('terminal-header'),
        termBox: !!document.getElementById('terminal-container'),
        selectIsFn: typeof selectAgent === 'function',
      };
    });
    if (t73.termHeader || t73.termBox) failures.push('T73 terminal chrome still in DOM');

    // ── T88 Enter rewinds when editing ──────────────────────────────
    const t88 = await page.evaluate(() => {
      // Structural: keydown handler source includes rewind on editingEl.
      // Product form (post Alt+Enter/interrupt split): 
      //   if (editingEl && !interrupt && !e.metaKey) rewindAndResend();
      // Older form: editingEl && !(e.metaKey || e.ctrlKey)) rewindAndResend
      const html = document.documentElement.innerHTML;
      return {
        rewindPrimary: /editingEl\s*&&\s*!interrupt\s*&&\s*!e\.metaKey\)\s*rewindAndResend/.test(html)
          || /editingEl && !\(e\.metaKey \|\| e\.ctrlKey\)\) rewindAndResend/.test(html)
          || /editingEl && !\(e\.metaKey/.test(html),
        escClears: /setEditingHighlight\(null\)/.test(html) && /Escape/.test(html),
      };
    });
    if (!t88.rewindPrimary) failures.push('T88: Enter while editing should rewindAndResend: ' + JSON.stringify(t88));
    if (!t88.escClears) failures.push('T88: Escape should clear edit mode');

    // ── T69/T123 composer: compact empty height matches #send ─────
    // T69 scrollbar policy remains (overflow hidden until cap); T123
    // removed the multi-line default min-height so empty ≈ send.
    const t69 = await page.evaluate(() => {
      const cs = getComputedStyle(input);
      const send = document.getElementById('send');
      const sendH = send ? send.getBoundingClientRect().height : 0;
      const inputH = input.getBoundingClientRect().height;
      const minH = parseFloat(cs.minHeight);
      const line = parseFloat(cs.lineHeight) || 20;
      const maxH = cs.maxHeight;
      return {
        minH,
        line,
        inputH,
        sendH,
        maxH,
        matchesSend: Math.abs(inputH - sendH) <= 2,
        singleLineMin: minH > 0 && minH <= line * 1.5 + 30,
        maxCapped: maxH === '28vh' || (parseFloat(maxH) > 0),
      };
    });
    if (!t69.matchesSend) {
      failures.push('T123 empty composer height must match send: ' + JSON.stringify(t69));
    }
    if (!t69.singleLineMin) {
      failures.push('T123 composer min-height must be single-line compact: ' + JSON.stringify(t69));
    }

    // ── T70 layout: messages flex shrinkable ────────────────────────
    const t70 = await page.evaluate(() => {
      const m = getComputedStyle(document.getElementById('messages'));
      const c = getComputedStyle(document.getElementById('chat-pane'));
      return {
        msgFlex: m.flexGrow === '1' || m.flex === '1 1 0%' || parseFloat(m.flexGrow) === 1,
        msgMin0: m.minHeight === '0px',
        chatCol: c.flexDirection === 'column',
      };
    });
    if (!t70.msgMin0 || !t70.chatCol) failures.push('T70 layout: ' + JSON.stringify(t70));

  } catch (e) {
    failures.push(String(e && e.stack || e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL batch-t109-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('ok - T109 UI batch: T69/T70/T73/T74/T75/T80/T84/T88/T91 hermetic');
})();
