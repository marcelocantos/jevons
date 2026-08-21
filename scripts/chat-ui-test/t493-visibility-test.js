// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright unit for 🎯T493 VisibilityHelper: a covered / opacity-0 /
// off-viewport node fails checkVisibility or the centre hit-test. Real
// Chromium APIs — the Node suite cannot call them.
//
// Oracle viewport is pinned (1280×800 @ dpr 1, matching screen). Never
// viewport:null. OCR of distinctive tokens is J19 + imagetext fixtures;
// this file is the DOM pair.
//
//   node scripts/chat-ui-test/t493-visibility-test.js [--headed]

'use strict';

const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const VC = require(path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js'));

const HEADED = process.argv.includes('--headed');
const censusPath = path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js');

function die(code, msg) {
  console.error(msg);
  process.exit(code);
}

const FIXTURE = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>T493 visibility fixture</title>
<style>
  html, body { margin: 0; padding: 0; background: #111; }
  #messages {
    width: 640px;
    height: 400px;
    overflow: hidden;
    position: relative;
    background: #fff;
    color: #111;
    font: 16px/1.4 sans-serif;
  }
  .msg { padding: 12px 16px; }
</style>
</head>
<body>
<div id="messages">
  <div id="visible" class="msg">VISIBLE-TOKEN on screen</div>
  <div id="opacity0" class="msg" style="opacity:0">OPACITY-TOKEN ghost</div>
  <div id="off" class="msg" style="position:absolute; top:-2000px; left:16px">OFFVIEW-TOKEN gone</div>
  <div id="covered-wrap" style="position:relative">
    <div id="covered" class="msg">COVERED-TOKEN under overlay</div>
    <div id="overlay" style="position:absolute; inset:0; background:#fff;"></div>
  </div>
</div>
</body>
</html>`;

(async () => {
  const browser = await chromium.launch({ headless: !HEADED });
  const context = await browser.newContext({
    viewport: VC.ORACLE_VIEWPORT,
    screen: VC.ORACLE_VIEWPORT,
    deviceScaleFactor: VC.ORACLE_DPR,
  });
  const page = await context.newPage();
  try {
    await page.setContent(FIXTURE);
    await page.addScriptTag({ path: censusPath });

    const report = await page.evaluate(() => {
      const H = window.ViewportCensus && window.ViewportCensus.VisibilityHelper;
      if (!H) return { missingHelper: true };
      const scroller = document.getElementById('messages');
      const sr = scroller.getBoundingClientRect();
      function one(id) {
        const el = document.getElementById(id);
        const g = H.inspect(el, sr);
        return {
          id: id,
          api: !!(el && typeof el.checkVisibility === 'function'),
          checkVisibility: g.checkVisibility,
          hitOk: g.hitOk,
          inScroller: g.inScroller,
          hitTag: g.hitTag,
          onScreen: H.domOnScreen(g),
          text: g.text,
        };
      }
      return {
        innerWidth: window.innerWidth,
        innerHeight: window.innerHeight,
        devicePixelRatio: window.devicePixelRatio,
        viewportPinned: window.ViewportCensus.viewportPinned(
          window.innerWidth, window.innerHeight, window.devicePixelRatio),
        visible: one('visible'),
        opacity0: one('opacity0'),
        off: one('off'),
        covered: one('covered'),
      };
    });

    console.log(JSON.stringify(report, null, 2));

    if (report.missingHelper) {
      die(1, 'T493: ViewportCensus.VisibilityHelper missing in page');
    }
    if (!report.viewportPinned) {
      die(1, 'T493 viewport not pinned: inner=' + report.innerWidth + 'x' +
        report.innerHeight + ' dpr=' + report.devicePixelRatio +
        ' want ' + VC.ORACLE_VIEWPORT.width + 'x' + VC.ORACLE_VIEWPORT.height +
        ' dpr=' + VC.ORACLE_DPR);
    }
    if (!report.visible.api) {
      die(1, 'T493: element.checkVisibility is not a function — gate 1 cannot fire');
    }
    if (!report.visible.onScreen || !report.visible.checkVisibility || !report.visible.hitOk) {
      die(1, 'T493: visible node must pass checkVisibility + centre hit-test: ' +
        JSON.stringify(report.visible));
    }
    if (report.opacity0.checkVisibility) {
      die(1, 'T493: opacity-0 must fail checkVisibility (gate 1): ' +
        JSON.stringify(report.opacity0));
    }
    if (report.opacity0.onScreen) {
      die(1, 'T493: opacity-0 must not be on-screen: ' + JSON.stringify(report.opacity0));
    }
    if (report.off.inScroller && report.off.hitOk) {
      die(1, 'T493: off-viewport must fail inScroller or the centre hit-test (gate 2): ' +
        JSON.stringify(report.off));
    }
    if (report.off.onScreen) {
      die(1, 'T493: off-viewport must not be on-screen: ' + JSON.stringify(report.off));
    }
    if (report.covered.hitOk) {
      die(1, 'T493: covered node must fail the centre hit-test (gate 2): ' +
        JSON.stringify(report.covered));
    }
    if (report.covered.onScreen) {
      die(1, 'T493: covered node must not be on-screen: ' + JSON.stringify(report.covered));
    }

    console.log('t493-visibility-test: passed (visible on-screen; opacity-0 / covered / off-viewport fail)');
  } finally {
    await browser.close().catch(() => {});
  }
})().catch((err) => {
  die(1, 't493-visibility-test: ' + (err && err.stack ? err.stack : err));
});
