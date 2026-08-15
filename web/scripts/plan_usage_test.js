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
//   clause 1  a running backend shows BOTH window percentages and the rollover
//   clause 2  a backend publishing nothing says "unavailable" — never blank/0
//   clause 5c a reading that has aged out is marked stale, not served as current
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

  // The ticker line itself carries all three facts, not just the model.
  assert.ok(view.text.indexOf('62% session') >= 0, 'line must show session remaining: ' + view.text);
  assert.ok(view.text.indexOf('29% weekly') >= 0, 'line must show weekly remaining: ' + view.text);
  assert.ok(view.text.indexOf('1h37m') >= 0, 'line must show the next rollover: ' + view.text);
  // And the absolute rollover time, which the line only had room to give as
  // a duration, is in the hover detail.
  assert.ok(view.title.indexOf('rolls over') >= 0, 'title must give the absolute rollover: ' + view.title);

  // CONTROL: the same backend with its windows withheld. If the predicate
  // passed on this too it would be measuring nothing, and a producer that
  // stopped publishing windows would draw a healthy-looking row.
  const stripped = PU.formatPlanUsage(snapshot([claudeBackend({ windows: [] })]), NOW);
  const ctl = rowFor(stripped, 'claude');
  assert.ok(!rendersBothWindows(ctl), 'control: a backend with no windows must not render as a reading');
  assert.strictEqual(ctl.available, false, 'control: available-with-no-windows is not available');
});

test('clause 1: the line covers the backends the fleet is running on, others go to the tooltip', function () {
  const idleCodex = {
    provider: 'codex',
    status: 'available',
    fleet_agents: 0,
    fetched_at: iso(NOW),
    age_seconds: 0,
    windows: [{ name: 'session', remaining_percent: 4, used_percent: 96, resets_at: iso(NOW + HOUR) }]
  };
  const view = PU.formatPlanUsage(snapshot([claudeBackend(), idleCodex]), NOW);

  assert.deepStrictEqual(view.rows.map(function (r) { return r.provider; }), ['claude'],
    'only backends with running agents belong on the line');
  assert.deepStrictEqual(view.others.map(function (r) { return r.provider; }), ['codex']);
  assert.ok(view.text.indexOf('codex') < 0, 'an idle backend must not crowd the line: ' + view.text);
  assert.ok(view.title.indexOf('codex') >= 0, 'an idle backend is still an answer, in the tooltip');
  // An exhausted allowance on a backend nobody is running is not a reason to
  // paint the line red — the same judgment capacity makes (🎯T390 clause 4).
  assert.strictEqual(view.className, '', 'an idle backend must not colour the line: ' + view.className);

  // CONTROL: an idle fleet would leave the line empty, which reads as
  // breakage rather than as an answer, so with nothing running it shows all.
  const idle = PU.formatPlanUsage(snapshot([claudeBackend({ fleet_agents: 0 })]), NOW);
  assert.strictEqual(idle.visible, true);
  assert.deepStrictEqual(idle.rows.map(function (r) { return r.provider; }), ['claude'],
    'control: with no backend running, show them all rather than nothing');
});

// ── clause 2 ────────────────────────────────────────────────────────────────

test('clause 2: an unavailable backend says so out loud, never a blank or a zero', function () {
  const view = PU.formatPlanUsage(snapshot([claudeBackend(), grokBackend()]), NOW);

  const g = rowFor(view, 'grok');
  assert.strictEqual(g.available, false);
  assert.strictEqual(g.status, PU.STATUS_UNAVAILABLE);
  assert.ok(g.text.indexOf('unavailable') >= 0, 'the row must say the word: ' + g.text);
  assert.ok(view.text.indexOf('grok unavailable') >= 0,
    'the visible line — not only the model — must say it: ' + view.text);
  assert.ok(view.title.indexOf('SuperGrok publishes no plan-remaining API') >= 0,
    "the provider's own reason must reach the owner: " + view.title);

  // The specific lie this rules out: a percentage where none was published.
  assert.strictEqual(g.text.indexOf('%'), -1, 'no invented percentage: ' + g.text);
  assert.deepStrictEqual(g.windows, [], 'an unavailable backend publishes no windows');
  assert.strictEqual(windowFor(g, 'session'), null, 'no invented session window');

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
  assert.ok(view.text.indexOf('stale 40m') >= 0, 'the line must carry the staleness: ' + view.text);
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

// ── the wiring itself ───────────────────────────────────────────────────────

test('index.html loads plan_usage.js, fetches the API, and renders it', function () {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.ok(html.indexOf('scripts/plan_usage.js') >= 0, 'must script-src plan_usage.js');
  assert.ok(html.indexOf('formatPlanUsage') >= 0, 'must call formatPlanUsage');
  assert.ok(html.indexOf('/api/plan-usage') >= 0, 'must fetch the served payload');
  assert.ok(html.indexOf('plan-ticker') >= 0, 'must have somewhere to render it');
});

test('web/embed.go embeds plan_usage.js so a released binary serves it', function () {
  const embed = fs.readFileSync(path.join(__dirname, '..', 'embed.go'), 'utf8');
  assert.ok(embed.indexOf('scripts/plan_usage.js') >= 0,
    'a module referenced by index.html and absent from the embed list is a 404 in a brew-installed daemon');
});
