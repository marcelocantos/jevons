// 🎯T603: does the cockpit tell the truth?
//
// A cockpit that looks plausible is not the same as one that is correct.
// This compares what the UI actually paints against the daemon's own APIs
// and against the Claudia broker's live grants — three independent sources — and fails
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
const { execFileSync } = require('child_process');
const { chromium } = require(path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright'));

const BASE = process.env.JEVONS_URL || 'http://localhost:13705';
const CLAUDIA = process.env.CLAUDIA_BIN || 'claudia';

const get = p => new Promise((res, rej) => http.get(BASE + p, r => {
  let d = ''; r.on('data', c => (d += c));
  r.on('end', () => { try { res(JSON.parse(d)); } catch (e) { rej(new Error(p + ': ' + e.message)); } });
}).on('error', rej));

function brokerGrants() {
  const lines = execFileSync(CLAUDIA, ['broker', 'grants'], { encoding: 'utf8' }).trimEnd().split('\n');
  const header = lines.shift();
  if (!header || !header.includes('NAME') || !header.includes('OWNED') || !header.includes('ALIVE')) {
    throw new Error('claudia broker grants returned an unknown format');
  }
  const field = (line, name, next) => line.slice(header.indexOf(name), header.indexOf(next)).trim();
  return new Map(lines.map(line => [
    field(line, 'NAME', 'PROVIDER'),
    { owned: field(line, 'OWNED', 'OWNER') === 'true', alive: field(line, 'ALIVE', 'PENDING') === 'true' },
  ]));
}

(async () => {
  const issues = [];
  const note = m => issues.push(m);

  let api;
  try { api = await get('/api/agents'); }
  catch (e) { console.error('OUTAGE: no daemon at ' + BASE + ' (' + e.message + ')'); process.exit(2); }
  const agents = Array.isArray(api) ? api : api.agents || [];
  const plan = await get('/api/plan-usage');

  let grants = null;
  try {
    grants = brokerGrants();
  } catch (e) {
    note('cannot read Claudia broker grants: ' + e.message);
  }

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
        modelTitle: n.querySelector('.model-badge')?.getAttribute('title') || '',
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
  // 2. The fleet the daemon has is held alive by the host broker (🎯T875).
  if (grants) for (const a of agents) {
    const grant = grants.get(a.name);
    if (!grant || !grant.owned || !grant.alive) {
      note(`${a.name}: /api/agents reports a seat but Claudia grant is ${
        grant ? `owned=${grant.owned} alive=${grant.alive}` : 'missing'}`);
    }
  }
  // 3. The badge's full title names the model actually pinned; its visible
  // abbreviation is typography and changes independently of the model id.
  for (const a of ui.agents) {
    const m = agents.find(x => x.name === a.name);
    if (m?.model && !a.modelTitle.includes(m.model)) {
      note(`${a.name}: badge title "${a.modelTitle}" but model is ${m.model}`);
    }
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

  console.log(`cockpit-truth: ${agents.length} agents, ${grants ? grants.size : '?'} broker grants, ` +
    `${ui.bars} bars, ${ui.rows} rows, ${ui.fromBottom}px from end`);
  if (issues.length) {
    console.error('\nMISMATCH — the cockpit is not telling the truth:');
    for (const i of issues) console.error('  - ' + i);
    process.exit(1);
  }
  console.log('cockpit-truth: UI, daemon and Claudia broker agree');
})();
