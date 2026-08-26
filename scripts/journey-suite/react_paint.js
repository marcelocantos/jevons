// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Playwright sidecar for React chrome journeys (🎯T540.1).
// Referent: retired target acceptance + vanilla cockpit chrome
// (#input, #messages, .msg.user, .msg-clipped, #frontier-table, …).
// The running React tree is not the referent — missing or renamed
// chrome is a FAIL (gap), not a reason to relax the check.
//
//   node scripts/journey-suite/react_paint.js --host 127.0.0.1:PORT \
//        --scenario send|fold-md|composer|fleet|aside|frontier|ticker \
//        [--token TXT] [--screenshot PATH]
//
// Exit 2 on usage / daily-port.

'use strict';

const path = require('path');
const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const DAILY_PORT = 13705;
const VIEWPORT = { width: 1280, height: 800 };

const argv = process.argv.slice(2);
function opt(name, def) {
  const i = argv.indexOf('--' + name);
  if (i === -1) return def;
  const next = argv[i + 1];
  return next && !next.startsWith('--') ? next : true;
}

const HOST = String(opt('host', '') || '');
const SCENARIO = String(opt('scenario', '') || '');
const TOKEN = String(opt('token', 'T540SEND') || 'T540SEND');
const AGENT = String(opt('agent', 'jv-t540-fleet-oracle') || 'jv-t540-fleet-oracle');
const SCREENSHOT = String(opt('screenshot', '') || '');

function die(code, msg) {
  console.error(msg);
  process.exit(code);
}

if (!HOST) die(2, 'react_paint: --host HOST:PORT is required');
if (HOST.indexOf(':' + DAILY_PORT) !== -1 || HOST === String(DAILY_PORT)) {
  die(2, 'react_paint: refuses daily port ' + DAILY_PORT + ' (Universe A)');
}
if (!SCENARIO) die(2, 'react_paint: --scenario is required');

function fail(id, msg) {
  throw new Error(id + ': ' + msg);
}

async function waitReact(page) {
  await page.goto('http://' + HOST + '/', { waitUntil: 'domcontentloaded', timeout: 30000 });
  // #root is the React mount, not chrome. Vanilla owner chrome is asserted
  // per-scenario (#messages, #input, …). Do not treat today's tree as ready.
  await page.waitForFunction(() => !!document.getElementById('root'), null, { timeout: 20000 });
  await page.waitForFunction(() => {
    const st = document.getElementById('status-text');
    return st && String(st.textContent || '') === 'connected';
  }, null, { timeout: 25000 }).catch(() => {});
}

async function shot(page) {
  if (!SCREENSHOT) return;
  const pane = await page.$('#messages');
  if (pane) await pane.screenshot({ path: SCREENSHOT });
  else await page.screenshot({ path: SCREENSHOT });
}

// Vanilla owner-visible chrome the retired targets named.
async function requireIds(page, ids, tid) {
  const missing = await page.evaluate((want) => {
    return want.filter((id) => !document.getElementById(id));
  }, ids);
  if (missing.length) fail(tid, 'vanilla chrome missing: #' + missing.join(' #'));
}

async function scenarioSend(page) {
  // T228 / T279 / T281 / T504: submit is visible exactly once; reply is below the user bubble.
  await requireIds(page, ['input', 'send', 'messages'], 'T228');
  const box = page.locator('#input');
  await box.click();
  await box.fill(TOKEN);
  await page.locator('#send').click();
  await page.waitForFunction((tok) => {
    const users = [...document.querySelectorAll('#messages .msg.user')];
    return users.some((el) => (el.textContent || '').indexOf(tok) >= 0);
  }, TOKEN, { timeout: 20000 }).catch(() => {
    fail('T279', 'owner-submitted text never became a visible .msg.user bubble (vanilla class)');
  });
  const counts = await page.evaluate((tok) => {
    const users = [...document.querySelectorAll('#messages .msg.user')];
    const hit = users.filter((el) => (el.textContent || '').indexOf(tok) >= 0);
    const first = hit[0];
    let after = null;
    if (first) {
      let n = first.nextElementSibling;
      while (n) {
        if (/\bmsg\b/.test(n.className) && /\b(assistant|jevons)\b/.test(n.className)) {
          after = n.className;
          break;
        }
        n = n.nextElementSibling;
      }
    }
    return { n: hit.length, after };
  }, TOKEN);
  if (counts.n !== 1) fail('T281', 'owner send must paint exactly once; saw ' + counts.n + ' .msg.user with token');
  // T504: a later assistant, if present, must sit after the user bubble — not above it.
  const order = await page.evaluate((tok) => {
    const rows = [...document.querySelectorAll('#messages .msg')];
    const idxUser = rows.findIndex((el) => (el.textContent || '').indexOf(tok) >= 0 && /\buser\b/.test(el.className));
    const idxAsst = rows.findIndex((el, i) => i > idxUser && /\b(assistant|jevons)\b/.test(el.className));
    return { idxUser, idxAsst };
  }, TOKEN);
  if (order.idxUser < 0) fail('T504', 'user bubble is not a .msg.user stream barrier');
}

async function scenarioFoldMd(page) {
  // T106 pocket, T59 mermaid, T238 silent must not be an owner bubble.
  await requireIds(page, ['messages'], 'T106');
  const clip = await page.locator('.msg.msg-clipped').count();
  if (clip < 1) {
    fail('T106', 'tall content must be a .msg.msg-clipped pocket (vanilla 14rem clip + edge tab)');
  }
  const tab = await page.locator('.msg-expand-tab').count();
  if (tab < 1) fail('T106', 'clipped pocket must carry the vanilla .msg-expand-tab');
  const mermaidOk = await page.waitForFunction(() => {
    return document.querySelectorAll('#messages svg, #messages .mermaid').length > 0;
  }, null, { timeout: 15000 }).then(() => true).catch(() => false);
  if (!mermaidOk) {
    const dump = await page.evaluate(() => {
      const codes = [...document.querySelectorAll('#messages pre, #messages code')].map((el) => el.className + ':' + (el.textContent || '').slice(0, 40));
      const w = window;
      return {
        codes,
        mermaidErrs: w.__jevonsMermaidErrs || [],
        html: (document.getElementById('messages')?.innerHTML || '').slice(0, 600),
      };
    });
    fail('T59', '```mermaid fence must render as a diagram (svg), not raw source; saw ' + JSON.stringify(dump));
  }
  const silentLeak = await page.evaluate(() => {
    const users = [...document.querySelectorAll('#messages .msg.user')];
    return users.some((el) => /\[silent\]/i.test(el.textContent || ''));
  });
  if (silentLeak) fail('T238', '[silent] replies must never appear as owner .msg.user bubbles');
  const rawFence = await page.evaluate(() => {
    const asst = [...document.querySelectorAll('#messages .msg.assistant, #messages .msg.jevons')];
    return asst.some((el) => /```mermaid/.test(el.textContent || ''));
  });
  if (rawFence) fail('T59', 'mermaid source leaked as text — renderer did not draw the diagram');
}

async function scenarioComposer(page) {
  // T126 Home/End, T123 empty height vs send, T478 one-control-tall after send.
  await requireIds(page, ['input', 'send', 'input-bar'], 'T123');
  const box = page.locator('#input');
  await box.click();
  await box.fill('alpha bravo charlie');
  await page.keyboard.press('Home');
  const start = await box.evaluate((el) => el.selectionStart);
  if (start !== 0) fail('T126', 'Home must move the caret to the start of the composer field; selectionStart=' + start);
  await page.keyboard.press('End');
  const end = await box.evaluate((el) => ({ s: el.selectionStart, len: el.value.length }));
  if (end.s !== end.len) fail('T126', 'End must move the caret to the end of the composer field');
  const heights = await page.evaluate(() => {
    const input = document.getElementById('input');
    const send = document.getElementById('send');
    const bar = document.getElementById('input-bar');
    return {
      input: input ? input.getBoundingClientRect().height : 0,
      send: send ? send.getBoundingClientRect().height : 0,
      bar: bar ? bar.getBoundingClientRect().height : 0,
    };
  });
  await box.fill('');
  const empty = await page.evaluate(() => {
    const input = document.getElementById('input');
    const send = document.getElementById('send');
    return {
      input: input ? input.getBoundingClientRect().height : 0,
      send: send ? send.getBoundingClientRect().height : 0,
    };
  });
  if (Math.abs(empty.input - empty.send) > 8) {
    fail('T123', 'empty composer height must match the send button (vanilla one-control-tall); input=' + empty.input + ' send=' + empty.send);
  }
  void heights;
}

async function scenarioFleet(page) {
  // T68 / T72 / T72.1: RHS tree shows the live agent graph.
  await requireIds(page, ['agents'], 'T72');
  const found = await page.evaluate((name) => {
    const root = document.getElementById('agents');
    if (!root) return false;
    return (root.textContent || '').indexOf(name) >= 0;
  }, AGENT);
  if (!found) fail('T72.1', 'live fleet agent ' + AGENT + ' must appear in #agents (vanilla RHS tree)');
  const tree = await page.locator('#agents .agent-node').count();
  if (tree < 1) fail('T68', 'RHS must paint who-started-whom as a relationship tree (.agent-node)');
}

async function scenarioAside(page) {
  // T95 / T250: target: opens an aside; it is not a main .msg.user.
  await requireIds(page, ['input', 'send', 'messages'], 'T95');
  const box = page.locator('#input');
  await box.click();
  await box.fill('target: T540.1 census aside from journey');
  await page.locator('#send').click();
  await page.evaluate(() => new Promise((r) => setTimeout(r, 800)));
  const leaked = await page.evaluate(() => {
    const users = [...document.querySelectorAll('#messages .msg.user')];
    return users.some((el) => /target:\s*T540\.1 census aside/.test(el.textContent || ''));
  });
  if (leaked) fail('T250', 'asides must not be visible in the main transcript as .msg.user');
  const inspect = await page.locator('#agent-inspect-input').count();
  if (inspect < 1) fail('T251', 'sidebar transcript must have its own composer (#agent-inspect-input)');
}

async function scenarioFrontier(page) {
  // T131 / T168 / T173 / T185: headerless table + Graph control.
  const tab = page.locator('#rhs-tab-frontier');
  if ((await tab.count()) < 1) fail('T131', 'RHS must have a Frontier tab (#rhs-tab-frontier)');
  await tab.click();
  await requireIds(page, ['frontier-pane', 'frontier-graph', 'frontier-body'], 'T185');
  const table = await page.locator('#frontier-table').count();
  if (table < 1) fail('T173', 'Frontier tab must paint a headerless table (vanilla #frontier-table)');
  const thead = await page.locator('#frontier-table thead').count();
  if (thead > 0) {
    const visibleHead = await page.locator('#frontier-table thead').isVisible().catch(() => false);
    if (visibleHead) fail('T173', 'Frontier table must be headerless');
  }
  await page.locator('#frontier-graph').click();
  const graph = await page.locator('#mermaid-viz-panel').count();
  if (graph < 1) fail('T185', 'Graph control must open #mermaid-viz-panel (vanilla near-full-page graph)');
}

async function scenarioTicker(page) {
  // T390 / T117: plan usage is a real ticker, not an invented zero.
  await requireIds(page, ['plan-ticker'], 'T390');
  const ticker = page.locator('#plan-ticker');
  const box = await ticker.boundingBox();
  if (!box || box.width < 8 || box.height < 8) {
    fail('T390', '#plan-ticker must be owner-visible (percent remaining + rollover), not an empty stub');
  }
  await requireIds(page, ['rhs-width-handle'], 'T248');
  const handle = await page.locator('#rhs-width-handle').boundingBox();
  if (!handle) fail('T248', 'owner must be able to drag-resize RHS width (#rhs-width-handle)');
}

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: VIEWPORT,
    screen: VIEWPORT,
    deviceScaleFactor: 1,
  });
  const page = await context.newPage();
  try {
    await waitReact(page);
    switch (SCENARIO) {
      case 'send':
        await scenarioSend(page);
        break;
      case 'fold-md':
        await scenarioFoldMd(page);
        break;
      case 'composer':
        await scenarioComposer(page);
        break;
      case 'fleet':
        await scenarioFleet(page);
        break;
      case 'aside':
        await scenarioAside(page);
        break;
      case 'frontier':
        await scenarioFrontier(page);
        break;
      case 'ticker':
        await scenarioTicker(page);
        break;
      default:
        die(2, 'react_paint: unknown scenario ' + SCENARIO);
    }
    await shot(page);
    console.log(JSON.stringify({ scenario: SCENARIO, ok: true }));
  } catch (err) {
    try { await shot(page); } catch (_) { /* ignore */ }
    console.error(String(err && err.message ? err.message : err));
    process.exit(1);
  } finally {
    await browser.close();
  }
})();
