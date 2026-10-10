// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
// 🎯T1041: packaged cockpit viewport and drawer behavior, without hardware.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const { chromium } = require('./playwright.cjs')();

const dist = path.resolve(__dirname, '../../ui/dist');
async function main() {
  // Clean gate clones omit ignored dist/node_modules. Build the tracked bundle
  // there before probing the actual packaged React output.
  if (!(await fs.stat(path.join(dist, 'index.html')).catch(() => null))) {
    execFileSync('make', ['ui-build'], { cwd: path.resolve(__dirname, '../..'), stdio: 'inherit' });
  }
  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://localhost');
      const file = path.resolve(dist, '.' + (url.pathname === '/' ? '/index.html' : decodeURIComponent(url.pathname)));
      assert(file.startsWith(dist + path.sep));
      const data = await fs.readFile(file);
      res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' }[path.extname(file)] || 'application/octet-stream');
      res.end(data);
    } catch { res.writeHead(404).end(); }
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    const toggle = page.locator('#agents-toggle');
    const pane = page.locator('#activity-pane');
    for (const width of [390, 599, 600, 800]) {
      await page.setViewportSize({ width, height: 844 });
      const narrow = width < 600;
      assert.equal(await toggle.isVisible(), narrow, `toggle at ${width}`);
      assert.equal(await pane.isVisible(), !narrow, `default sidebar at ${width}`);
      assert.equal(await page.locator('#rhs-split').count(), 1, 'sidebar remains mounted');
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), width, `no horizontal overflow at ${width}`);
      if (!narrow) continue;
      assert.equal(await pane.getAttribute('inert'), '', 'closed sidebar is inert');
      await toggle.click();
      assert.equal(await toggle.getAttribute('aria-expanded'), 'true');
      assert.equal(await pane.isVisible(), true);
      assert.equal(await page.evaluate(() => document.activeElement?.id), 'agents-close');
      await page.keyboard.press('Escape');
      assert.equal(await pane.isVisible(), false);
      assert.equal(await page.evaluate(() => document.activeElement?.id), 'agents-toggle');
    }
    console.log('T1041 viewport 390/599/600/800: narrow drawer, inert/focus/Escape, wide split, no overflow GREEN');
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
