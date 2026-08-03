// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for clip-collapse of oversized chat bubbles
// (🎯T106 + T66/T77). Serves the static web/ UI, drives addMsg() and the
// streaming append/seal path with short + huge content for both roles, and
// asserts the clip model (not the old truncated re-parse preview):
//   * tallness = full rendered height > COLLAPSED_MAX_HEIGHT × 1.5
//   * collapsed tall bubbles keep FULL HTML in the DOM (tables/lists intact)
//   * container is height-capped (max-height / overflow hidden / .msg-clipped)
//   * bottom pocket scrim (.msg-clip-fade) present when collapsed (darken)
//   * chevron tab (.msg-expand-tab) only — no "Show more" / "Show less" text
//   * short bubble has no tab and no fade
//   * timestamp outside bubble box; tab tongue inside bottom edge
//   * scrim height matches bubble --radius (short edge cue, not multi-line)
//   * T66: latest request/response stay expanded when tall (incl. stream
//     that grows short→tall)
//   * T77: when either role ceases to be latest, auto-expand reverts to clip
// Screenshots the collapsed and expanded states into artifacts/.
//
//   node scripts/chat-ui-test/collapse-test.js [--headed]

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
        res.end(JSON.stringify({ version: 'collapse-test', ok: true }));
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

function assertNoShowMoreLess(text, label, failures) {
  if (/show\s*more|show\s*less/i.test(text || '')) {
    failures.push(`${label}: found Show more/less text: ${JSON.stringify(text)}`);
  }
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.addMsg === 'function' && !!window.marked, null, { timeout: 10000 });
    // Collapse asserts structural DOM on all bubbles; stub virtualisation so
    // T56 dematerialise cannot empty off-screen bodies mid-assert (virt is
    // covered by virtual-list-test.js).
    await page.evaluate(() => {
      window.virtualizeMessages = function () {};
      window.dematerializeMsg = function () {};
      window.scheduleVirtualize = function () {};
    });

    // ── Scenario A (T106/T66): short + middle huge (both roles) + latest huge assistant ──
    // Build: short reply, a huge assistant list, a huge user block, then
    // a huge assistant "latest". Middle huge ones are NOT the last message
    // → clipped full render; the LAST one auto-expands (T66).
    await page.evaluate(() => {
      const N = 60;
      const bigList = Array.from({ length: N }, (_, i) => `- bullseye__bullseye_tool_${i}`).join('\n');
      const bigUser = Array.from({ length: 80 }, (_, i) => `recap line ${i}: durable conversation record continues`).join('\n');
      const latestList = Array.from({ length: N }, (_, i) => `- latest__item_${i}`).join('\n');
      // Table-first tall content: clip must keep thead/header cells in DOM.
      const tableMd = [
        '| ID | Name | Notes |',
        '| --- | --- | --- |',
        ...Array.from({ length: 40 }, (_, i) => `| ${i} | item_${i} | note ${i} |`),
      ].join('\n');
      window.addMsg('jevons', 'Short reply, nothing to collapse.');
      const jBig = window.addMsg('jevons', '### bullseye\n' + bigList);
      const uBig = window.addMsg('user', bigUser);
      const jTable = window.addMsg('jevons', '### inventory\n\n' + tableMd);
      const latest = window.addMsg('jevons', '### latest\n' + latestList);
      window._t = { N, uFullLen: bigUser.length };
      window._els = { jBig, uBig, jTable, latest, short: document.querySelectorAll('#messages .msg.jevons')[0] };
    });
    // Auto-expand-latest is debounced (rAF) — let it settle.
    await page.waitForTimeout(200);

    const state = await page.evaluate(() => {
      const { jBig, uBig, jTable, latest, short } = window._els;
      const jBodyStyle = getComputedStyle(jBig._body);
      const uBodyStyle = getComputedStyle(uBig._body);
      const jBorder = getComputedStyle(jBig).borderTopWidth;
      const jBg = getComputedStyle(jBig).backgroundColor;
      const pageText = document.getElementById('messages').innerText || '';
      const jFade = jBig.querySelector('.msg-clip-fade');
      const jFadeBg = jFade ? getComputedStyle(jFade).backgroundImage : '';
      const jMsgRect = jBig.getBoundingClientRect();
      const jTime = jBig.querySelector('.msg-time');
      const jTimeRect = jTime ? jTime.getBoundingClientRect() : null;
      const jTabRect = jBig._expandBtn ? jBig._expandBtn.getBoundingClientRect() : null;
      const jFadeRect = jFade ? jFade.getBoundingClientRect() : null;
      const shortFade = short.querySelector('.msg-clip-fade');
      return {
        shortHasBtn: !!short.querySelector('.msg-expand-tab'),
        shortHasOldBtn: !!short.querySelector('.msg-expand:not(.msg-expand-tab)'),
        shortHasFade: !!shortFade && getComputedStyle(shortFade).display !== 'none',
        shortExpandBtn: !!short._expandBtn,
        // middle huge ones: full DOM, clipped container
        jItems: jBig._body.querySelectorAll('li').length,
        jFullN: window._t.N,
        jHasBtn: !!jBig._expandBtn,
        jClipped: jBig.classList.contains('msg-clipped'),
        jExpanded: jBig._expanded === true,
        jMaxH: jBodyStyle.maxHeight,
        jOverflow: jBodyStyle.overflow,
        jHasFade: !!jFade && getComputedStyle(jFade).display !== 'none',
        jFadeBg,
        jBtnClass: jBig._expandBtn ? jBig._expandBtn.className : '',
        jBtnText: jBig._expandBtn ? jBig._expandBtn.textContent.trim() : '',
        jAria: jBig._expandBtn ? jBig._expandBtn.getAttribute('aria-label') : '',
        // Pocket geometry: time outside border box; tab tongue inside bottom edge
        // (bottom flush; top above msg bottom); fade bottom flush + height ≤ radius.
        jTimeOutside: jTimeRect ? jTimeRect.top >= jMsgRect.bottom - 1 : false,
        jTabInside: jTabRect
          ? (Math.abs(jTabRect.bottom - jMsgRect.bottom) <= 3 && jTabRect.top < jMsgRect.bottom - 2)
          : false,
        jFadeFlush: jFadeRect ? Math.abs(jFadeRect.bottom - jMsgRect.bottom) <= 2 : false,
        jFadeH: jFadeRect ? jFadeRect.height : 0,
        jRadiusPx: parseFloat(getComputedStyle(jBig).borderBottomLeftRadius) || 0,
        jPadBottom: getComputedStyle(jBig).paddingBottom,
        uLines: uBig._body.textContent.split('\n').length,
        uFullLen: window._t.uFullLen,
        uBodyLen: uBig._body.textContent.length,
        uExpanded: uBig._expanded === true,
        uClipped: uBig.classList.contains('msg-clipped'),
        uHasBtn: !!uBig._expandBtn,
        uMaxH: uBodyStyle.maxHeight,
        uOverflow: uBodyStyle.overflow,
        // table: header cells must remain when collapsed (clip model)
        tableTh: jTable._body.querySelectorAll('th').length,
        tableThText: Array.from(jTable._body.querySelectorAll('th')).map((th) => th.textContent.trim()),
        tableClipped: jTable.classList.contains('msg-clipped'),
        tableRows: jTable._body.querySelectorAll('tr').length,
        // latest huge assistant: auto-EXPANDED (full items), no clip
        latestItems: latest._body.querySelectorAll('li').length,
        latestExpanded: latest._expanded === true,
        latestClipped: latest.classList.contains('msg-clipped'),
        latestLabel: latest._expandBtn ? latest._expandBtn.getAttribute('aria-label') : '',
        latestBtnText: latest._expandBtn ? latest._expandBtn.textContent.trim() : '',
        // assistant geometry: border present, no opaque fill
        jBorderPx: parseFloat(jBorder) || 0,
        jBg,
        pageHasShowMoreLess: /show\s*more|show\s*less/i.test(pageText),
      };
    });

    if (state.shortHasBtn) failures.push('short bubble sprouted an expand tab (should not)');
    if (state.shortHasOldBtn) failures.push('short bubble has legacy .msg-expand control');
    if (state.shortHasFade) failures.push('short bubble has visible .msg-clip-fade (should not)');
    if (state.shortExpandBtn) failures.push('short bubble has _expandBtn ref (should not)');
    if (!state.jHasBtn) failures.push('huge assistant bubble has no expand tab');
    if (state.jItems < state.jFullN) {
      failures.push(`collapsed assistant DOM has ${state.jItems}/${state.jFullN} items — want full render under clip`);
    }
    if (!state.jClipped) failures.push('non-latest huge assistant missing .msg-clipped');
    if (state.jExpanded) failures.push('T77: non-latest huge assistant is expanded (want clipped)');
    if (!state.jMaxH || state.jMaxH === 'none') failures.push(`collapsed assistant max-height = ${state.jMaxH}, want capped`);
    if (state.jOverflow !== 'hidden') failures.push(`collapsed assistant overflow = ${state.jOverflow}, want hidden`);
    if (!state.jHasFade) failures.push('collapsed assistant missing .msg-clip-fade gradient');
    // Pocket scrim must darken (rgba black), not dissolve into var(--bg)/page.
    if (state.jFadeBg && !/rgba?\(\s*0\s*,\s*0\s*,\s*0/i.test(state.jFadeBg)) {
      failures.push(`pocket fade backgroundImage = ${JSON.stringify(state.jFadeBg)}, want darken via rgba(0,0,0,…)`);
    }
    if (state.jFadeBg && /var\(--bg\)|var\(--user-bg\)/i.test(state.jFadeBg)) {
      failures.push('pocket fade still references --bg/--user-bg (fade-to-page, not dark scrim)');
    }
    if (!state.jTimeOutside) failures.push('timestamp still inside bubble box (want outside / under border)');
    if (!state.jTabInside) failures.push('expand tab not inside bottom edge (want tongue into pocket, not hang-off)');
    if (!state.jFadeFlush) failures.push('clip fade not flush to inner bottom of bubble border');
    if (state.jFadeH > 0 && state.jRadiusPx > 0 && state.jFadeH > state.jRadiusPx + 1) {
      failures.push(`clip fade height ${state.jFadeH}px > bubble radius ${state.jRadiusPx}px (want height: var(--radius))`);
    }
    if (state.jPadBottom && state.jPadBottom !== '0px') {
      failures.push(`clipped bubble padding-bottom = ${state.jPadBottom}, want 0 so border butts content`);
    }
    if (!/msg-expand-tab/.test(state.jBtnClass || '')) failures.push(`expand control class = ${JSON.stringify(state.jBtnClass)}, want msg-expand-tab`);
    assertNoShowMoreLess(state.jBtnText, 'assistant tab', failures);
    if (!/expand/i.test(state.jAria || '')) failures.push(`collapsed tab aria-label = ${JSON.stringify(state.jAria)}, want Expand`);

    if (state.uBodyLen < state.uFullLen) failures.push('collapsed user body text shorter than full — re-parse preview?');
    if (state.uLines < 80) failures.push(`collapsed user DOM has ${state.uLines} lines — want full 80 under clip`);
    if (!state.uClipped) failures.push('non-latest huge user missing .msg-clipped');
    if (state.uExpanded) failures.push('T77: non-latest huge user is expanded (want clipped)');
    if (!state.uHasBtn) failures.push('T77: non-latest huge user has no expand tab');
    if (!state.uMaxH || state.uMaxH === 'none') failures.push(`collapsed user max-height = ${state.uMaxH}, want capped`);
    if (state.uOverflow !== 'hidden') failures.push(`collapsed user overflow = ${state.uOverflow}, want hidden`);

    if (state.tableTh < 3) failures.push(`collapsed table has ${state.tableTh} th cells — want full header under clip`);
    if (!state.tableThText.includes('ID') || !state.tableThText.includes('Name') || !state.tableThText.includes('Notes')) {
      failures.push(`collapsed table headers = ${JSON.stringify(state.tableThText)}, want ID/Name/Notes`);
    }
    if (!state.tableClipped) failures.push('tall table bubble not clipped');
    if (state.tableRows < 40) failures.push(`collapsed table rows = ${state.tableRows}, want full structure`);

    if (!state.latestExpanded) failures.push('latest message is NOT auto-expanded');
    if (state.latestClipped) failures.push('latest auto-expanded message still has .msg-clipped');
    if (state.latestItems < state.jFullN) failures.push(`latest auto-expand rendered ${state.latestItems} of ${state.jFullN} items — not full`);
    if (!/collapse/i.test(state.latestLabel || '')) {
      failures.push(`latest tab aria-label = ${JSON.stringify(state.latestLabel)}, want Collapse`);
    }
    assertNoShowMoreLess(state.latestBtnText, 'latest tab', failures);
    if (state.pageHasShowMoreLess) failures.push('messages pane still contains Show more/Show less text');

    // Border-only assistant: non-zero border; transparent/no solid fill.
    if (state.jBorderPx < 1) failures.push(`assistant bubble border width = ${state.jBorderPx}, want ≥1px`);
    if (state.jBg && !/rgba?\(\s*0\s*,\s*0\s*,\s*0\s*,\s*0\s*\)|transparent/i.test(state.jBg)) {
      // allow fully transparent; solid fills fail
      const m = state.jBg.match(/rgba?\(([^)]+)\)/);
      if (m) {
        const parts = m[1].split(',').map((s) => parseFloat(s.trim()));
        if (parts.length >= 4 && parts[3] > 0.05) {
          failures.push(`assistant bubble has fill background ${state.jBg} — want border-only/transparent`);
        } else if (parts.length === 3) {
          failures.push(`assistant bubble has solid background ${state.jBg} — want border-only/transparent`);
        }
      }
    }

    await page.screenshot({ path: path.join(OUT_DIR, 'collapse-preview.png'), fullPage: true });

    // Expand the (clipped, non-latest) assistant bubble — DOM item count
    // stays full; only clip class drops.
    await page.locator('#messages .msg.jevons').nth(1).locator('.msg-expand-tab').click();
    const expanded = await page.evaluate(() => {
      const j = window._els.jBig;
      return {
        items: j._body.querySelectorAll('li').length,
        clipped: j.classList.contains('msg-clipped'),
        expanded: j._expanded === true,
        label: j._expandBtn ? j._expandBtn.getAttribute('aria-label') : '',
        btnText: j._expandBtn ? j._expandBtn.textContent.trim() : '',
        maxH: getComputedStyle(j._body).maxHeight,
      };
    });
    if (expanded.items < 60) failures.push(`after expand, only ${expanded.items} of 60 items in DOM`);
    if (expanded.clipped) failures.push('after expand, bubble still .msg-clipped');
    if (!expanded.expanded) failures.push('after expand, _expanded is false');
    if (!/collapse/i.test(expanded.label || '')) {
      failures.push(`toggle aria after expand = ${JSON.stringify(expanded.label)}, want Collapse`);
    }
    assertNoShowMoreLess(expanded.btnText, 'expanded tab', failures);
    if (expanded.maxH && expanded.maxH !== 'none') {
      // expanded body should not keep the clip max-height
      failures.push(`after expand, body max-height still ${expanded.maxH}`);
    }

    await page.screenshot({ path: path.join(OUT_DIR, 'collapse-expanded.png'), fullPage: true });

    // ── Scenario B (T66 stream): small first chunk, grow until tall, seal ──
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      window.appendOrAddJevons('hi'); // small open
    });
    await page.waitForTimeout(50);
    await page.evaluate(() => {
      const big = Array.from({ length: 60 }, (_, i) => `- stream__item_${i}`).join('\n');
      window.appendOrAddJevons('\n### stream\n' + big);
    });
    await page.waitForTimeout(100);
    // Mid-stream must not be clipped.
    const midStream = await page.evaluate(() => {
      const el = document.querySelector('#messages .msg.jevons');
      return {
        clipped: el && el.classList.contains('msg-clipped'),
        streaming: el && typeof el._streamRaw === 'string',
      };
    });
    if (midStream.clipped) failures.push('T150/T106: live stream bubble is .msg-clipped mid-stream');

    await page.evaluate(() => { window.sealAssistantStream(); });
    await page.waitForTimeout(200);

    const streamState = await page.evaluate(() => {
      const el = document.querySelector('#messages .msg.jevons');
      if (!el) return { missing: true };
      return {
        expanded: el._expanded === true,
        auto: el._autoExpanded === true,
        items: el._body.querySelectorAll('li').length,
        label: el._expandBtn ? el._expandBtn.getAttribute('aria-label') : '',
        btnText: el._expandBtn ? el._expandBtn.textContent.trim() : '',
        hasFull: el._fullText != null,
        clipped: el.classList.contains('msg-clipped'),
      };
    });
    if (streamState.missing) failures.push('T66 stream: no assistant bubble after seal');
    else {
      if (!streamState.hasFull) failures.push('T66 stream: sealed bubble has no _fullText (not oversized?)');
      if (!streamState.expanded) failures.push('T66 stream: latest assistant after small→large stream is NOT expanded');
      if (!streamState.auto) failures.push('T66 stream: latest assistant missing _autoExpanded');
      if (streamState.items < 60) failures.push(`T66 stream: only ${streamState.items}/60 items rendered after auto-expand`);
      if (streamState.clipped) failures.push('T66 stream: latest sealed tall bubble is clipped');
      if (!/collapse/i.test(streamState.label || '')) {
        failures.push(`T66 stream: aria-label = ${JSON.stringify(streamState.label)}, want Collapse`);
      }
      assertNoShowMoreLess(streamState.btnText, 'T66 stream tab', failures);
    }

    // ── Scenario C (T77): large user then large assistant — user collapses ──
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const bigUser = Array.from({ length: 80 }, (_, i) => `user line ${i}`).join('\n');
      const bigAsst = Array.from({ length: 60 }, (_, i) => `- asst_${i}`).join('\n');
      const u = window.addMsg('user', bigUser);
      window._els2 = { u, asst: null, bigAsst };
      window._els2.asstText = '### reply\n' + bigAsst;
    });
    await page.waitForTimeout(150);
    const afterUser = await page.evaluate(() => {
      const u = window._els2.u;
      return { expanded: u._expanded === true, auto: u._autoExpanded === true };
    });
    if (!afterUser.expanded) failures.push('T77 setup: latest huge user did not auto-expand');

    await page.evaluate(() => {
      window._els2.asst = window.addMsg('jevons', window._els2.asstText);
    });
    await page.waitForTimeout(200);

    const afterAsst = await page.evaluate(() => {
      const { u, asst } = window._els2;
      return {
        uExpanded: u._expanded === true,
        uAuto: u._autoExpanded === true,
        uClipped: u.classList.contains('msg-clipped'),
        uFullLines: u._body.textContent.split('\n').length,
        aExpanded: asst._expanded === true,
        aItems: asst._body.querySelectorAll('li').length,
        aLabel: asst._expandBtn ? asst._expandBtn.getAttribute('aria-label') : '',
        aClipped: asst.classList.contains('msg-clipped'),
      };
    });
    if (afterAsst.uExpanded) failures.push('T77: large user still expanded after newer assistant arrived');
    if (afterAsst.uAuto) failures.push('T77: large user still _autoExpanded after ceasing to be latest');
    if (!afterAsst.uClipped) failures.push('T77: large user not .msg-clipped after collapse');
    if (afterAsst.uFullLines < 80) failures.push('T77: large user lost full body text after clip collapse');
    if (!afterAsst.aExpanded) failures.push('T66/T77: latest large assistant not expanded after displacing user');
    if (afterAsst.aClipped) failures.push('T66: latest assistant is clipped');
    if (afterAsst.aItems < 60) failures.push(`T66: latest assistant rendered ${afterAsst.aItems}/60 items`);
    if (!/collapse/i.test(afterAsst.aLabel || '')) {
      failures.push(`T66: latest assistant aria = ${JSON.stringify(afterAsst.aLabel)}`);
    }

    // ── Scenario D (T77): large assistant then large user — assistant collapses ──
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      const bigAsst = Array.from({ length: 60 }, (_, i) => `- prior_${i}`).join('\n');
      const bigUser = Array.from({ length: 80 }, (_, i) => `next user ${i}`).join('\n');
      const a = window.addMsg('jevons', '### prior\n' + bigAsst);
      window._els3 = { a, u: null, bigUser };
    });
    await page.waitForTimeout(150);
    await page.evaluate(() => {
      window._els3.u = window.addMsg('user', window._els3.bigUser);
    });
    await page.waitForTimeout(200);

    const afterUser2 = await page.evaluate(() => {
      const { a, u } = window._els3;
      return {
        aExpanded: a._expanded === true,
        aAuto: a._autoExpanded === true,
        aItems: a._body.querySelectorAll('li').length,
        aClipped: a.classList.contains('msg-clipped'),
        uExpanded: u._expanded === true,
        uLines: u._body.textContent.split('\n').length,
      };
    });
    if (afterUser2.aExpanded) failures.push('T77: large assistant still expanded after newer user arrived');
    if (afterUser2.aAuto) failures.push('T77: large assistant still _autoExpanded after ceasing to be latest');
    if (afterUser2.aItems < 60) failures.push('T77: non-latest assistant lost full list under clip');
    if (!afterUser2.aClipped) failures.push('T77: non-latest assistant not .msg-clipped');
    if (!afterUser2.uExpanded) failures.push('T77: latest large user not auto-expanded');

    // Manual toggle wins: expand non-latest assistant, then add a new msg —
    // manual expand must survive (not forced closed by auto path on others).
    await page.evaluate(() => {
      const a = window._els3.a;
      a._expanded = true;
      a._userToggled = true;
      window.renderBody(a, a._fullRole, a._fullText);
      window.addMsg('jevons', 'short trailing');
    });
    await page.waitForTimeout(200);
    const manual = await page.evaluate(() => {
      const a = window._els3.a;
      return {
        expanded: a._expanded === true,
        userToggled: a._userToggled === true,
        items: a._body.querySelectorAll('li').length,
        clipped: a.classList.contains('msg-clipped'),
      };
    });
    if (!manual.userToggled) failures.push('manual toggle flag lost');
    if (!manual.expanded) failures.push('T77: manual expand of non-latest was undone by later messages');
    if (manual.items < 60) failures.push('T77: manual expand lost full content after later message');
    if (manual.clipped) failures.push('T77: manual expand still clipped after later message');

  } catch (e) {
    failures.push('exception: ' + e.message);
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL collapse-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - pocket clip collapse (radius scrim + inside tab + time outside), T66/T77, short no tab');
  console.log('screenshots: artifacts/collapse-preview.png, artifacts/collapse-expanded.png');
})();
