// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Host-level hermetic for 🎯T494.1.1: apply the daily replay event mix
// (user, assistant, agent_note, system, tool_use — notes/tools between
// owner turns) through window.handle while historyReplayActive, then
// pin. Fold-unit tests do not cover host openFoldTurnSlot during replay
// (virtualize is a no-op). A text-only tape that greens is a failed
// oracle, not a passed product.
//
//   node scripts/chat-ui-test/t494.1.1-replay-mix-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);
const VC = require(path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js'));

const HEADED = process.argv.includes('--headed');
const MID = 20;

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  return 'application/octet-stream';
}

function startServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/history') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ start: 0, total: 0, lines: [] }));
        return;
      }
      if (u.pathname === '/api/log') {
        res.writeHead(204);
        res.end();
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
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startServer();
  const browser = await chromium.launch({ headless: !HEADED });
  const context = await browser.newContext({
    viewport: VC.ORACLE_VIEWPORT,
    screen: VC.ORACLE_VIEWPORT,
    deviceScaleFactor: VC.ORACLE_DPR,
  });
  const page = await context.newPage();
  const censusPath = path.join(__dirname, '..', '..', 'web', 'scripts', 'viewport_census.js');
  await page.addInitScript({ path: censusPath });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.handle === 'function' &&
        typeof window.beginHistoryReplay === 'function' &&
        typeof window.ViewportCensus === 'object' &&
        !!window.marked,
      null,
      { timeout: 10000 },
    );

    const result = await page.evaluate(async (mid) => {
      const tools = ['Read', 'Bash', 'Grep'];
      window.beginHistoryReplay();
      const replayAtStart = !!window.historyReplayActive;
      window.handle({
        type: 'user',
        message: { role: 'user', content: 'ROOTmix-00 owner turn 0' },
      });
      window.handle({
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [{ type: 'text', text: 'ack ROOTmix-00' }],
          stop_reason: 'end_turn',
        },
      });
      let toolUse = 0;
      let notes = 0;
      let systems = 0;
      for (let i = 0; i < mid; i++) {
        const name = tools[i % tools.length];
        window.handle({
          type: 'assistant',
          message: {
            role: 'assistant',
            content: [{ type: 'tool_use', name: name, input: { path: 'pad' } }],
          },
        });
        toolUse++;
        window.handle({
          type: 'tool_result',
          message: { content: [{ type: 'tool_result', content: 'ok ' + name }] },
        });
        window.handle({
          type: 'agent_note',
          text: '[Agent pad responded] mid ' + i,
        });
        notes++;
        window.handle({ type: 'system' });
        systems++;
      }
      const replayDuringMix = !!window.historyReplayActive;
      window.handle({
        type: 'user',
        message: { role: 'user', content: 'ROOTmix-01 owner turn 1' },
      });
      window.handle({
        type: 'assistant',
        message: {
          role: 'assistant',
          content: [{ type: 'text', text: 'ack ROOTmix-01' }],
          stop_reason: 'end_turn',
        },
      });
      const eventsFed = 2 + 2 + mid * 4;
      window.handle({ type: 'history_meta', older: 0, start: 0, total: eventsFed });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      if (typeof window.virtualizeMessages === 'function') window.virtualizeMessages();
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const census = window.ViewportCensus.collect({ prefix: 'ROOTmix-' });
      const rows = Array.isArray(window.__transcriptRows) ? window.__transcriptRows : [];
      const slotItems = [];
      rows.forEach(function (row) {
        const kind = row && (row.kind || row.role) || '';
        if (kind !== 'turn-slot' && kind !== 'turn-marker') return;
        (row.items || []).forEach(function (it) {
          slotItems.push(it && it.cls);
        });
      });
      return {
        replayAtStart: replayAtStart,
        replayDuringMix: replayDuringMix,
        replayAfterMeta: !!window.historyReplayActive,
        toolUse: toolUse,
        notes: notes,
        systems: systems,
        eventsFed: eventsFed,
        modelRows: rows.length,
        emptySlots: census.emptySlots,
        labelledSlots: census.labelledSlots,
        emptySlotDesert: census.emptySlotDesert,
        visibleBubbles: census.visibleBubbles,
        packedPaneFail: census.packedPaneFail,
        latestOnHardReload: census.latestOnHardReload,
        fabHidden: census.fabHidden,
        slotItemClasses: slotItems,
      };
    }, MID);

    if (!result.replayAtStart || !result.replayDuringMix) {
      failures.push('historyReplayActive was false while applying the mix (got start=' +
        result.replayAtStart + ' during=' + result.replayDuringMix + ')');
    }
    if (result.replayAfterMeta) {
      failures.push('historyReplayActive still true after history_meta');
    }
    if (result.toolUse < MID || result.notes < MID || result.systems < MID) {
      failures.push('tape was not the daily mix: tool_use=' + result.toolUse +
        ' notes=' + result.notes + ' system=' + result.systems);
    }
    if (result.emptySlots > 0 || result.emptySlotDesert) {
      failures.push('empty turn-slot desert: emptySlots=' + result.emptySlots +
        ' labelled=' + result.labelledSlots);
    }
    if (result.labelledSlots < 1) {
      failures.push('notes/tools between owner turns minted no labelled ⋯ n steps slot');
    }
    if (result.modelRows >= result.eventsFed - 4) {
      failures.push('host minted ~one row per event during replay (startTurn desert): rows=' +
        result.modelRows + ' events=' + result.eventsFed);
    }
    const classes = result.slotItemClasses || [];
    if (classes.indexOf('tool-use') < 0) {
      failures.push('labelled slot has no tool-use item (got ' + JSON.stringify(classes) + ')');
    }
    if (classes.indexOf('agent-note') < 0) {
      failures.push('labelled slot has no agent-note item (got ' + JSON.stringify(classes) + ')');
    }
    if (result.visibleBubbles < 2 || result.packedPaneFail) {
      failures.push('pane not packed: visibleBubbles=' + result.visibleBubbles);
    }
    if (result.latestOnHardReload || result.fabHidden === false) {
      failures.push('Latest visible after replay pin: fabHidden=' + result.fabHidden);
    }
  } catch (e) {
    failures.push('exception: ' + (e && e.stack ? e.stack : e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL t494.1.1-replay-mix-test:');
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log('ok - host apply during historyReplayActive coalesces daily mix, no desert (🎯T494.1.1)');
})();
