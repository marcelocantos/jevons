// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for mermaid diagram rendering (🎯T59),
// open/copy toolbar (🎯T83.1), and durable graph viz chrome (🎯T83).
// Serves the static web/ UI and drives addMsg() with assistant content,
// then asserts:
//   * a ```mermaid fence renders as an SVG (.mermaid-diagram svg), not raw
//   * a plain ```js fence is untouched — still a <pre><code>, no diagram
//   * invalid mermaid source degrades to its original code block (no throw,
//     no diagram)
//   * a diagram that is only PARTIALLY streamed does not render mid-stream
//     (the _streamRaw gate): it renders only once the turn is sealed
//   * toolbar Open / Copy source / Copy image is present
//   * Open puts the same graph in the dedicated panel (not chat scroll)
//   * Copy source path writes Mermaid text; Copy image path yields a PNG blob
//   * Agents "Open viz" opens durable panel empty state without chat
//   * Open-from-chat pins last graph; reopen via Open viz / Load last restores it
//   * Paste + Render path loads bullseye-style Mermaid export into the panel
// Screenshots the rendered diagram into artifacts/.
//
//   node scripts/chat-ui-test/mermaid-test.js [--headed]

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
        res.end(JSON.stringify({ version: 'mermaid-test', ok: true }));
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
  const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    // mermaid is a heavy CDN bundle — give it room to load alongside marked.
    await page.waitForFunction(
      () => typeof window.addMsg === 'function' && !!window.marked && !!window.mermaid,
      null, { timeout: 20000 });

    // A valid diagram, a plain code block, and an invalid diagram.
    await page.evaluate(() => {
      window._diag = window.addMsg('jevons', '```mermaid\ngraph TD;\n  A[Start] --> B{OK?};\n  B -->|yes| C[Ship];\n  B -->|no| A;\n```');
      window._code = window.addMsg('jevons', 'Here is code:\n```js\nconst x = 1;\n```');
      window._bad  = window.addMsg('jevons', '```mermaid\nthis is not valid mermaid !!!\n```');
    });

    // The valid diagram renders an SVG asynchronously.
    await page.waitForFunction(
      () => window._diag && window._diag._body.querySelector('.mermaid-diagram svg'),
      null, { timeout: 10000 }).catch(() => {});

    const state = await page.evaluate(() => ({
      diagSvg: !!window._diag._body.querySelector('.mermaid-diagram svg'),
      diagStillCode: !!window._diag._body.querySelector('code.language-mermaid'),
      codeIsPre: !!window._code._body.querySelector('pre code'),
      codeIsDiagram: !!window._code._body.querySelector('.mermaid-diagram'),
      // invalid mermaid: no diagram, degrades to the raw code block
      badIsDiagram: !!window._bad._body.querySelector('.mermaid-diagram svg'),
      badStillCode: !!window._bad._body.querySelector('code.language-mermaid'),
    }));

    if (!state.diagSvg) failures.push('valid ```mermaid did not render an SVG diagram');
    if (state.diagStillCode) failures.push('```mermaid left a raw code block after rendering');
    if (!state.codeIsPre) failures.push('plain ```js block is not a <pre><code>');
    if (state.codeIsDiagram) failures.push('plain ```js block was wrongly turned into a diagram');
    if (state.badIsDiagram) failures.push('invalid mermaid produced an SVG (should degrade)');
    if (!state.badStillCode) failures.push('invalid mermaid did not degrade to its code block');

    // Invalid source must NOT leak mermaid's "Syntax error" bomb graphic
    // into the page body (suppressErrorRendering + orphan sweep).
    const leaked = await page.evaluate(() => {
      const stray = Array.from(document.querySelectorAll('body > svg, body > div'))
        .some(n => /Syntax error|mermaid version/i.test(n.textContent || ''));
      return stray || /Syntax error in text/i.test(document.body.innerText || '');
    });
    if (leaked) failures.push('mermaid error graphic leaked into the page body on invalid source');

    // Streaming gate: while a turn is mid-stream (_streamRaw set), a partial
    // diagram must NOT render. Simulate by rendering with the marker present.
    const streamState = await page.evaluate(() => {
      const el = window.addMsg('jevons', '');
      el._streamRaw = '```mermaid\ngraph TD; A-->B;';       // unsealed, partial
      window.renderBody(el, 'jevons', el._streamRaw);
      const midStream = !!el._body.querySelector('.mermaid-diagram');
      // Seal it: clear the marker, render the complete diagram.
      delete el._streamRaw;
      window.renderBody(el, 'jevons', '```mermaid\ngraph TD; A-->B;\n```');
      window._sealed = el;
      return { midStream };
    });
    if (streamState.midStream) failures.push('a mid-stream partial diagram rendered before seal (gate failed)');

    await page.waitForFunction(
      () => window._sealed && window._sealed._body.querySelector('.mermaid-diagram svg'),
      null, { timeout: 10000 }).catch(() => {});
    const sealedOk = await page.evaluate(() => !!window._sealed._body.querySelector('.mermaid-diagram svg'));
    if (!sealedOk) failures.push('diagram did not render after the stream was sealed');

    // ── 🎯T83.1 open / copy ─────────────────────────────────────────
    await page.waitForFunction(
      () => window._diag && window._diag._body.querySelector('.mermaid-diagram .mermaid-toolbar'),
      null, { timeout: 10000 }).catch(() => {});

    const toolbar = await page.evaluate(() => {
      const wrap = window._diag && window._diag._body.querySelector('.mermaid-diagram');
      if (!wrap) return { ok: false, reason: 'no wrap' };
      const btns = Array.from(wrap.querySelectorAll('.mermaid-toolbar button')).map(b => ({
        action: b.dataset.mermaidAction,
        label: b.textContent.trim(),
      }));
      return {
        ok: true,
        src: wrap.dataset.mermaidSrc || '',
        actions: btns.map(b => b.action),
        labels: btns.map(b => b.label),
        hasSvg: !!wrap.querySelector('svg'),
      };
    });
    if (!toolbar.ok) failures.push('toolbar: no .mermaid-diagram wrap');
    else {
      if (toolbar.actions.join(',') !== 'open,copy-source,copy-image') {
        failures.push('toolbar actions: ' + toolbar.actions.join(','));
      }
      if (!toolbar.src || toolbar.src.indexOf('graph TD') < 0) {
        failures.push('diagram missing data-mermaid-src');
      }
    }

    // Open → dedicated panel (outside #messages) hosts the same SVG graph.
    const openState = await page.evaluate(async () => {
      const wrap = window._diag._body.querySelector('.mermaid-diagram');
      const openBtn = wrap.querySelector('[data-mermaid-action="open"]');
      openBtn.click();
      const panel = document.getElementById('mermaid-viz-panel');
      const body = document.getElementById('mvp-body');
      const inMessages = !!(panel && document.getElementById('messages') &&
        document.getElementById('messages').contains(panel));
      return {
        open: !!(panel && panel.classList.contains('open') && !panel.hidden),
        hasSvg: !!(body && body.querySelector('svg')),
        svgText: body ? (body.textContent || '') : '',
        outsideChat: !!panel && !inMessages,
        panelParentIsBody: !!(panel && panel.parentElement === document.body),
      };
    });
    if (!openState.open) failures.push('Open did not show #mermaid-viz-panel');
    if (!openState.hasSvg) failures.push('panel missing SVG of the graph');
    if (!openState.outsideChat) failures.push('panel is inside #messages chat scroll');
    // Graph labels from the sample diagram should appear in the panel SVG text.
    if (openState.svgText.indexOf('Start') < 0 && openState.svgText.indexOf('Ship') < 0) {
      failures.push('panel SVG does not look like the same graph (no Start/Ship labels)');
    }

    // Copy source — paste path writes Mermaid text/plain.
    const copySrc = await page.evaluate(async () => {
      const wrap = window._diag._body.querySelector('.mermaid-diagram');
      const src = wrap.dataset.mermaidSrc || '';
      let wrote = null;
      if (navigator.clipboard) {
        navigator.clipboard.writeText = async (t) => { wrote = String(t); };
      }
      await window.handleMermaidAction('copy-source', wrap);
      return { src, wrote, match: wrote === src && src.indexOf('graph TD') >= 0 };
    });
    if (!copySrc.match) {
      failures.push('copy source did not write mermaid text (wrote=' +
        JSON.stringify(copySrc.wrote) + ' srcLen=' + (copySrc.src || '').length + ')');
    }

    // Copy image path exists: svg → PNG blob (and multi-MIME plan when available).
    const copyImg = await page.evaluate(async () => {
      const wrap = window._diag._body.querySelector('.mermaid-diagram');
      const svg = wrap.querySelector('svg');
      if (!svg) return { ok: false, reason: 'no svg' };
      const blob = await window.svgToPngBlob(svg.outerHTML);
      if (!blob || blob.type !== 'image/png' || !(blob.size > 0)) {
        return { ok: false, reason: 'bad png blob', type: blob && blob.type, size: blob && blob.size };
      }
      let writeArgs = null;
      if (navigator.clipboard) {
        navigator.clipboard.write = async (items) => { writeArgs = items; };
      }
      const result = await window.copyMermaidImage(wrap.dataset.mermaidSrc || '', svg.outerHTML);
      let types = [];
      if (writeArgs && writeArgs[0] && writeArgs[0].types) {
        types = Array.from(writeArgs[0].types || []);
      } else if (writeArgs && writeArgs[0] && typeof writeArgs[0] === 'object') {
        try { types = Array.from(writeArgs[0].types || []); } catch (_) {}
      }
      return {
        ok: true,
        mode: result && result.mode,
        pngSize: blob.size,
        types: types,
        hasPngPath: true,
      };
    });
    if (!copyImg.ok) {
      failures.push('copy image path failed: ' + (copyImg.reason || JSON.stringify(copyImg)));
    } else if (!copyImg.hasPngPath) {
      failures.push('copy image path missing');
    }

    // ── 🎯T83 durable chrome: Open viz, pin/persist, paste ──────────
    // Fresh storage: clear pin, open from Agents chrome → empty state.
    const emptyChrome = await page.evaluate(() => {
      try { localStorage.removeItem(window.MermaidActions.PIN_STORAGE_KEY); } catch (_) {}
      window.closeMermaidPanel();
      const btn = document.getElementById('open-viz-btn');
      if (!btn) return { ok: false, reason: 'no #open-viz-btn' };
      btn.click();
      const panel = document.getElementById('mermaid-viz-panel');
      const body = document.getElementById('mvp-body');
      const empty = body && body.querySelector('[data-mvp-empty]');
      const inMessages = !!(panel && document.getElementById('messages') &&
        document.getElementById('messages').contains(panel));
      return {
        ok: true,
        open: !!(panel && panel.classList.contains('open') && !panel.hidden),
        hasEmpty: !!empty,
        outsideChat: !!panel && !inMessages,
        title: (document.getElementById('mvp-title') || {}).textContent || '',
        loadLast: !!document.getElementById('mvp-load-last'),
        pasteBtn: !!document.getElementById('mvp-paste-btn'),
      };
    });
    if (!emptyChrome.ok) failures.push('open viz chrome: ' + emptyChrome.reason);
    else {
      if (!emptyChrome.open) failures.push('Open viz did not open #mermaid-viz-panel');
      if (!emptyChrome.hasEmpty) failures.push('Open viz missing empty state (data-mvp-empty)');
      if (!emptyChrome.outsideChat) failures.push('viz panel is inside #messages');
      if (!emptyChrome.loadLast || !emptyChrome.pasteBtn) {
        failures.push('panel chrome missing Load last / Paste controls');
      }
    }

    // Re-open from chat diagram → pins; close; Open viz restores last.
    const persist = await page.evaluate(async () => {
      try { localStorage.removeItem(window.MermaidActions.PIN_STORAGE_KEY); } catch (_) {}
      window.closeMermaidPanel();
      const wrap = window._diag && window._diag._body.querySelector('.mermaid-diagram');
      if (!wrap) return { ok: false, reason: 'no diagram wrap' };
      await window.handleMermaidAction('open', wrap);
      const pinnedAfterOpen = window.MermaidActions.loadPinnedGraph(localStorage);
      window.closeMermaidPanel();
      const closed = (() => {
        const p = document.getElementById('mermaid-viz-panel');
        return !p || p.hidden || !p.classList.contains('open');
      })();
      window.openVizFromChrome();
      const panel = document.getElementById('mermaid-viz-panel');
      const body = document.getElementById('mvp-body');
      return {
        ok: true,
        pinnedSrc: pinnedAfterOpen && pinnedAfterOpen.src,
        closed: closed,
        reopenOpen: !!(panel && panel.classList.contains('open') && !panel.hidden),
        hasSvg: !!(body && body.querySelector('svg')),
        svgText: body ? (body.textContent || '') : '',
        emptyAfterPin: !!(body && body.querySelector('[data-mvp-empty]')),
      };
    });
    if (!persist.ok) failures.push('persist path: ' + persist.reason);
    else {
      if (!persist.pinnedSrc || persist.pinnedSrc.indexOf('graph TD') < 0) {
        failures.push('open-from-chat did not pin mermaid source');
      }
      if (!persist.closed) failures.push('closeMermaidPanel left panel open');
      if (!persist.reopenOpen) failures.push('Open viz did not reopen after pin');
      if (persist.emptyAfterPin) failures.push('Open viz showed empty after pin (expected last graph)');
      if (!persist.hasSvg) failures.push('restored last graph missing SVG');
      if (persist.svgText.indexOf('Start') < 0 && persist.svgText.indexOf('Ship') < 0) {
        failures.push('restored last graph labels missing');
      }
    }

    // Paste + Render: bullseye-style export into durable panel (no chat).
    const pastePath = await page.evaluate(async () => {
      window.closeMermaidPanel();
      const ta = document.getElementById('mvp-paste');
      if (!ta) return { ok: false, reason: 'no #mvp-paste' };
      ta.value = '```mermaid\ngraph LR;\n  Bullseye[T83] --> Panel[viz];\n```';
      document.getElementById('mvp-paste-wrap').classList.add('open');
      await window.renderMermaidSourceInPanel(ta.value, { title: 'Bullseye export', pin: true });
      const panel = document.getElementById('mermaid-viz-panel');
      const body = document.getElementById('mvp-body');
      const pin = window.MermaidActions.loadPinnedGraph(localStorage);
      return {
        ok: true,
        open: !!(panel && panel.classList.contains('open') && !panel.hidden),
        hasSvg: !!(body && body.querySelector('svg')),
        text: body ? (body.textContent || '') : '',
        pinSrc: pin && pin.src,
        pinTitle: pin && pin.title,
      };
    });
    if (!pastePath.ok) failures.push('paste path: ' + pastePath.reason);
    else {
      if (!pastePath.open) failures.push('paste render did not open panel');
      if (!pastePath.hasSvg) failures.push('paste render missing SVG');
      if (pastePath.text.indexOf('T83') < 0 && pastePath.text.indexOf('viz') < 0) {
        failures.push('paste render labels missing (T83/viz)');
      }
      if (!pastePath.pinSrc || pastePath.pinSrc.indexOf('graph LR') < 0) {
        failures.push('paste path did not pin source');
      }
    }

    await page.screenshot({ path: path.join(OUT_DIR, 'mermaid.png'), fullPage: true });
  } catch (e) {
    failures.push('exception: ' + e.message);
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL mermaid-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - ```mermaid renders; toolbar open/copy; plain code untouched; stream-gated');
  console.log('screenshot: artifacts/mermaid.png');
})();
