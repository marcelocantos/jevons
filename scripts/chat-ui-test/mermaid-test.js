// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic perceptual test for mermaid diagram rendering (🎯T59). Serves
// the static web/ UI and drives addMsg() with assistant content, then
// asserts:
//   * a ```mermaid fence renders as an SVG (.mermaid-diagram svg), not raw
//   * a plain ```js fence is untouched — still a <pre><code>, no diagram
//   * invalid mermaid source degrades to its original code block (no throw,
//     no diagram)
//   * a diagram that is only PARTIALLY streamed does not render mid-stream
//     (the _streamRaw gate): it renders only once the turn is sealed
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
  console.log('ok - ```mermaid renders an SVG; plain code untouched; invalid degrades; stream-gated');
  console.log('screenshot: artifacts/mermaid.png');
})();
