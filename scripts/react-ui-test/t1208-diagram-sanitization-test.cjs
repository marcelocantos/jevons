// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// T1208: exercise the shipped Mermaid asset through the React transcript.
// This is a hermetic browser check, not a live-agent journey. All input and
// transport are local fixtures; no owner state or external endpoint is used.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { chromium } = require('./playwright.cjs')();

const dist = path.resolve(__dirname, '../../ui/dist');
const diagrams = [
  'flowchart LR\n A[Owner] --> B[Agent]',
  'flowchart LR\n A["<img src=x onerror=globalThis.__t1208Executed=1>Safe label"] --> B["<a href=javascript:globalThis.__t1208Executed=1>Link</a>"]',
  'flowchart LR\n A["$$x^2 + y^2$$"] --> B[Result]',
];
let server;
let browser;

async function main() {
  await fs.access(path.join(dist, 'index.html'));
  server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://127.0.0.1');
      const file = path.resolve(dist, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)] || 'application/octet-stream');
      res.end(data);
    } catch {
      res.writeHead(404).end();
    }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const base = new URL(`http://127.0.0.1:${server.address().port}`);
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
  page.setDefaultTimeout(60000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => { globalThis.__t1208Executed = 0; });
  await page.route('**/*', route => new URL(route.request().url()).origin === base.origin ? route.continue() : route.abort());
  await page.route('**/api/**', route => {
    const pathname = new URL(route.request().url()).pathname;
    const body = pathname === '/api/agents'
      ? [{ name: 'jevons', purpose: 'overseer', provider: 'fixture', running: true, status: 'running', phase: 'idle' }]
      : pathname === '/api/plan-usage' ? { backends: [] }
        : pathname === '/api/frontier' ? [] : {};
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) });
  });
  const rows = diagrams.flatMap((source, i) => [
    {
      id: `e:${i * 2 + 1}`, index: i * 2 + 1, op: 'put', type: 'user',
      event: { type: 'user', turn_origin: 'owner', message: {
        role: 'user', content: [{ type: 'text', text: `Render diagram ${i + 1}.` }],
      } },
    },
    {
      id: `e:${i * 2 + 2}`, index: i * 2 + 2, op: 'put', type: 'assistant',
      event: { type: 'assistant', message: {
        role: 'assistant', content: [{ type: 'text', text: '```mermaid\n' + source + '\n```' }], stop_reason: 'end_turn',
      } },
    },
  ]);
  await page.routeWebSocket('**/ws/mux', socket => {
    socket.onMessage(payload => {
      const frame = JSON.parse(String(payload));
      if (frame.type === 'ping') return socket.send(JSON.stringify({ type: 'pong' }));
      if (frame.t !== 'open' && frame.t !== 'window') return;
      const send = (t, body) => socket.send(JSON.stringify({ v: 1, ch: frame.ch, t, body }));
      const history = frame.ch === 'transcript:jevons' ? rows : [];
      for (const row of history) send('frame', row);
      send('meta', { lo: 1, hi: 0, n: history.length, start: 1, older: 0, total: history.length, following: true, working: false, phase: { phase: 'idle' }, overseer_down: '' });
    });
  });
  await page.goto(base.href);
  const screenshot = path.join(await fs.mkdtemp(path.join(os.tmpdir(), 'jevons-t1208-')), 'diagrams.png');
  try {
    await page.waitForFunction(count => document.querySelectorAll('#messages .mermaid-diagram svg').length === count, diagrams.length);
  } catch (error) {
    await page.screenshot({ path: screenshot, fullPage: true });
    console.error({ screenshot, errors, state: await page.evaluate(() => ({
      text: document.querySelector('#messages')?.textContent,
      mermaidErrors: window.__jevonsMermaidErrs,
    })) });
    throw error;
  }
  const rendered = await page.locator('#messages .mermaid-diagram').evaluateAll(elements => elements.map(el => {
    const svg = el.querySelector('svg');
    const box = svg.getBoundingClientRect();
    return {
      text: svg.textContent,
      width: box.width,
      height: box.height,
      math: Boolean(svg.querySelector('.katex, math')),
      scripts: el.querySelectorAll('script').length,
      unexpectedSiblings: [...el.childNodes]
        .filter(node => node !== svg && (node.nodeType !== Node.TEXT_NODE || node.textContent.trim()))
        .map(node => node.nodeName),
      // Include the wrapper, SVG root, and every descendant. Unsafe markup
      // outside the returned SVG is inserted by the same innerHTML assignment.
      unsafeAttributes: [el, ...el.querySelectorAll('*')].flatMap(node => [...node.attributes]
        .filter(attr => /^on/i.test(attr.name) || /^(href|xlink:href|src)$/i.test(attr.name) && /^\s*javascript:/i.test(attr.value))
        .map(attr => `${attr.name}=${attr.value}`)),
    };
  }));
  for (const diagram of rendered) {
    assert(diagram.width > 0 && diagram.height > 0, 'diagram has real rendered geometry');
    assert.equal(diagram.scripts, 0, 'no script anywhere in the inserted diagram wrapper');
    assert.deepEqual(diagram.unexpectedSiblings, [], 'the returned SVG has no markup or text siblings');
    assert.deepEqual(diagram.unsafeAttributes, [], 'wrapper, SVG root, and descendants contain no unsafe attributes');
  }
  assert(rendered.some(diagram => diagram.text.includes('Owner') && diagram.text.includes('Agent')));
  assert(rendered.some(diagram => diagram.text.includes('Safe label')));
  assert(rendered.some(diagram => diagram.math), 'math label uses the shipped KaTeX path');
  assert.equal(await page.evaluate(() => globalThis.__t1208Executed), 0, 'unsafe label markup did not execute');
  assert.deepEqual(errors, []);
  assert.deepEqual(await page.evaluate(() => window.__jevonsMermaidErrs || []), []);
  await page.screenshot({ path: screenshot, fullPage: true });
  console.log(`PASS: T1208 built React renders diagrams and math, strips unsafe label markup; screenshot=${screenshot}`);
}

main().catch(error => { console.error(error); process.exitCode = 1; }).finally(async () => {
  await browser?.close();
  if (server) await new Promise(resolve => server.close(resolve));
});
