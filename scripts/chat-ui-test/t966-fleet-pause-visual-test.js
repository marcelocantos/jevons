// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// T966 / T493.1: isolated cockpit visual — pause button beside connection dot;
// paused composer pink/red with overlay hint "Paused...".
//
//   UI_HOST=127.0.0.1:<isolate-port> node scripts/chat-ui-test/t966-fleet-pause-visual-test.js

'use strict';

const fs = require('fs');
const http = require('http');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const host = process.env.UI_HOST || '';
const OUT_DIR = path.join(__dirname, 'artifacts', 't966-fleet-pause');
fs.mkdirSync(OUT_DIR, { recursive: true });

function die(code, msg) {
  console.error(msg);
  process.exit(code);
}

function parseHost(h) {
  if (!h) die(2, 'Set UI_HOST to an isolated daemon host:port');
  const base = new URL(`http://${h}`);
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname)) {
    die(2, 'UI_HOST must be loopback');
  }
  if (!base.port || base.port === '13705' || base.port === '13706') {
    die(2, 'refusing development/comparison ports — use an isolate');
  }
  return base;
}

function getJSON(url) {
  return new Promise((resolve, reject) => {
    http.get(url, (res) => {
      let body = '';
      res.on('data', (c) => { body += c; });
      res.on('end', () => {
        try { resolve({ status: res.statusCode, json: JSON.parse(body) }); }
        catch (e) { reject(e); }
      });
    }).on('error', reject);
  });
}

async function waitFleetState(base, want, tries) {
  for (let i = 0; i < tries; i++) {
    const intent = await getJSON(new URL('/api/fleet-intent', base));
    if (intent.json && intent.json.fleet_state === want) return intent.json;
    await new Promise((r) => setTimeout(r, 150));
  }
  const last = await getJSON(new URL('/api/fleet-intent', base));
  die(1, `fleet-intent never reached ${want}: ${JSON.stringify(last.json)}`);
}

async function main() {
  const base = parseHost(host);
  const health = await getJSON(new URL('/health', base));
  if (health.status !== 200) die(2, `health HTTP ${health.status}`);

  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  page.setDefaultTimeout(60000);

  const runningShot = path.join(OUT_DIR, 'running.png');
  const pausedShot = path.join(OUT_DIR, 'paused.png');
  const resumedShot = path.join(OUT_DIR, 'resumed.png');

  await page.goto(base.href, { waitUntil: 'networkidle' });
  await page.waitForSelector('#fleet-pause-btn', { state: 'visible' });
  await page.waitForSelector('#status .dot', { state: 'visible' });

  const layout = await page.evaluate(() => {
    const btn = document.querySelector('#fleet-pause-btn');
    const dot = document.querySelector('#status .dot');
    if (!btn || !dot) return { ok: false, reason: 'missing fleet-pause-btn or connection dot' };
    const br = btn.getBoundingClientRect();
    const dr = dot.getBoundingClientRect();
    const beside = br.left >= dr.right - 4 && Math.abs(br.top - dr.top) < 24;
    return { ok: beside, pauseIcon: btn.textContent.trim(), btnLeft: br.left, dotRight: dr.right };
  });
  if (!layout.ok) die(1, `pause button not beside connection dot: ${JSON.stringify(layout)}`);
  if (layout.pauseIcon !== '❚❚') die(1, `expected pause icon while running, got ${layout.pauseIcon}`);

  await page.screenshot({ path: runningShot, fullPage: false });

  await page.click('#fleet-pause-btn');
  await waitFleetState(base, 'blocked_owner', 40);

  await page.waitForSelector('.composer-field.fleet-paused-composer', { state: 'visible' });
  const pausedDom = await page.evaluate(() => {
    const hint = document.querySelector('.composer-field.fleet-paused-composer .composer-hint');
    const box = document.querySelector('.composer-field.fleet-paused-composer textarea');
    const btn = document.querySelector('#fleet-pause-btn');
    const style = box ? getComputedStyle(box) : null;
    return {
      hint: hint ? hint.textContent.trim() : '',
      placeholder: box ? box.getAttribute('placeholder') : null,
      disabled: box ? box.disabled : null,
      playIcon: btn ? btn.textContent.trim() : '',
      borderColor: style ? style.borderColor : '',
      background: style ? style.backgroundColor : '',
    };
  });
  if (pausedDom.hint !== 'Paused...') die(1, `composer hint want Paused..., got ${pausedDom.hint}`);
  if (pausedDom.playIcon !== '▶') die(1, `expected play icon while paused, got ${pausedDom.playIcon}`);
  if (!pausedDom.disabled) die(1, 'composer must be disabled while fleet paused');

  await page.screenshot({ path: pausedShot, fullPage: false });

  await page.click('#fleet-pause-btn');
  await waitFleetState(base, 'working', 40);
  await page.waitForFunction(
    () => !document.querySelector('.fleet-paused-composer'),
    { timeout: 15000 },
  );
  await page.waitForFunction(
    () => document.querySelector('#fleet-pause-btn')?.textContent.trim() === '❚❚',
    { timeout: 15000 },
  );

  const resumedDom = await page.evaluate(() => {
    const hint = document.querySelector('#main .composer-hint');
    const btn = document.querySelector('#fleet-pause-btn');
    return {
      pausedClass: !!document.querySelector('.fleet-paused-composer'),
      hint: hint ? hint.textContent.trim() : '',
      pauseIcon: btn ? btn.textContent.trim() : '',
    };
  });
  if (resumedDom.pausedClass) die(1, 'fleet-paused-composer still present after resume');
  if (resumedDom.pauseIcon !== '❚❚') die(1, `expected pause icon after resume, got ${resumedDom.pauseIcon}`);

  await page.screenshot({ path: resumedShot, fullPage: false });
  await browser.close();

  console.log('T966 fleet-pause visual: GREEN');
  console.log('screenshots:', runningShot, pausedShot, resumedShot);
}

main().catch((e) => die(1, String(e)));
