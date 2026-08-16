// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for clip-collapse of oversized chat bubbles
// (🎯T106 + T66/T246 + T166). Serves the static web/ UI, drives addMsg() and the
// streaming append/seal path with short + huge + medium-tall content for
// both roles, and asserts the clip model (not the old truncated re-parse
// preview):
//   * tallness = full rendered height > COLLAPSED_MAX_HEIGHT (+ epsilon)
//     (not 1.5× the clip box — that left medium-tall 14–21rem without tab)
//   * medium-tall (clip < full < 1.5×clip) non-latest → tab + .msg-clipped
//   * collapsed tall bubbles keep FULL HTML in the DOM (tables/lists intact)
//   * container is height-capped (max-height / overflow hidden / .msg-clipped)
//   * bottom pocket scrim (.msg-clip-fade) present when collapsed (darken)
//   * chevron tab (.msg-expand-tab) only — no "Show more" / "Show less" text
//   * short bubble has no tab and no fade
//   * timestamp outside bubble box; tab tongue inside bottom edge
//   * scrim height matches bubble --radius (short edge cue, not multi-line)
//   * T66: latest request/response stay expanded when tall (incl. stream
//     that grows short→tall)
//   * T246: auto-expanded stays open while any part is in the viewport;
//     may collapse only after fully scrolled out of sight (above fold).
//     (Supersedes T77 "collapse when no longer latest".)
//   * T261: after history pin / stick-to-bottom near end, tall messages
//     still in the viewport are expanded (not only the single latest).
//     Mid-replay must not leave end-of-transcript rows collapsed.
//   * T166: expanded+tab reserves bottom padding so last text clears ▲ tab;
//     collapsed msg-clipped stays padding-bottom:0
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
      const jFadeZ = jFade ? parseFloat(getComputedStyle(jFade).zIndex) || 0 : 0;
      const jTabZ = jBig._expandBtn ? parseFloat(getComputedStyle(jBig._expandBtn).zIndex) || 0 : 0;
      const uFade = uBig.querySelector('.msg-clip-fade');
      const uFadeBg = uFade ? getComputedStyle(uFade).backgroundImage : '';
      const uFadeZ = uFade ? parseFloat(getComputedStyle(uFade).zIndex) || 0 : 0;
      const uTabZ = uBig._expandBtn ? parseFloat(getComputedStyle(uBig._expandBtn).zIndex) || 0 : 0;
      // Parse max alpha from linear-gradient(... rgba(0,0,0,A) ...) end stop.
      const maxScrimAlpha = (bg) => {
        if (!bg) return null;
        let max = 0;
        const re = /rgba?\(\s*0\s*,\s*0\s*,\s*0\s*(?:,\s*([0-9.]+)\s*)?\)/gi;
        let m;
        while ((m = re.exec(bg))) {
          const a = m[1] == null ? 1 : parseFloat(m[1]);
          if (!Number.isNaN(a) && a > max) max = a;
        }
        return max;
      };
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
        jFadeZ,
        jTabZ,
        jScrimAlpha: maxScrimAlpha(jFadeBg),
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
        uHasFade: !!uFade && getComputedStyle(uFade).display !== 'none',
        uFadeBg,
        uFadeZ,
        uTabZ,
        uScrimAlpha: maxScrimAlpha(uFadeBg),
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
    if (state.jExpanded) failures.push('T246: off-screen non-latest huge assistant is expanded (want clipped)');
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
    // Soft pocket: max scrim opacity ~half of prior 0.45 (accept 0.15–0.28).
    if (state.jScrimAlpha == null || state.jScrimAlpha < 0.15 || state.jScrimAlpha > 0.28) {
      failures.push(`assistant scrim max alpha = ${state.jScrimAlpha}, want ~0.22 (range 0.15–0.28)`);
    }
    // Tab must stack above scrim for assistant and user (both roles).
    if (!(state.jTabZ > state.jFadeZ)) {
      failures.push(`assistant tab z-index ${state.jTabZ} not above fade ${state.jFadeZ}`);
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
    if (state.uExpanded) failures.push('T246: off-screen non-latest huge user is expanded (want clipped)');
    if (!state.uHasBtn) failures.push('T246: non-latest huge user has no expand tab');
    if (!state.uHasFade) failures.push('collapsed user missing .msg-clip-fade gradient');
    if (state.uFadeBg && !/rgba?\(\s*0\s*,\s*0\s*,\s*0/i.test(state.uFadeBg)) {
      failures.push(`user pocket fade backgroundImage = ${JSON.stringify(state.uFadeBg)}, want darken via rgba(0,0,0,…)`);
    }
    if (state.uScrimAlpha == null || state.uScrimAlpha < 0.15 || state.uScrimAlpha > 0.28) {
      failures.push(`user scrim max alpha = ${state.uScrimAlpha}, want ~0.22 (range 0.15–0.28)`);
    }
    if (!(state.uTabZ > state.uFadeZ)) {
      failures.push(`user tab z-index ${state.uTabZ} not above fade ${state.uFadeZ}`);
    }
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
      const body = j._body;
      const tab = j._expandBtn;
      const padBottom = getComputedStyle(j).paddingBottom;
      const padPx = parseFloat(padBottom) || 0;
      const tabH = tab ? tab.getBoundingClientRect().height : 0;
      const bodyRect = body ? body.getBoundingClientRect() : null;
      const tabRect = tab ? tab.getBoundingClientRect() : null;
      // Gap from body bottom edge to tab top — should be ≥ 0 (text clears tab).
      const bodyTabGap = (bodyRect && tabRect) ? (tabRect.top - bodyRect.bottom) : null;
      // Also measure last text line box vs tab top.
      let lastLineBottom = null;
      if (body) {
        const walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT, null);
        let n;
        while ((n = walker.nextNode())) {
          if (!n.nodeValue || !/\S/.test(n.nodeValue)) continue;
          const range = document.createRange();
          range.selectNodeContents(n);
          const rects = range.getClientRects();
          for (let i = 0; i < rects.length; i++) {
            const r = rects[i];
            if (r.height < 0.5 || r.width < 0.5) continue;
            if (lastLineBottom == null || r.bottom > lastLineBottom) lastLineBottom = r.bottom;
          }
        }
      }
      const lastLineTabGap = (lastLineBottom != null && tabRect)
        ? (tabRect.top - lastLineBottom)
        : null;
      return {
        items: body.querySelectorAll('li').length,
        clipped: j.classList.contains('msg-clipped'),
        expanded: j._expanded === true,
        hasTab: !!tab,
        label: tab ? tab.getAttribute('aria-label') : '',
        btnText: tab ? tab.textContent.trim() : '',
        maxH: getComputedStyle(body).maxHeight,
        padBottom,
        padPx,
        tabH,
        bodyTabGap,
        lastLineTabGap,
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
    // 🎯T166: expanded tall + tab → bottom padding clears collapse tab.
    if (!expanded.hasTab) failures.push('T166: expanded tall bubble lost expand tab');
    if (!(expanded.padPx >= expanded.tabH - 0.5)) {
      failures.push(
        `T166: expanded padding-bottom ${expanded.padBottom} (${expanded.padPx}px) < tab height ${expanded.tabH}px`
      );
    }
    if (expanded.lastLineTabGap == null || expanded.lastLineTabGap < -0.5) {
      failures.push(
        `T166: last text line overlaps collapse tab (gap=${expanded.lastLineTabGap})`
      );
    }
    if (expanded.bodyTabGap != null && expanded.bodyTabGap < -0.5) {
      failures.push(
        `T166: msg-body bottom overlaps tab top (gap=${expanded.bodyTabGap})`
      );
    }

    // T166: latest auto-expanded tall also clears tab; short still no tab.
    const t166Latest = await page.evaluate(() => {
      const latest = window._els.latest;
      const short = window._els.short;
      const tab = latest && latest._expandBtn;
      const padPx = latest ? (parseFloat(getComputedStyle(latest).paddingBottom) || 0) : 0;
      const tabH = tab ? tab.getBoundingClientRect().height : 0;
      const tabRect = tab ? tab.getBoundingClientRect() : null;
      let lastLineBottom = null;
      if (latest && latest._body) {
        const walker = document.createTreeWalker(latest._body, NodeFilter.SHOW_TEXT, null);
        let n;
        while ((n = walker.nextNode())) {
          if (!n.nodeValue || !/\S/.test(n.nodeValue)) continue;
          const range = document.createRange();
          range.selectNodeContents(n);
          const rects = range.getClientRects();
          for (let i = 0; i < rects.length; i++) {
            const r = rects[i];
            if (r.height < 0.5 || r.width < 0.5) continue;
            if (lastLineBottom == null || r.bottom > lastLineBottom) lastLineBottom = r.bottom;
          }
        }
      }
      const lastLineTabGap = (lastLineBottom != null && tabRect)
        ? (tabRect.top - lastLineBottom)
        : null;
      return {
        latestExpanded: latest && latest._expanded === true,
        latestClipped: latest && latest.classList.contains('msg-clipped'),
        latestHasTab: !!tab,
        padPx,
        tabH,
        lastLineTabGap,
        shortHasTab: !!(short && (short._expandBtn || short.querySelector('.msg-expand-tab'))),
      };
    });
    if (!t166Latest.latestExpanded || t166Latest.latestClipped) {
      failures.push('T166 setup: latest tall not expanded without .msg-clipped');
    }
    if (!t166Latest.latestHasTab) failures.push('T166: latest tall missing collapse tab');
    if (!(t166Latest.padPx >= t166Latest.tabH - 0.5)) {
      failures.push(
        `T166: latest expanded pad-bottom ${t166Latest.padPx}px < tab ${t166Latest.tabH}px`
      );
    }
    if (t166Latest.lastLineTabGap == null || t166Latest.lastLineTabGap < -0.5) {
      failures.push(
        `T166: latest last line overlaps tab (gap=${t166Latest.lastLineTabGap})`
      );
    }
    if (t166Latest.shortHasTab) failures.push('T166: short bubble grew expand tab');

    await page.screenshot({ path: path.join(OUT_DIR, 'collapse-expanded.png'), fullPage: true });

    // ── Scenario B (T66 stream): small first chunk, grow until tall, seal ──
    await page.evaluate(() => {
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else document.getElementById('messages').innerHTML = '';
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

    // ── Scenario C (T246): large user then large assistant with Track pin ──
    // Stick-to-bottom puts the prior fully above the fold → auto-collapse.
    // Explicit enterTrackBottom: earlier scenarios may leave Free mode.
    await page.evaluate(() => {
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else document.getElementById('messages').innerHTML = '';
      if (window.enterTrackBottom) window.enterTrackBottom();
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
    if (!afterUser.expanded) failures.push('T246 setup: latest huge user did not auto-expand');

    await page.evaluate(() => {
      if (window.enterTrackBottom) window.enterTrackBottom();
      window._els2.asst = window.addMsg('jevons', window._els2.asstText);
    });
    await page.waitForTimeout(200);
    // Ensure pin + off-screen collapse settled (rAF + late layout).
    await page.evaluate(() => {
      if (window.enterTrackBottom) window.enterTrackBottom();
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
    });

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
    // Two tall bubbles + pin bottom → prior is off-screen and collapses.
    if (afterAsst.uExpanded) failures.push('T246: large user still expanded after newer tall assistant pinned bottom');
    if (afterAsst.uAuto) failures.push('T246: large user still _autoExpanded after off-screen collapse');
    if (!afterAsst.uClipped) failures.push('T246: large user not .msg-clipped after off-screen collapse');
    if (afterAsst.uFullLines < 80) failures.push('T246: large user lost full body text after clip collapse');
    if (!afterAsst.aExpanded) failures.push('T66/T246: latest large assistant not expanded after displacing user');
    if (afterAsst.aClipped) failures.push('T66: latest assistant is clipped');
    if (afterAsst.aItems < 60) failures.push(`T66: latest assistant rendered ${afterAsst.aItems}/60 items`);
    if (!/collapse/i.test(afterAsst.aLabel || '')) {
      failures.push(`T66: latest assistant aria = ${JSON.stringify(afterAsst.aLabel)}`);
    }

    // ── Scenario D (T246): large assistant then large user — assistant off-screen collapses ──
    await page.evaluate(() => {
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else document.getElementById('messages').innerHTML = '';
      if (window.enterTrackBottom) window.enterTrackBottom();
      const bigAsst = Array.from({ length: 60 }, (_, i) => `- prior_${i}`).join('\n');
      const bigUser = Array.from({ length: 80 }, (_, i) => `next user ${i}`).join('\n');
      const a = window.addMsg('jevons', '### prior\n' + bigAsst);
      window._els3 = { a, u: null, bigUser };
    });
    await page.waitForTimeout(150);
    await page.evaluate(() => {
      if (window.enterTrackBottom) window.enterTrackBottom();
      window._els3.u = window.addMsg('user', window._els3.bigUser);
    });
    await page.waitForTimeout(200);
    await page.evaluate(() => {
      if (window.enterTrackBottom) window.enterTrackBottom();
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
    });

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
    if (afterUser2.aExpanded) failures.push('T246: large assistant still expanded after newer tall user pinned bottom');
    if (afterUser2.aAuto) failures.push('T246: large assistant still _autoExpanded after off-screen collapse');
    if (afterUser2.aItems < 60) failures.push('T246: non-latest assistant lost full list under clip');
    if (!afterUser2.aClipped) failures.push('T246: non-latest assistant not .msg-clipped when off-screen');
    if (!afterUser2.uExpanded) failures.push('T246: latest large user not auto-expanded');

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
    if (!manual.expanded) failures.push('T246: manual expand of non-latest was undone by later messages');
    if (manual.items < 60) failures.push('T246: manual expand lost full content after later message');
    if (manual.clipped) failures.push('T246: manual expand still clipped after later message');

    // ── Scenario C2 (T246 core) ─────────────────────────────────────────
    // Phase 1: tall + short only; leave track; keep prior partially visible
    // → stays auto-expanded (not "collapse when no longer latest").
    // Phase 2: free-scroll fillers for room, then scroll prior fully above
    // → may collapse.
    await page.evaluate(() => {
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else document.getElementById('messages').innerHTML = '';
      if (window.enterTrackBottom) window.enterTrackBottom();
      const tall = Array.from({ length: 50 }, (_, i) => `- stay_${i}`).join('\n');
      const prior = window.addMsg('jevons', '### stay open\n' + tall);
      window._t246 = { prior };
    });
    await page.waitForTimeout(150);
    await page.evaluate(() => {
      window._t246.short = window.addMsg('user', 'short follow-up that does not fill the viewport alone');
    });
    await page.waitForTimeout(200);
    const t246Stay = await page.evaluate(() => {
      const el = document.getElementById('messages');
      const p = window._t246.prior;
      if (window.leaveTrackBottom) window.leaveTrackBottom();
      // Keep prior partially in view (top slightly above scrollTop).
      const target = Math.max(0, Math.min(
        p.offsetTop + Math.floor(p.offsetHeight * 0.35),
        Math.max(0, el.scrollHeight - el.clientHeight - 1),
      ));
      el.scrollTop = target;
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
      const top = p.offsetTop;
      const bot = top + p.offsetHeight;
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      return {
        anyVisible: bot > viewTop && top < viewBot,
        fullyAbove: bot <= viewTop,
        expanded: p._expanded === true,
        auto: p._autoExpanded === true,
        clipped: p.classList.contains('msg-clipped'),
        top, bot, viewTop, viewBot,
      };
    });
    if (!t246Stay.anyVisible || t246Stay.fullyAbove) {
      failures.push('T246 stay-expanded setup: prior not partially in viewport ' + JSON.stringify(t246Stay));
    } else {
      if (!t246Stay.expanded) failures.push('T246: prior tall collapsed while still partially on-screen');
      if (!t246Stay.auto) failures.push('T246: prior tall lost _autoExpanded while still on-screen');
      if (t246Stay.clipped) failures.push('T246: prior tall .msg-clipped while still on-screen');
    }

    // Phase 2: free-scroll filler bulk (no stick-to-bottom pin), then scroll out.
    const t246Off = await page.evaluate(() => {
      const el = document.getElementById('messages');
      const p = window._t246.prior;
      if (window.leaveTrackBottom) window.leaveTrackBottom();
      // Ensure prior is still the auto-expanded subject (fillers become latest).
      if (!p._autoExpanded) {
        p._autoExpanded = true;
        p._expanded = true;
        p._userToggled = false;
        if (p._fullText != null) window.renderBody(p, p._fullRole, p._fullText);
      }
      for (let i = 0; i < 14; i++) {
        window.addMsg('jevons', 'filler block ' + i + '\n\n' + Array.from({ length: 18 }, (_, j) => 'line ' + j).join('\n'));
      }
      // scrollDown is a no-op while Free; still force free.
      if (window.leaveTrackBottom) window.leaveTrackBottom();
      el.scrollTop = p.offsetTop + p.offsetHeight + 80;
      if (p.offsetTop + p.offsetHeight > el.scrollTop) {
        el.scrollTop = el.scrollHeight;
      }
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
      const bot = p.offsetTop + p.offsetHeight;
      return {
        fullyAbove: bot <= el.scrollTop,
        expanded: p._expanded === true,
        auto: p._autoExpanded === true,
        clipped: p.classList.contains('msg-clipped'),
        items: p._body.querySelectorAll('li').length,
        scrollTop: el.scrollTop,
        bot,
        maxScroll: el.scrollHeight - el.clientHeight,
      };
    });
    if (!t246Off.fullyAbove) {
      failures.push('T246 off-screen setup failed to place prior above fold ' + JSON.stringify(t246Off));
    } else {
      if (t246Off.expanded) failures.push('T246: prior tall still expanded after fully scrolled out of view');
      if (t246Off.auto) failures.push('T246: prior tall still _autoExpanded when fully off-screen');
      if (!t246Off.clipped) failures.push('T246: prior tall not .msg-clipped after leaving viewport');
      if (t246Off.items < 50) failures.push('T246: clipped prior lost full list under clip model');
    }

    // ── Scenario E (T106 tallness gate): medium-tall > clip box, < 1.5× clip ──
    // Old 1.5× ratio against fixed 14rem left these without tab/.msg-clipped.
    // Fixture lines tuned so measureCollapse fullH sits in
    // (collapsedH+eps, collapsedH×1.5). Layout drift out of band → fail with
    // heights so the fixture can be retuned (not silently test huge path).
    // 🎯T261: when near end these stay in the viewport → expanded. Clip chrome
    // is checked after free-scroll pushes them fully above the fold.
    await page.evaluate(() => {
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else document.getElementById('messages').innerHTML = '';
      if (window.enterTrackBottom) window.enterTrackBottom();
      // ~12 user lines at default metrics ≈ 14–20rem (under old 1.5× gate).
      const medUser = Array.from({ length: 12 }, (_, i) =>
        `medium user line ${i}: request body exceeds the 14rem clip pocket`).join('\n');
      // Live list metrics: 6 items ≈ 178px < 14rem clip. 10 items lands in
      // (collapsedH+eps, 1.5×collapsedH) under the T480 live-height measure.
      const medAsst = Array.from({ length: 10 }, (_, i) =>
        `- medium_item_${i} with a bit of padding`).join('\n');
      const uMed = window.addMsg('user', medUser);
      const jMed = window.addMsg('jevons', '### medium\n' + medAsst);
      // Trailing short so neither medium bubble is latest (T66 expand would
      // mask clip-off-screen). Short must not be tall.
      const trail = window.addMsg('jevons', 'Short trailing reply.');
      window._elsMed = { uMed, jMed, trail, medUser, medAsst };
    });
    await page.waitForTimeout(200);
    await page.evaluate(() => {
      if (window.enterTrackBottom) window.enterTrackBottom();
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
    });

    const medium = await page.evaluate(() => {
      const { uMed, jMed, trail, medUser, medAsst } = window._elsMed;
      const el = document.getElementById('messages');
      // Same probe path as production (classic script → window.measureCollapse).
      const um = typeof window.measureCollapse === 'function'
        ? window.measureCollapse(uMed, 'user', medUser)
        : null;
      const jm = typeof window.measureCollapse === 'function'
        ? window.measureCollapse(jMed, 'jevons', '### medium\n' + medAsst)
        : null;
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      const inView = (m) => {
        const top = m.offsetTop;
        const bot = top + m.offsetHeight;
        return bot > viewTop && top < viewBot;
      };
      return {
        um,
        jm,
        uInView: inView(uMed),
        jInView: inView(jMed),
        uClipped: uMed.classList.contains('msg-clipped'),
        jClipped: jMed.classList.contains('msg-clipped'),
        uHasBtn: !!uMed._expandBtn,
        jHasBtn: !!jMed._expandBtn,
        uExpanded: uMed._expanded === true,
        jExpanded: jMed._expanded === true,
        trailHasBtn: !!trail._expandBtn,
        trailClipped: trail.classList.contains('msg-clipped'),
      };
    });

    if (!medium.um || !medium.jm) {
      failures.push('Scenario E: measureCollapse not on window — cannot band-check medium fixtures');
    } else {
      const band = (m, label) => {
        if (!m.tall) {
          failures.push(`${label}: measureCollapse.tall=false (fullH=${m.fullH}, collapsedH=${m.collapsedH}) — fixture too short`);
          return;
        }
        // Must sit under the old 1.5× gate so this scenario proves the fix.
        if (m.fullH >= m.collapsedH * 1.5) {
          failures.push(
            `${label}: fullH=${m.fullH} >= 1.5×collapsedH=${m.collapsedH * 1.5} — fixture too huge; retune for medium band`
          );
        }
      };
      band(medium.um, 'medium-tall user');
      band(medium.jm, 'medium-tall assistant');
    }
    // Near end + in view → expanded (T261), still tall enough for expand tab.
    if (medium.uInView) {
      if (!medium.uExpanded) failures.push('T261: medium-tall user in view near end is collapsed');
      if (medium.uClipped) failures.push('T261: medium-tall user in view near end still .msg-clipped');
      if (!medium.uHasBtn) failures.push('medium-tall user missing expand tab (tall chrome)');
    }
    if (medium.jInView) {
      if (!medium.jExpanded) failures.push('T261: medium-tall assistant in view near end is collapsed');
      if (medium.jClipped) failures.push('T261: medium-tall assistant in view near end still .msg-clipped');
      if (!medium.jHasBtn) failures.push('medium-tall assistant missing expand tab (tall chrome)');
    }
    if (medium.trailHasBtn) failures.push('short trailing sprouted expand tab');
    if (medium.trailClipped) failures.push('short trailing has .msg-clipped');

    // Off-screen: free-scroll fillers then push mediums above fold → clip chrome.
    const mediumOff = await page.evaluate(() => {
      const { uMed, jMed } = window._elsMed;
      const el = document.getElementById('messages');
      if (window.leaveTrackBottom) window.leaveTrackBottom();
      for (let i = 0; i < 12; i++) {
        window.addMsg('jevons', 'filler bulk ' + i + '\n' + Array.from({ length: 16 }, (_, j) => 'pad ' + j).join('\n'));
      }
      el.scrollTop = Math.min(el.scrollHeight, Math.max(uMed.offsetTop + uMed.offsetHeight, jMed.offsetTop + jMed.offsetHeight) + 40);
      if (uMed.offsetTop + uMed.offsetHeight > el.scrollTop) el.scrollTop = el.scrollHeight;
      if (window.collapseAutoExpandedOffScreen) window.collapseAutoExpandedOffScreen();
      return {
        uAbove: uMed.offsetTop + uMed.offsetHeight <= el.scrollTop,
        jAbove: jMed.offsetTop + jMed.offsetHeight <= el.scrollTop,
        uExpanded: uMed._expanded === true,
        jExpanded: jMed._expanded === true,
        uClipped: uMed.classList.contains('msg-clipped'),
        jClipped: jMed.classList.contains('msg-clipped'),
        uHasBtn: !!uMed._expandBtn,
        jHasBtn: !!jMed._expandBtn,
      };
    });
    if (mediumOff.uAbove) {
      if (mediumOff.uExpanded) failures.push('T106/T261: medium user still expanded fully above fold');
      if (!mediumOff.uClipped) failures.push('T106: medium user above fold missing .msg-clipped');
      if (!mediumOff.uHasBtn) failures.push('T106: medium user above fold missing expand tab');
    }
    if (mediumOff.jAbove) {
      if (mediumOff.jExpanded) failures.push('T106/T261: medium assistant still expanded fully above fold');
      if (!mediumOff.jClipped) failures.push('T106: medium assistant above fold missing .msg-clipped');
      if (!mediumOff.jHasBtn) failures.push('T106: medium assistant above fold missing expand tab');
    }

    // ── Scenario F (T261): history-replay pin leaves end-of-transcript expanded ──
    // Mid-burst sits at scrollTop≈0; without T261, near-end tall rows collapse
    // as "below fold" then stay clipped after pin.
    const t261Replay = await page.evaluate(async () => {
      const el = document.getElementById('messages');
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else el.innerHTML = '';
      if (typeof window.beginHistoryReplay === 'function') window.beginHistoryReplay();
      const tall = (n, prefix) => Array.from({ length: n }, (_, i) =>
        `${prefix} line ${i}: durable conversation record padding text`).join('\n');
      // Older bulk so pin-bottom leaves some above fold.
      for (let i = 0; i < 8; i++) {
        window.addMsg('user', tall(40, 'old' + i));
        await new Promise((r) => requestAnimationFrame(r));
      }
      // Near-end pair: medium-tall user + assistant that should stay in view after pin.
      const nearUser = window.addMsg('user', tall(14, 'near-user'));
      await new Promise((r) => requestAnimationFrame(r));
      const nearAsst = window.addMsg('jevons', '### near end\n' +
        Array.from({ length: 12 }, (_, i) => `- near_item_${i} padding`).join('\n'));
      await new Promise((r) => requestAnimationFrame(r));
      // Mid-replay: near-end messages must not be force-collapsed yet (geometry at top).
      const midCollapsed = !!(nearUser._fullText && nearUser._expanded !== true && nearUser.classList.contains('msg-clipped'));
      if (typeof window.endHistoryReplayAndPin === 'function') {
        window.endHistoryReplayAndPin('t261-test');
      } else if (typeof window.handle === 'function') {
        window.handle({ type: 'history_meta', older: 0, start: 0, total: 10 });
      }
      // 🎯T347: replay appends are lazy shells; virtualisation is stubbed in
      // this harness (see top), so run the post-pin band materialize by hand
      // for the near-end pair — the real band pass is virtual-list-test.js's.
      if (window.rematerializeMsg) {
        window.rematerializeMsg(nearUser);
        window.rematerializeMsg(nearAsst);
      }
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      const dist = el.scrollHeight - el.clientHeight - el.scrollTop;
      const probe = (m) => {
        const top = m.offsetTop;
        const bot = top + m.offsetHeight;
        return {
          inView: bot > viewTop && top < viewBot,
          expanded: m._expanded === true,
          auto: m._autoExpanded === true,
          clipped: m.classList.contains('msg-clipped'),
          tall: m._fullText != null,
        };
      };
      return {
        dist,
        replay: !!window.historyReplayActive,
        midHadCollapsed: midCollapsed,
        user: probe(nearUser),
        asst: probe(nearAsst),
        msgCount: el.querySelectorAll('.msg').length,
      };
    });
    if (t261Replay.msgCount < 8) {
      failures.push('T261 replay: expected many messages, got ' + t261Replay.msgCount);
    }
    if (t261Replay.replay) failures.push('T261 replay: historyReplayActive still true after pin');
    if (t261Replay.dist > 48) {
      failures.push('T261 replay: not pinned near bottom dist=' + t261Replay.dist);
    }
    const checkNear = (label, p) => {
      if (!p.tall) {
        failures.push(`T261 replay: ${label} not measured tall — retune fixture`);
        return;
      }
      if (!p.inView) {
        failures.push(`T261 replay: ${label} not in viewport after pin ` + JSON.stringify(p));
        return;
      }
      if (!p.expanded) failures.push(`T261: ${label} in view after history pin is collapsed`);
      if (p.clipped) failures.push(`T261: ${label} in view after history pin still .msg-clipped`);
      if (!p.auto) failures.push(`T261: ${label} missing _autoExpanded after pin`);
    };
    checkNear('near-user', t261Replay.user);
    checkNear('near-asst', t261Replay.asst);

    // ── Scenario G (T261 non-reload): stick-to-bottom new content ──
    // Two tall bubbles that both fit in the viewport stay expanded (not only latest).
    const t261Stick = await page.evaluate(async () => {
      const el = document.getElementById('messages');
      if (typeof window.resetTranscript === 'function') window.resetTranscript();
      else el.innerHTML = '';
      if (window.enterTrackBottom) window.enterTrackBottom();
      const bodyA = Array.from({ length: 10 }, (_, i) =>
        `stick A line ${i}: request body exceeds the clip pocket slightly`).join('\n');
      const bodyB = Array.from({ length: 10 }, (_, i) =>
        `stick B line ${i}: second tall bubble still in the same viewport`).join('\n');
      const a = window.addMsg('user', bodyA);
      await new Promise((r) => requestAnimationFrame(r));
      const b = window.addMsg('jevons', bodyB);
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      if (window.refreshLatestExpansion) window.refreshLatestExpansion();
      const viewTop = el.scrollTop;
      const viewBot = viewTop + el.clientHeight;
      const probe = (m) => {
        const top = m.offsetTop;
        const bot = top + m.offsetHeight;
        return {
          inView: bot > viewTop && top < viewBot,
          expanded: m._expanded === true,
          clipped: m.classList.contains('msg-clipped'),
          tall: m._fullText != null,
        };
      };
      return { a: probe(a), b: probe(b), dist: el.scrollHeight - el.clientHeight - el.scrollTop };
    });
    if (t261Stick.a.tall && t261Stick.a.inView && !t261Stick.a.expanded) {
      failures.push('T261 stick: prior tall still collapsed while in view near end ' + JSON.stringify(t261Stick.a));
    }
    if (t261Stick.a.tall && t261Stick.a.inView && t261Stick.a.clipped) {
      failures.push('T261 stick: prior tall .msg-clipped while in view near end');
    }
    if (t261Stick.b.tall && !t261Stick.b.expanded) {
      failures.push('T261 stick: latest tall not expanded ' + JSON.stringify(t261Stick.b));
    }

  } catch (e) {
    failures.push('exception: ' + e.message + (e.stack ? '\n' + e.stack : ''));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL collapse-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - pocket clip collapse, T66/T246/T261 near-end expand, short no tab, medium-tall gate, T166 expand pad');
  console.log('screenshots: artifacts/collapse-preview.png, artifacts/collapse-expanded.png');
})();
