// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Acceptance oracle for 🎯T390 clauses 1, 2 and 5 at the render layer, over
// fixture payloads shaped exactly like GET /api/plan-usage.
//
// The Go oracle (internal/planusage/t390_oracle_test.go) asserts the bytes
// the daemon serves. This one asserts what the owner is shown, which is the
// half the target is named for — the producer, the consumer and the API all
// existed while the cockpit still displayed nothing at all.
//
//   clause 1  a published window renders as `cl/s 62%` (abbrev + percent)
//   clause 2  a backend publishing nothing says "unavailable" — never blank/0
//   clause 5c a reading that has aged out is marked stale, not served as current
//
// Owner pin 2026-08-15: the bar is in #status next to #theme-toggle, always
// visible, compact bars. A publisher is on the bar even with no agent running.
// These placement / idle-publisher checks are red against the pre-fix tree
// (RHS #plan-ticker, display:none, running-only filter).
//
// Every property carries a CONTROL: an input mutated so the property must
// fail. Two of these three regressions are silent — they draw a plausible
// number rather than an error — and a check that cannot go red is not
// evidence the code is right, only that the assertion is weak.

'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const PU = require('./plan_usage.js');

function test(name, fn) {
  try {
    fn();
    console.log('ok - ' + name);
  } catch (e) {
    console.error('not ok - ' + name);
    console.error(e);
    process.exitCode = 1;
  }
}

// The oracle's clock. Every fixture is expressed against it, so staleness and
// rollover are decided by arithmetic rather than by how long the test took.
const NOW = Date.parse('2026-08-15T12:00:00Z');
const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;

function iso(ms) { return new Date(ms).toISOString(); }

// claudeBackend is the shape the daemon serves for a Claude OAuth account:
// five_hour→session and seven_day→weekly, both with a rollover.
function claudeBackend(over) {
  return Object.assign({
    provider: 'claude',
    status: 'available',
    plan_type: 'max',
    fleet_agents: 7,
    fetched_at: iso(NOW - 30 * 1000),
    age_seconds: 30,
    windows: [
      { name: 'session', remaining_percent: 62, used_percent: 38, resets_at: iso(NOW + 97 * MINUTE) },
      { name: 'weekly', remaining_percent: 29, used_percent: 71, resets_at: iso(NOW + 52 * HOUR) }
    ]
  }, over || {});
}

// grokBackend is the honest residual inherited from claudia 🎯T18: SuperGrok
// publishes no remaining, so the answer is a stated unavailable with a reason.
// It is also the fleet's default backend, so this row is on screen every day.
function grokBackend(over) {
  return Object.assign({
    provider: 'grok',
    status: 'unavailable',
    reason: 'SuperGrok publishes no plan-remaining API',
    fleet_agents: 3,
    fetched_at: iso(NOW),
    age_seconds: 0
  }, over || {});
}

function snapshot(backends) {
  return { at: iso(NOW), backends: backends };
}

function rowFor(view, provider) {
  const all = (view.rows || []).concat(view.others || []);
  for (let i = 0; i < all.length; i++) {
    if (all[i].provider === provider) return all[i];
  }
  throw new Error('provider ' + provider + ' absent from rendered view: ' + JSON.stringify(view));
}

function windowFor(row, name) {
  for (let i = 0; i < (row.windows || []).length; i++) {
    if (row.windows[i].name === name) return row.windows[i];
  }
  return null;
}

function chipFor(view, key) {
  const chips = view.chips || [];
  for (let i = 0; i < chips.length; i++) {
    if (chips[i].key === key) return chips[i];
  }
  return null;
}

// rendersBothWindows is clause 1 as a predicate, so the control can assert it
// goes false rather than duplicating the check with its sense flipped.
function rendersBothWindows(row) {
  const names = [PU.WINDOW_SESSION, PU.WINDOW_WEEKLY];
  for (let i = 0; i < names.length; i++) {
    const w = windowFor(row, names[i]);
    if (!w) return false;
    if (typeof w.remainingText !== 'string' || w.remainingText === '') return false;
    if (typeof w.rollsInText !== 'string' || w.rollsInText === '') return false;
  }
  return true;
}

// ── abbreviations ───────────────────────────────────────────────────────────

test('provider and window abbreviations match the owner pin', function () {
  assert.strictEqual(PU.providerAbbrev('claude'), 'cl');
  assert.strictEqual(PU.providerAbbrev('codex'), 'cx');
  assert.strictEqual(PU.providerAbbrev('grok'), 'gk');
  assert.strictEqual(PU.providerAbbrev('bedrock'), 'bd');
  assert.strictEqual(PU.windowAbbrev('session'), 's');
  assert.strictEqual(PU.windowAbbrev('weekly'), 'w');
});

// ── clause 1 ────────────────────────────────────────────────────────────────

test('clause 1: a running backend renders both percentages and the rollover', function () {
  const view = PU.formatPlanUsage(snapshot([claudeBackend()]), NOW);

  assert.strictEqual(view.visible, true, 'a fleet running on a metered backend must show a line');
  const row = rowFor(view, 'claude');
  assert.strictEqual(row.available, true);
  assert.ok(rendersBothWindows(row),
    'session and weekly must each render a percentage and a rollover, got ' + JSON.stringify(row.windows));

  const session = windowFor(row, 'session');
  assert.strictEqual(session.remainingText, '62%', "claudia's number, unaltered");
  assert.strictEqual(session.usedText, '38%');
  assert.strictEqual(session.rollsInText, '1h37m', '97 minutes out');
  const weekly = windowFor(row, 'weekly');
  assert.strictEqual(weekly.remainingText, '29%');
  assert.strictEqual(weekly.rollsInText, '2d 4h', '52 hours out');

  // Compact header form: abbrev + percent, not "62% session".
  assert.ok(view.text.indexOf('cl/s 62%') >= 0, 'line must show session remaining: ' + view.text);
  assert.ok(view.text.indexOf('cl/w 29%') >= 0, 'line must show weekly remaining: ' + view.text);
  const clS = chipFor(view, 'cl/s');
  assert.ok(clS, 'session chip must exist');
  assert.strictEqual(clS.remainingPercent, 62, 'bar width is the published remaining, not decoration');
  const clW = chipFor(view, 'cl/w');
  assert.ok(clW, 'weekly chip must exist');
  assert.strictEqual(clW.remainingPercent, 29);
  // The absolute rollover time lives in the hover detail — the bar only
  // has room for the percentage.
  assert.ok(view.title.indexOf('rolls over') >= 0, 'title must give the absolute rollover: ' + view.title);
  assert.ok(view.title.indexOf('1h37m') < 0 || view.title.indexOf('rolls over') >= 0);

  // CONTROL: the same backend with its windows withheld. If the predicate
  // passed on this too it would be measuring nothing, and a producer that
  // stopped publishing windows would draw a healthy-looking row.
  const stripped = PU.formatPlanUsage(snapshot([claudeBackend({ windows: [] })]), NOW);
  const ctl = rowFor(stripped, 'claude');
  assert.ok(!rendersBothWindows(ctl), 'control: a backend with no windows must not render as a reading');
  assert.strictEqual(ctl.available, false, 'control: available-with-no-windows is not available');
  assert.strictEqual(chipFor(stripped, 'cl/s'), null, 'control: no invented session chip');
});

test('clause 1: a backend that publishes remaining % is on the bar even if idle', function () {
  const idleCodex = {
    provider: 'codex',
    status: 'available',
    fleet_agents: 0,
    fetched_at: iso(NOW),
    age_seconds: 0,
    windows: [{ name: 'weekly', remaining_percent: 100, used_percent: 0, resets_at: iso(NOW + HOUR) }]
  };
  const view = PU.formatPlanUsage(snapshot([claudeBackend(), idleCodex]), NOW);

  assert.ok(view.text.indexOf('cl/s 62%') >= 0, view.text);
  assert.ok(view.text.indexOf('cl/w 29%') >= 0, view.text);
  assert.ok(view.text.indexOf('cx/w 100%') >= 0,
    'an idle publisher must still occupy the bar: ' + view.text);
  assert.ok(chipFor(view, 'cx/w'), 'idle weekly chip');
  assert.strictEqual(chipFor(view, 'cx/w').remainingPercent, 100);
  assert.deepStrictEqual(view.others, [], 'nothing is hidden in a tooltip-only bucket');

  // CONTROL: an idle fleet would previously have been the only path that
  // showed idle publishers. That control now has to stay green too — the
  // bar is the same whether anyone is running.
  const idle = PU.formatPlanUsage(snapshot([claudeBackend({ fleet_agents: 0 })]), NOW);
  assert.strictEqual(idle.visible, true);
  assert.ok(idle.text.indexOf('cl/s 62%') >= 0, 'control: idle claude still on the bar: ' + idle.text);
});

// ── clause 2 ────────────────────────────────────────────────────────────────

test('clause 2: an unavailable backend says so out loud, never a blank or a zero', function () {
  const view = PU.formatPlanUsage(snapshot([claudeBackend(), grokBackend()]), NOW);

  const g = rowFor(view, 'grok');
  assert.strictEqual(g.available, false);
  assert.strictEqual(g.status, PU.STATUS_UNAVAILABLE);
  assert.ok(g.text.indexOf('unavailable') >= 0, 'the row must say the word: ' + g.text);
  assert.ok(view.text.indexOf('gk unavailable') >= 0,
    'the visible line — not only the model — must say it: ' + view.text);
  assert.ok(view.title.indexOf('SuperGrok publishes no plan-remaining API') >= 0,
    "the provider's own reason must reach the owner: " + view.title);

  // The specific lie this rules out: a percentage where none was published.
  assert.strictEqual(g.text.indexOf('%'), -1, 'no invented percentage: ' + g.text);
  assert.deepStrictEqual(g.windows, [], 'an unavailable backend publishes no windows');
  assert.strictEqual(windowFor(g, 'session'), null, 'no invented session window');
  const gk = chipFor(view, 'gk');
  assert.ok(gk, 'unavailable chip occupies the bar');
  assert.strictEqual(gk.remainingPercent, null, 'unavailable chip carries no number for a bar fill');
  assert.ok(gk.text.indexOf('unavailable') >= 0, gk.text);

  // CONTROL: a real published zero. This is the distinction the whole clause
  // turns on — 0% remaining is a true and different statement from "nobody
  // publishes a number", and collapsing the two in either direction is the
  // bug. A consumer that rendered unavailable as 0% would pass the assertions
  // above only by also failing these.
  const exhausted = PU.formatPlanUsage(snapshot([claudeBackend({
    windows: [
      { name: 'session', remaining_percent: 0, used_percent: 100, resets_at: iso(NOW + 20 * MINUTE) },
      { name: 'weekly', remaining_percent: 0, used_percent: 100, resets_at: iso(NOW + 3 * HOUR) }
    ]
  })]), NOW);
  const z = rowFor(exhausted, 'claude');
  assert.strictEqual(z.available, true, 'control: a published zero is a reading, not an absence');
  assert.strictEqual(windowFor(z, 'session').remainingText, '0%',
    'control: a real zero renders as 0%');
  assert.ok(exhausted.text.indexOf('cl/s 0%') >= 0, 'control: ' + exhausted.text);
  assert.ok(z.text.indexOf('unavailable') < 0,
    'control: an exhausted plan is not an unavailable plan: ' + z.text);
  assert.strictEqual(exhausted.className, 'plan-crit', 'control: an exhausted plan colours the line');
});

test('clause 2: a producer claiming available while publishing nothing is downgraded, with a reason', function () {
  const buggy = { provider: 'codex', status: 'available', fleet_agents: 2, fetched_at: iso(NOW), age_seconds: 0 };
  const view = PU.formatPlanUsage(snapshot([buggy]), NOW);
  const row = rowFor(view, 'codex');
  assert.strictEqual(row.available, false, 'taken at face value this draws an empty row that looks like a reading');
  assert.ok(row.text.indexOf('unavailable') >= 0, row.text);
  assert.ok(view.title.indexOf('no plan-remaining published') >= 0,
    'the downgrade must say why it happened: ' + view.title);
});

test('clause 2: Grok and Bedrock both occupy the bar as the word, never a number', function () {
  const bedrock = {
    provider: 'bedrock',
    status: 'unavailable',
    reason: 'AWS Bedrock does not publish Claude-style session/weekly subscription remaining',
    fetched_at: iso(NOW),
    age_seconds: 0
  };
  const view = PU.formatPlanUsage(snapshot([
    grokBackend({ fleet_agents: 3 }),
    bedrock,
    claudeBackend({ fleet_agents: 0 }),
    {
      provider: 'codex',
      status: 'available',
      fleet_agents: 0,
      fetched_at: iso(NOW),
      age_seconds: 0,
      windows: [{ name: 'weekly', remaining_percent: 100, used_percent: 0, resets_at: iso(NOW + HOUR) }]
    }
  ]), NOW);

  // Owner example shape: cl/s · cl/w · cx/w, plus the two unavailables.
  assert.ok(view.text.indexOf('cl/s 62%') >= 0, view.text);
  assert.ok(view.text.indexOf('cl/w 29%') >= 0, view.text);
  assert.ok(view.text.indexOf('cx/w 100%') >= 0, view.text);
  assert.ok(view.text.indexOf('gk unavailable') >= 0, view.text);
  assert.ok(view.text.indexOf('bd unavailable') >= 0, view.text);
  assert.ok(view.text.indexOf('grok') < 0, 'header uses the abbrev, not the full name: ' + view.text);
  assert.strictEqual(chipFor(view, 'gk').remainingPercent, null);
  assert.strictEqual(chipFor(view, 'bd').remainingPercent, null);
});

// ── clause 5c ───────────────────────────────────────────────────────────────

test('clause 5c: an aged reading is marked stale rather than shown as current', function () {
  const AGE_SECONDS = 40 * 60;
  const view = PU.formatPlanUsage(snapshot([claudeBackend({
    stale: true,
    age_seconds: AGE_SECONDS,
    fetched_at: iso(NOW - AGE_SECONDS * 1000)
  })]), NOW);

  const row = rowFor(view, 'claude');
  assert.strictEqual(row.stale, true);
  assert.strictEqual(row.ageText, '40m', 'the owner reads the age, not just the flag');
  assert.ok(view.text.indexOf('stale') >= 0, 'the line must carry the staleness: ' + view.text);
  assert.ok(view.title.indexOf('shown stale rather than as current') >= 0, view.title);
  // Staleness must not blank the reading: "we last saw 62% forty minutes ago"
  // beats showing nothing at all.
  assert.ok(rendersBothWindows(row), 'a stale reading is still rendered, with its age');
  assert.strictEqual(view.className, 'plan-stale');

  // CONTROL: the identical reading inside the bound. Without this, a consumer
  // that marked everything stale would pass the assertions above and the flag
  // would carry no information.
  const fresh = PU.formatPlanUsage(snapshot([claudeBackend()]), NOW);
  assert.strictEqual(rowFor(fresh, 'claude').stale, false, 'control: a 30s-old reading is not stale');
  assert.ok(fresh.text.indexOf('stale') < 0, 'control: ' + fresh.text);
  assert.strictEqual(fresh.className, '', 'control: a fresh healthy reading colours nothing');
});

// ── surrounding honesty ─────────────────────────────────────────────────────

test('a daemon without plan usage hides the line; a query failure states itself', function () {
  assert.strictEqual(PU.formatPlanUsage(null, NOW).visible, false);
  assert.strictEqual(PU.formatPlanUsage({ disabled: true, error: 'plan usage not enabled' }, NOW).visible, false,
    'an old binary with no wiring has nothing to say — hiding is not a blank reading');
  assert.strictEqual(PU.formatPlanUsage(snapshot([]), NOW).visible, false);

  const pending = PU.formatPlanUsage({ pending: true }, NOW);
  assert.strictEqual(pending.visible, true);
  assert.ok(pending.text.indexOf('waiting') >= 0,
    'before the first fetch, say so — distinct from every backend being unavailable: ' + pending.text);

  const failed = PU.formatPlanUsage({ error: 'claudia refused the arguments' }, NOW);
  assert.strictEqual(failed.visible, true);
  assert.ok(failed.text.indexOf('claudia refused the arguments') >= 0, failed.text);
  assert.ok(failed.text.indexOf('unavailable') >= 0, failed.text);
});

test('low and critical thresholds colour the line, and the tightest window wins', function () {
  const low = PU.formatPlanUsage(snapshot([claudeBackend({
    windows: [
      { name: 'session', remaining_percent: 80, used_percent: 20, resets_at: iso(NOW + HOUR) },
      { name: 'weekly', remaining_percent: PU.LOW_PERCENT - 1, used_percent: 86, resets_at: iso(NOW + 30 * HOUR) }
    ]
  })]), NOW);
  assert.strictEqual(low.className, 'plan-low', 'the tightest window decides, not the first');
  assert.strictEqual(rowFor(low, 'claude').lowestRemaining, PU.LOW_PERCENT - 1);
  assert.strictEqual(chipFor(low, 'cl/w').className, 'plan-low');

  const crit = PU.formatPlanUsage(snapshot([claudeBackend({
    windows: [{ name: 'session', remaining_percent: PU.CRITICAL_PERCENT - 1, resets_at: iso(NOW + HOUR) }]
  })]), NOW);
  assert.strictEqual(crit.className, 'plan-crit');
});

test('humanDuration says it the way the owner would', function () {
  assert.strictEqual(PU.humanDuration(0), 'now');
  assert.strictEqual(PU.humanDuration(59), 'now');
  assert.strictEqual(PU.humanDuration(12 * 60), '12m');
  assert.strictEqual(PU.humanDuration(97 * 60), '1h37m');
  assert.strictEqual(PU.humanDuration(2 * 3600), '2h');
  assert.strictEqual(PU.humanDuration(52 * 3600), '2d 4h');
  assert.strictEqual(PU.humanDuration(48 * 3600), '2d');
  assert.strictEqual(PU.humanDuration(-500), 'now', 'a rollover already past is not a negative countdown');
});

test('percentText refuses to invent a number', function () {
  assert.strictEqual(PU.percentText(62), '62%');
  assert.strictEqual(PU.percentText(0), '0%', 'a published zero is a number');
  assert.strictEqual(PU.percentText(null), null, 'an absent reading is not 0% and not blank');
  assert.strictEqual(PU.percentText(undefined), null);
  assert.strictEqual(PU.percentText(NaN), null);
});

// ── the wiring itself (red against the pre-fix RHS ticker) ──────────────────

test('index.html loads plan_usage.js, fetches the API, and paints chips', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/plan_usage.js') >= 0, 'must script-src plan_usage.js');
  assert.ok(html.indexOf('formatPlanUsage') >= 0, 'must call formatPlanUsage');
  assert.ok(html.indexOf('paintPlanUsage') >= 0, 'must paint chips, not only set textContent');
  assert.ok(html.indexOf('/api/plan-usage') >= 0, 'must fetch the served payload');
  assert.ok(html.indexOf('id="plan-ticker"') >= 0, 'must have somewhere to render it');
});

test('index.html mounts the plan bar in #status next to #theme-toggle, not the RHS', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  const statusAt = html.indexOf('id="status"');
  const planAt = html.indexOf('id="plan-ticker"');
  const themeAt = html.indexOf('id="theme-toggle"');
  const activityAt = html.indexOf('id="activity-pane"');
  assert.ok(statusAt >= 0 && planAt >= 0 && themeAt >= 0, 'status, plan bar, and theme toggle must exist');
  assert.ok(statusAt < planAt && planAt < themeAt,
    'plan bar must sit inside #status immediately before #theme-toggle');
  assert.ok(activityAt < 0 || planAt < activityAt,
    'plan bar must not live in the RHS activity pane');

  // A second plan-ticker in the activity pane would be the old ticker left behind.
  const second = html.indexOf('id="plan-ticker"', planAt + 1);
  assert.strictEqual(second, -1, 'exactly one #plan-ticker');
});

test('index.html never hides #plan-ticker pending a fetch, and styles compact bars', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(!/#plan-ticker\s*\{[^}]*display\s*:\s*none/.test(html),
    '#plan-ticker must not start as display:none (the pre-fix tree hid it)');
  assert.ok(html.indexOf('.plan-bar') >= 0, 'compact bar indicator CSS');
  assert.ok(html.indexOf('.plan-bar-fill') >= 0, 'bar fill for remaining %');
  assert.ok(html.indexOf('display: none') < 0 || !/#plan-ticker\s*\{[^}]*display\s*:\s*none/.test(html));
  // The paint path must not re-hide the slot on a failed fetch.
  const paintBlock = html.slice(html.indexOf('function refreshPlanUsage'), html.indexOf('function refreshPlanUsage') + 1200);
  assert.ok(paintBlock.indexOf("display = 'none'") < 0 && paintBlock.indexOf('display = "none"') < 0,
    'refreshPlanUsage must not set display:none: ' + paintBlock.slice(0, 400));
});

test('web/embed.go embeds plan_usage.js so a released binary serves it', function () {
  const embed = fs.readFileSync(path.join(__dirname, '..', 'embed.go'), 'utf8');
  assert.ok(embed.indexOf('scripts/plan_usage.js') >= 0,
    'a module referenced by index.html and absent from the embed list is a 404 in a brew-installed daemon');
});
