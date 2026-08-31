// 🎯T603: does the cockpit tell the truth?
//
// A cockpit that looks plausible is not the same as one that is correct.
// This compares what the UI actually paints against the daemon's own APIs
// and against the live tmux server — three independent sources — and fails
// when they disagree.
//
// It exists because on 2026-08-31 the fleet panel showed seven agents
// "running" when the tmux server held none at all: Agent.Alive() returned a
// cached flag, so the registry, the UI and reality had drifted apart with
// nothing to notice (🎯T602). A green unit suite said nothing about it.
//
//   node scripts/cockpit-truth/check.cjs            (needs a running daemon)
//   make test-cockpit-truth
const path = require('path');
const http = require('http');
const { execSync } = require('child_process');
const { chromium } = require(path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright'));

const BASE = process.env.JEVONS_URL || 'http://localhost:13705';
const SOCK = process.env.CLAUDIA_TMUX_SOCK ||
  path.join(process.env.HOME, '.local', 'state', 'claudia', 'tmux.sock');

const get = p => new Promise((res, rej) => http.get(BASE + p, r => {
  let d = ''; r.on('data', c => (d += c));
  r.on('end', () => { try { res(JSON.parse(d)); } catch (e) { rej(new Error(p + ': ' + e.message)); } });
}).on('error', rej));

// Badge text the cockpit paints for a model id.
const BADGE = { 'claude-opus-5': 'O5', 'claude-fable-5': 'F5', 'claude-sonnet-5': 'S5',
  'claude-haiku-4-5': 'H4', 'grok-4.5': 'G4', 'grok-4': 'G4' };

(async () => {
  const issues = [];
  const note = m => issues.push(m);

  let api;
  try { api = await get('/api/agents'); }
  catch (e) { console.error('OUTAGE: no daemon at ' + BASE + ' (' + e.message + ')'); process.exit(2); }
  const agents = Array.isArray(api) ? api : api.agents || [];
  const plan = await get('/api/plan-usage');

  let panes = null;
  try {
    panes = parseInt(execSync(
      `tmux -S ${SOCK} list-windows -a -F '#{window_name}' 2>/dev/null | grep -c claudia || true`
    ).toString().trim(), 10);
  } catch { /* no tmux server: leave null and say so rather than guess */ }

  const b = await chromium.launch();
  const p = await b.newPage({ viewport: { width: 1500, height: 950 } });
  const consoleErrors = [];
  p.on('console', m => { if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 160)); });
  p.on('pageerror', e => consoleErrors.push('pageerror: ' + String(e).slice(0, 160)));
  await p.goto(BASE + '/', { waitUntil: 'networkidle' });
  await p.waitForTimeout(Number(process.env.SETTLE_MS || 6000));

  const ui = await p.evaluate(() => {
    const txt = e => ((e && e.textContent) || '').trim();
    const sc = document.getElementById('messages');
    return {
      agents: [...document.querySelectorAll('.agent-node')].map(n => ({
        name: txt(n.querySelector('.agent-name')) || txt(n).split('\n')[0],
        badge: txt(n.querySelector('.model-badge')),
      })).filter(a => a.name),
      bars: [...document.querySelectorAll('#plan-ticker .plan-win')].length,
      rows: sc ? sc.querySelectorAll('[data-kind]').length : 0,
      fromBottom: sc ? Math.round(sc.scrollHeight - sc.scrollTop - sc.clientHeight) : null,
      composerReady: !!document.querySelector('#composer textarea, .composer textarea, textarea'),
    };
  });
  await b.close();

  // 1. The fleet the UI shows is the fleet the daemon has.
  const uiNames = ui.agents.map(a => a.name).sort();
  const apiNames = agents.map(a => a.name).sort();
  if (JSON.stringify(uiNames) !== JSON.stringify(apiNames)) {
    note(`fleet panel disagrees with /api/agents:\n      UI  ${uiNames.join(' ')}\n      API ${apiNames.join(' ')}`);
  }
  // 2. The fleet the daemon has is the fleet that exists (🎯T602).
  if (panes !== null && panes !== agents.length) {
    note(`/api/agents reports ${agents.length} agents but tmux holds ${panes} panes`);
  }
  // 3. Model badges name the model actually pinned.
  for (const a of ui.agents) {
    const m = agents.find(x => x.name === a.name);
    if (!m || !a.badge) continue;
    const want = BADGE[m.model] || m.model;
    if (a.badge !== want) note(`${a.name}: badge "${a.badge}" but model is ${m.model}`);
  }
  // 4. One bar per published window.
  const wins = (plan.backends || []).flatMap(x => (x.windows || []).length ? x.windows : []);
  if (ui.bars !== wins.length) note(`plan ticker shows ${ui.bars} bars for ${wins.length} published windows`);
  // 5. The transcript is at the live end (🎯T587 / 🎯T603).
  if (ui.rows > 0 && ui.fromBottom !== null && ui.fromBottom > 40) {
    note(`transcript is ${ui.fromBottom}px from the live end on a fresh load`);
  }
  // 6. The owner can actually type.
  if (!ui.composerReady) note('no composer on the page — the owner cannot send anything');
  if (consoleErrors.length) note('console errors: ' + consoleErrors.join(' | '));

  console.log(`cockpit-truth: ${agents.length} agents, ${panes === null ? '?' : panes} panes, ` +
    `${ui.bars} bars, ${ui.rows} rows, ${ui.fromBottom}px from end`);
  if (issues.length) {
    console.error('\nMISMATCH — the cockpit is not telling the truth:');
    for (const i of issues) console.error('  - ' + i);
    process.exit(1);
  }
  console.log('cockpit-truth: UI, daemon and tmux agree');
})();
