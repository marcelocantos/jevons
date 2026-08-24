// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T390 / 🎯T390.1: the cockpit half of plan usage — how much of each
// backend's subscription allowance is left, when it rolls over, and
// whether spend is outrunning the clock. DOM-free so the hermetic Node
// test drives it with fixture payloads.
//
// The daemon serves the picture at GET /api/plan-usage; this turns it
// into the header bar. One rule still shapes every branch: a number that
// was never published is never rendered as a number. A blank or a 0%
// reads as "you have nothing left", which is a different and false
// statement from "nobody publishes this".
//
// 🎯T390.1 layout: one group per provider — T287 company mark, then a
// boxed pair of session/weekly remaining bars. A triangle under each bar
// sits at remaining-time fraction of the window. Bar and triangle go
// orange when spend outstrips time (used/elapsed > 1), red when it
// outstrips by a lot (used/elapsed > 1.5). Grok is a real weekly bar
// when the billing surface answers. Bedrock stays off the bar unless a
// fleet agent is running on it — AWS publishes no subscription remaining.
//
// Owner pin 2026-08-15: the bar lives in #status next to #theme-toggle,
// always visible. A backend that publishes remaining % is on the bar
// even with no agent running.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.PlanUsage = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Window names, mirroring internal/planusage so the cockpit never has to
  // map two vocabularies.
  const WINDOW_SESSION = 'session';
  const WINDOW_WEEKLY = 'weekly';
  const STATUS_AVAILABLE = 'available';
  const STATUS_UNAVAILABLE = 'unavailable';

  // Steady-state poll is slow: a plan window is hours long. GET
  // /api/plan-usage long-polls while the first batch is still pending, so
  // the "waiting" paint is usually replaced when that request returns.
  // PLAN_POLL_PENDING_MS is only the retry gap after a long-poll times out
  // still pending — not a busy-poll for the first reading itself.
  const PLAN_POLL_MS = 60000;
  const PLAN_POLL_PENDING_MS = 5000;

  function planPollMs(view) {
    if (view && view.pending) return PLAN_POLL_PENDING_MS;
    return PLAN_POLL_MS;
  }

  // Remaining fractions at which a chip changes colour when there is no
  // time signal to pace against. A subscription window is hours long, so
  // these are "start thinking" and "stop starting new work", not alarms.
  const LOW_PERCENT = 15;
  const CRITICAL_PERCENT = 5;

  // 🎯T390.1 pace: burn = (used+λ)/(elapsed+λ). No elapsed cutoff
  // (🎯T390.1.6.2) — λ eases early-window extremes without a hard
  // return-ok. "A lot" is 1.5× the expected spend rate (discussable).
  const PACE_WARMUP_PERCENT = 5; // served document only; colour ignores it
  const PACE_AHEAD_RATIO = 1.0;
  const PACE_HOT_RATIO = 1.5;

  // 🎯T390.1.6.1 early-window damping: burn = (used+λ)/(elapsed+λ).
  // Raw 9% used at 5.6% elapsed is burn 1.6 and painted a barely-started
  // week red. λ pulls small samples toward the neutral 1.0 without
  // moving mid-window readings across the vertices (80/50 damps to 1.55,
  // still hot; λ must stay below 10 or it stops being). Same formula
  // and λ as Go WeeklyBandOf, served in the thresholds document.
  const PACE_DAMP_LAMBDA = 5;

  // 🎯T390.1.1 weekly waste (owner pick C). Same 15 as remaining-low;
  // discussable. Continuation = leftover if pace does not change.
  // Locked = leftover even at a 1.5× sprint. Weekly only.
  const PACE_UNDER_WASTE = 15;
  const PACE_LOCKED_WASTE = 15;

  // 🎯T390.1.6: daemon-owned vertices. applyThresholds overwrites these
  // from GET /api/plan-usage/thresholds (once). Defaults match Go
  // DefaultThresholds so hermetic tests stay independent of a fetch.
  let aheadRatio = PACE_AHEAD_RATIO;
  let hotRatio = PACE_HOT_RATIO;
  let underWaste = PACE_UNDER_WASTE;
  let lockedWaste = PACE_LOCKED_WASTE;
  let lowRemaining = LOW_PERCENT;
  let criticalRemaining = CRITICAL_PERCENT;
  let dampLambda = PACE_DAMP_LAMBDA;

  function applyThresholds(doc) {
    if (!doc || typeof doc !== 'object') return;
    if (typeof doc.ahead_ratio === 'number') aheadRatio = doc.ahead_ratio;
    if (typeof doc.hot_ratio === 'number') hotRatio = doc.hot_ratio;
    if (typeof doc.under_waste_percent === 'number') underWaste = doc.under_waste_percent;
    if (typeof doc.locked_waste_percent === 'number') lockedWaste = doc.locked_waste_percent;
    if (typeof doc.low_remaining_percent === 'number') lowRemaining = doc.low_remaining_percent;
    if (typeof doc.critical_remaining_percent === 'number') criticalRemaining = doc.critical_remaining_percent;
    if (typeof doc.damp_lambda_percent === 'number') dampLambda = doc.damp_lambda_percent;
  }

  const PACE_OK = 'ok';
  const PACE_AHEAD = 'ahead';
  const PACE_HOT = 'hot';
  const PACE_UNDER = 'under';
  const PACE_LOCKED = 'locked';

  const CLASS_CRITICAL = 'plan-crit';
  const CLASS_LOW = 'plan-low';
  const CLASS_STALE = 'plan-stale';
  const CLASS_UNAVAIL = 'plan-unavail';
  const CLASS_AHEAD = 'plan-ahead';
  const CLASS_HOT = 'plan-hot';
  const CLASS_UNDER = 'plan-under';
  const CLASS_LOCKED = 'plan-locked';
  const CLASS_EXHAUSTED = 'plan-exhausted';

  const MS_PER_SECOND = 1000;
  const SECONDS_PER_MINUTE = 60;
  const MINUTES_PER_HOUR = 60;
  const HOURS_PER_DAY = 24;
  const SESSION_LIMIT_SECONDS = 5 * 60 * 60;
  const WEEKLY_LIMIT_SECONDS = 7 * 24 * 60 * 60;

  const SEP_CHIP = ' · ';

  // Compact provider / window labels for the header text / hover.
  const PROVIDER_ABBREV = {
    claude: 'cl',
    codex: 'cx',
    grok: 'gk',
    bedrock: 'bd',
    cursor: 'cu'
  };
  const WINDOW_ABBREV = {
    session: 's',
    weekly: 'w'
  };
  const PROVIDER_RANK = {
    claude: 0,
    codex: 1,
    grok: 2,
    bedrock: 3,
    cursor: 4
  };

  // Same company map as model_prefix.js — claude and bedrock share the
  // Anthropic splat; grok wears the Grok mark; codex wears OpenAI;
  // cursor wears the Cursor mark.
  const PROVIDER_COMPANY = {
    claude: 'anthropic',
    anthropic: 'anthropic',
    bedrock: 'anthropic',
    grok: 'xai',
    xai: 'xai',
    codex: 'openai',
    openai: 'openai',
    cursor: 'cursor'
  };

  /**
   * humanDuration renders a span of seconds the way the owner would say it:
   * "2d 4h", "1h37m", "12m", "now". Deliberately coarse — a rollover three
   * hours out does not become more useful with seconds on it.
   *
   * @param {number} seconds
   * @returns {string}
   */
  function humanDuration(seconds) {
    const s = Math.max(0, Math.floor(seconds));
    if (s < SECONDS_PER_MINUTE) return 'now';
    const totalMinutes = Math.floor(s / SECONDS_PER_MINUTE);
    const minutes = totalMinutes % MINUTES_PER_HOUR;
    const totalHours = Math.floor(totalMinutes / MINUTES_PER_HOUR);
    const hours = totalHours % HOURS_PER_DAY;
    const days = Math.floor(totalHours / HOURS_PER_DAY);
    if (days > 0) return days + 'd' + (hours > 0 ? ' ' + hours + 'h' : '');
    if (totalHours > 0) return totalHours + 'h' + (minutes > 0 ? pad2(minutes) + 'm' : '');
    return totalMinutes + 'm';
  }

  function pad2(n) { return n < 10 ? '0' + n : String(n); }

  function clampPercent(v) {
    if (typeof v !== 'number' || !isFinite(v)) return null;
    if (v < 0) return 0;
    if (v > 100) return 100;
    return v;
  }

  /**
   * percentText renders a published percentage. Returns null — never '0%',
   * never '' — when nothing was published, so a caller cannot accidentally
   * put an invented number on the screen by treating the absence as falsy.
   *
   * @param {number|null|undefined} v
   * @returns {?string}
   */
  function percentText(v) {
    if (typeof v !== 'number' || !isFinite(v)) return null;
    return Math.round(v) + '%';
  }

  /**
   * providerAbbrev is the two-letter header token: cl / cx / gk / bd.
   * Unknown providers fall back to the first two letters, never a number.
   */
  function providerAbbrev(provider) {
    const p = String(provider || '').toLowerCase();
    if (PROVIDER_ABBREV[p]) return PROVIDER_ABBREV[p];
    if (p.length >= 2) return p.slice(0, 2);
    return p || '?';
  }

  /**
   * windowAbbrev is the one-letter window token: s / w.
   */
  function windowAbbrev(name) {
    const n = String(name || '').toLowerCase();
    if (WINDOW_ABBREV[n]) return WINDOW_ABBREV[n];
    if (n) return n.charAt(0);
    return '?';
  }

  function providerRank(provider) {
    const p = String(provider || '').toLowerCase();
    return Object.prototype.hasOwnProperty.call(PROVIDER_RANK, p) ? PROVIDER_RANK[p] : 50;
  }

  function providerCompany(provider) {
    const p = String(provider || '').toLowerCase();
    return PROVIDER_COMPANY[p] || '';
  }

  /**
   * limitSecondsFor is the window length used to place the time triangle.
   * A published limit_window_seconds wins; session/weekly fall back to the
   * product defaults (5h / 7d). Anything else without a published length
   * returns null — no invented triangle.
   */
  function limitSecondsFor(w) {
    if (w && typeof w.limit_window_seconds === 'number' && isFinite(w.limit_window_seconds) && w.limit_window_seconds > 0) {
      return w.limit_window_seconds;
    }
    const n = String((w && w.name) || '').toLowerCase();
    if (n === WINDOW_SESSION) return SESSION_LIMIT_SECONDS;
    if (n === WINDOW_WEEKLY) return WEEKLY_LIMIT_SECONDS;
    return null;
  }

  /**
   * weeklyWaste is the two C quantities, as percents of the weekly pool.
   *
   *   continuation  leftover if used/elapsed does not change
   *   locked        leftover even if the rest of the week runs at 1.5×
   *
   * Nulls when the inputs cannot support the arithmetic — never invented.
   */
  function weeklyWaste(usedPercent, remainingPercent, remainingTimePercent) {
    if (typeof remainingTimePercent !== 'number' || !isFinite(remainingTimePercent)) {
      return { continuation: null, locked: null };
    }
    const rem = (typeof remainingPercent === 'number' && isFinite(remainingPercent))
      ? remainingPercent : null;
    const used = (typeof usedPercent === 'number' && isFinite(usedPercent))
      ? usedPercent
      : (rem !== null ? 100 - rem : null);
    const elapsed = 100 - remainingTimePercent;
    let continuation = null;
    if (used !== null && elapsed > 0) {
      continuation = Math.max(0, 100 - (used / elapsed) * 100);
    }
    const locked = rem === null ? null : Math.max(0, rem - hotRatio * remainingTimePercent);
    return { continuation: continuation, locked: locked };
  }

  /**
   * classifyPace compares token spend to elapsed time.
   *
   *   ok      on pace (or session under-spend — sessions do not waste)
   *   ahead   damped burn > 1                          → orange
   *   hot     damped burn > 1.5, or remaining is 0     → red
   *   under   weekly continuation waste ≥ 15%          → blue
   *   locked  weekly locked waste ≥ 15%                → purple
   *
   * Damped burn = (used+λ)/(elapsed+λ) (🎯T390.1.6.1) — early-window
   * samples lean toward 1.0. No elapsed cutoff (🎯T390.1.6.2): 26%
   * gone with 95% of the week left is hot, not forced green.
   * Waste arithmetic stays raw.
   * No time signal → empty string (caller falls back to remaining-low).
   * windowName is required for under/locked; anything other than weekly
   * keeps the overspend colours only (🎯T390.1.1).
   */
  function classifyPace(usedPercent, remainingPercent, remainingTimePercent, windowName) {
    if (typeof remainingPercent === 'number' && remainingPercent <= 0) return PACE_HOT;
    if (typeof remainingTimePercent !== 'number' || !isFinite(remainingTimePercent)) return '';
    const used = (typeof usedPercent === 'number' && isFinite(usedPercent))
      ? usedPercent
      : (typeof remainingPercent === 'number' ? 100 - remainingPercent : null);
    if (used === null) return '';
    const elapsed = 100 - remainingTimePercent;
    const lambda = dampLambda < 0 ? 0 : dampLambda;
    const burn = (used + lambda) / (elapsed + lambda);
    if (burn > hotRatio) return PACE_HOT;
    if (burn > aheadRatio) return PACE_AHEAD;
    const weekly = String(windowName || '').toLowerCase() === WINDOW_WEEKLY;
    if (weekly) {
      const w = weeklyWaste(used, remainingPercent, remainingTimePercent);
      if (w.locked !== null && w.locked >= lockedWaste) return PACE_LOCKED;
      if (w.continuation !== null && w.continuation >= underWaste) return PACE_UNDER;
    }
    return PACE_OK;
  }

  function paceClassName(pace) {
    if (pace === PACE_HOT) return CLASS_HOT;
    if (pace === PACE_AHEAD) return CLASS_AHEAD;
    if (pace === PACE_LOCKED) return CLASS_LOCKED;
    if (pace === PACE_UNDER) return CLASS_UNDER;
    return '';
  }

  function hotterPace(current, next) {
    const rank = {};
    rank[PACE_HOT] = 4;
    rank[PACE_AHEAD] = 3;
    rank[PACE_LOCKED] = 2;
    rank[PACE_UNDER] = 1;
    const a = rank[current] || 0;
    const b = rank[next] || 0;
    return b > a ? next : current;
  }

  function isRockBottomRemaining(remaining) {
    return typeof remaining === 'number' && isFinite(remaining) && remaining <= 0;
  }

  function windowClassName(w, stale) {
    const parts = [];
    const paceOrRem = (w && w.pace) ? w.paceClass : chipClassForRemaining(w && w.remainingPercent, stale);
    if (paceOrRem) parts.push(paceOrRem);
    if (isRockBottomRemaining(w && w.remainingPercent)) parts.push(CLASS_EXHAUSTED);
    return parts.join(' ');
  }

  function chipClassForRemaining(remaining, stale) {
    if (typeof remaining === 'number' && remaining <= criticalRemaining) return CLASS_CRITICAL;
    if (typeof remaining === 'number' && remaining <= lowRemaining) return CLASS_LOW;
    if (stale) return CLASS_STALE;
    return '';
  }

  /**
   * formatWindow turns one published allowance window into its rendered
   * parts. resets_at is optional even on an available window: a provider may
   * publish a percentage without a rollover, and half an answer is still an
   * answer. A missing rollover means no triangle — never an invented position.
   */
  function formatWindow(w, nowMs) {
    const remaining = (w && typeof w.remaining_percent === 'number') ? w.remaining_percent : null;
    const used = (w && typeof w.used_percent === 'number') ? w.used_percent : null;
    const out = {
      name: (w && w.name) || '',
      remainingPercent: remaining,
      usedPercent: used,
      remainingText: percentText(remaining),
      usedText: percentText(used),
      resetsAt: (w && w.resets_at) || null,
      rollsInText: null,
      rollsAtText: null,
      limitWindowSeconds: null,
      remainingTimePercent: null,
      continuationWaste: null,
      lockedWaste: null,
      pace: '',
      paceClass: ''
    };
    const limitSec = limitSecondsFor(w);
    if (limitSec) out.limitWindowSeconds = limitSec;
    if (out.resetsAt) {
      const at = Date.parse(out.resetsAt);
      if (!isNaN(at)) {
        out.rollsInText = humanDuration((at - nowMs) / MS_PER_SECOND);
        out.rollsAtText = new Date(at).toLocaleString();
        if (limitSec) {
          out.remainingTimePercent = clampPercent(100 * ((at - nowMs) / MS_PER_SECOND) / limitSec);
        }
      }
    }
    if (String(out.name || '').toLowerCase() === WINDOW_WEEKLY && out.remainingTimePercent !== null) {
      const waste = weeklyWaste(out.usedPercent, out.remainingPercent, out.remainingTimePercent);
      out.continuationWaste = waste.continuation;
      out.lockedWaste = waste.locked;
    }
    out.pace = classifyPace(out.usedPercent, out.remainingPercent, out.remainingTimePercent, out.name);
    out.paceClass = paceClassName(out.pace);
    return out;
  }

  function pickWindow(windows, name) {
    for (let i = 0; i < windows.length; i++) {
      if (String(windows[i].name || '').toLowerCase() === name) return windows[i];
    }
    return null;
  }

  /**
   * showOnBar: Bedrock is off the bar unless a fleet agent is running on
   * it. AWS publishes no subscription remaining, and a permanent
   * unavailable chip is noise the owner does not recognise as a backend.
   * Every other publisher — including Grok when the billing surface is
   * down — stays on the bar.
   */
  function isExhaustedReason(reason) {
    const s = String(reason || '').toLowerCase();
    if (!s) return false;
    return s.indexOf('429') >= 0 ||
      s.indexOf('rate_limit') >= 0 ||
      s.indexOf('rate-limit') >= 0 ||
      s.indexOf('rate limited') >= 0;
  }

  function exhaustedZeroWindows() {
    return [
      { name: WINDOW_SESSION, remaining_percent: 0, used_percent: 100 },
      { name: WINDOW_WEEKLY, remaining_percent: 0, used_percent: 100 }
    ];
  }

  function showOnBar(row) {
    if (row.provider === 'bedrock' && !row.available && !row.running) return false;
    return true;
  }

  /**
   * formatBackend renders one provider's row and the compact chips the
   * header paints. A backend is available only when it says so AND
   * published at least one window. Anything else renders the word
   * "unavailable" and the reason the provider gave.
   */
  function formatBackend(b, nowMs) {
    const provider = String((b && b.provider) || '').toLowerCase();
    const abbrev = providerAbbrev(provider);
    const company = providerCompany(provider);
    let windowsIn = Array.isArray(b && b.windows) ? b.windows : [];
    let available = !!(b && b.status === STATUS_AVAILABLE && windowsIn.length > 0);
    // 🎯T390.1.3: Claude's usage API answers 429 rate_limit when the
    // allowance is gone. That is exhausted (0% left), not "unpublished".
    // Painting it as a bare icon looks like a failed render.
    if (!available && isExhaustedReason(b && b.reason) && windowsIn.length === 0) {
      available = true;
      windowsIn = exhaustedZeroWindows();
    }
    const stale = !!(b && b.stale);
    const ageSeconds = (b && typeof b.age_seconds === 'number') ? b.age_seconds : null;

    const row = {
      provider: provider,
      abbrev: abbrev,
      company: company,
      status: available ? STATUS_AVAILABLE : STATUS_UNAVAILABLE,
      available: available,
      reason: (b && b.reason) || '',
      planType: (b && b.plan_type) || '',
      fleetAgents: (b && typeof b.fleet_agents === 'number') ? b.fleet_agents : 0,
      running: !!(b && b.fleet_agents > 0),
      stale: stale,
      ageSeconds: ageSeconds,
      ageText: ageSeconds === null ? null : humanDuration(ageSeconds),
      windows: [],
      chips: [],
      lowestRemaining: null,
      hottestPace: '',
      className: '',
      text: ''
    };

    if (!available) {
      row.text = abbrev + ' ' + STATUS_UNAVAILABLE;
      row.detail = row.reason || 'no plan-remaining published';
      row.className = CLASS_UNAVAIL;
      row.chips.push({
        key: abbrev,
        provider: provider,
        providerAbbrev: abbrev,
        company: company,
        window: null,
        windowAbbrev: null,
        remainingPercent: null,
        remainingText: null,
        remainingTimePercent: null,
        pace: '',
        text: row.text,
        available: false,
        stale: stale,
        className: CLASS_UNAVAIL
      });
      return row;
    }

    // Session and weekly first and in that order — they are the two windows
    // clause 1 names — then anything else the provider published.
    const ordered = [];
    const session = pickWindow(windowsIn, WINDOW_SESSION);
    const weekly = pickWindow(windowsIn, WINDOW_WEEKLY);
    if (session) ordered.push(session);
    if (weekly) ordered.push(weekly);
    for (let i = 0; i < windowsIn.length; i++) {
      if (windowsIn[i] !== session && windowsIn[i] !== weekly) ordered.push(windowsIn[i]);
    }

    const parts = [];
    for (let i = 0; i < ordered.length; i++) {
      const w = formatWindow(ordered[i], nowMs);
      row.windows.push(w);
      if (w.remainingText === null) continue;
      if (row.lowestRemaining === null || w.remainingPercent < row.lowestRemaining) {
        row.lowestRemaining = w.remainingPercent;
      }
      row.hottestPace = hotterPace(row.hottestPace, w.pace);
      const wAbbrev = windowAbbrev(w.name);
      const key = abbrev + '/' + wAbbrev;
      let chipText = key + ' ' + w.remainingText;
      if (stale && row.ageText) chipText += ' stale';
      // Remaining-low only when there is no pace reading. An on-pace
      // weekly at 13% left is green, not amber — that leftover is the
      // fair remainder, not a waste or an exhaustion risk.
      const chipClass = w.pace ? w.paceClass : chipClassForRemaining(w.remainingPercent, stale);
      const chip = {
        key: key,
        provider: provider,
        providerAbbrev: abbrev,
        company: company,
        window: w.name,
        windowAbbrev: wAbbrev,
        remainingPercent: w.remainingPercent,
        remainingText: w.remainingText,
        remainingTimePercent: w.remainingTimePercent,
        usedPercent: w.usedPercent,
        pace: w.pace,
        text: chipText,
        available: true,
        stale: stale,
        className: chipClass
      };
      row.chips.push(chip);
      parts.push(chipText);
    }

    row.text = parts.join(SEP_CHIP);
    if (row.hottestPace === PACE_HOT) {
      row.className = CLASS_HOT;
    } else if (row.hottestPace === PACE_AHEAD) {
      row.className = CLASS_AHEAD;
    } else if (row.hottestPace === PACE_LOCKED) {
      row.className = CLASS_LOCKED;
    } else if (row.hottestPace === PACE_UNDER) {
      row.className = CLASS_UNDER;
    } else if (row.lowestRemaining !== null && row.lowestRemaining <= CRITICAL_PERCENT) {
      row.className = CLASS_CRITICAL;
    } else if (row.lowestRemaining !== null && row.lowestRemaining <= LOW_PERCENT) {
      row.className = CLASS_LOW;
    } else if (stale) {
      row.className = CLASS_STALE;
    }
    return row;
  }

  /**
   * formatPlanUsage turns the GET /api/plan-usage body into the header bar.
   *
   * @param {object|null|undefined} snap - the served payload
   * @param {number} [nowMs] - clock, injectable so the oracle is not timing-dependent
   * @returns {{visible:boolean, text?:string, className?:string, title?:string,
   *            rows?:Array, others?:Array, chips?:Array, groups?:Array, pending?:boolean}}
   */
  function formatPlanUsage(snap, nowMs) {
    const now = (typeof nowMs === 'number') ? nowMs : Date.now();

    // A daemon without the plan-usage wiring is a daemon with nothing to say
    // here; hide rather than accuse it of an outage.
    if (!snap || snap.disabled) return { visible: false, chips: [], rows: [], others: [], groups: [] };

    if (snap.error) {
      return {
        visible: true,
        text: 'plan usage unavailable: ' + snap.error,
        className: CLASS_STALE,
        title: 'GET /api/plan-usage reported a whole-query failure (🎯T390)',
        rows: [],
        others: [],
        chips: [],
        groups: []
      };
    }

    if (snap.pending) {
      return {
        visible: true,
        pending: true,
        text: 'plan usage: waiting for the first reading',
        className: '',
        title: 'The daemon has wired plan usage but no backend has answered yet (🎯T390)',
        rows: [],
        others: [],
        chips: [],
        groups: []
      };
    }

    const backends = Array.isArray(snap.backends) ? snap.backends : [];
    if (backends.length === 0) return { visible: false, chips: [], rows: [], others: [], groups: [] };

    const all = backends.map(function (b) { return formatBackend(b, now); });
    all.sort(function (a, b) { return providerRank(a.provider) - providerRank(b.provider); });

    // Owner pin: every publisher is on the bar, running or not — except
    // idle Bedrock, which can never grow a bar. Unavailable backends that
    // stay on the bar say the word, never a blank or a number.
    const chips = [];
    const groups = [];
    for (let i = 0; i < all.length; i++) {
      if (!showOnBar(all[i])) continue;
      for (let j = 0; j < all[i].chips.length; j++) chips.push(all[i].chips[j]);
      groups.push(groupFromRow(all[i]));
    }

    const text = chips.map(function (c) { return c.text; }).join(SEP_CHIP);

    const classRank = {};
    classRank[CLASS_HOT] = 5;
    classRank[CLASS_CRITICAL] = 5;
    classRank[CLASS_AHEAD] = 4;
    classRank[CLASS_LOW] = 4;
    classRank[CLASS_LOCKED] = 3;
    classRank[CLASS_UNDER] = 2;
    classRank[CLASS_STALE] = 1;
    let className = '';
    for (let i = 0; i < all.length; i++) {
      if (!showOnBar(all[i])) continue;
      if ((classRank[all[i].className] || 0) > (classRank[className] || 0)) {
        className = all[i].className;
      }
    }

    return {
      visible: true,
      text: text,
      className: className,
      title: titleFor(all, []),
      rows: all,
      others: [],
      chips: chips,
      groups: groups
    };
  }

  function groupFromRow(row) {
    const wins = [];
    for (let i = 0; i < row.windows.length; i++) {
      const w = row.windows[i];
      if (w.remainingText === null) continue;
      wins.push({
        name: w.name,
        windowAbbrev: windowAbbrev(w.name),
        remainingPercent: w.remainingPercent,
        remainingTimePercent: w.remainingTimePercent,
        usedPercent: w.usedPercent,
        pace: w.pace,
        className: windowClassName(w, row.stale)
      });
    }
    return {
      provider: row.provider,
      company: row.company,
      abbrev: row.abbrev,
      available: row.available,
      stale: row.stale,
      className: row.available ? (row.stale ? CLASS_STALE : '') : CLASS_UNAVAIL,
      windows: wins
    };
  }

  /**
   * titleFor is the hover detail: the absolute rollover times the bar only
   * has room to give as a fill, why each unavailable backend is unavailable,
   * and the spend-vs-time reading the triangle encodes.
   */
  function titleFor(rows, others) {
    const lines = [];
    const hidden = [];
    for (let i = 0; i < rows.length; i++) {
      if (!showOnBar(rows[i])) {
        hidden.push(rows[i]);
        continue;
      }
      lines.push(describe(rows[i]));
    }
    if (hidden.length) {
      lines.push('');
      lines.push('Not on the bar (no subscription remaining):');
      for (let i = 0; i < hidden.length; i++) lines.push(indent(describe(hidden[i]), '  '));
    }
    if (others.length) {
      lines.push('');
      lines.push('Not currently running:');
      for (let i = 0; i < others.length; i++) lines.push(indent(describe(others[i]), '  '));
    }
    return lines.join('\n');
  }

  function indent(text, pad) {
    return text.split('\n').map(function (line) { return pad + line; }).join('\n');
  }

  function describe(r) {
    const head = r.provider + (r.planType ? ' (' + r.planType + ')' : '') +
      (r.running ? ' — ' + r.fleetAgents + ' agent' + (r.fleetAgents === 1 ? '' : 's') : '');
    if (!r.available) return head + ': unavailable — ' + (r.detail || r.reason || 'no plan-remaining published');
    const bits = [];
    for (let i = 0; i < r.windows.length; i++) {
      const w = r.windows[i];
      if (w.remainingText === null) continue;
      let bit = '  ' + w.name + ': ' + w.remainingText + ' remaining';
      if (w.usedText) bit += ' (' + w.usedText + ' used)';
      if (w.rollsAtText) bit += ', rolls over ' + w.rollsAtText;
      if (w.pace === PACE_HOT) bit += ' — spend far ahead of time';
      else if (w.pace === PACE_AHEAD) bit += ' — spend ahead of time';
      else if (w.pace === PACE_LOCKED) {
        bit += ' — ' + Math.round(w.lockedWaste) + '% of the week already unrecoverable at 1.5×';
      } else if (w.pace === PACE_UNDER) {
        bit += ' — on track to leave ' + Math.round(w.continuationWaste) + '% of the week unused';
      }
      bits.push(bit);
    }
    let out = head + ':\n' + bits.join('\n');
    if (r.stale && r.ageText) {
      out += '\n  reading is ' + r.ageText + ' old — shown stale rather than as current';
    }
    return out;
  }

  function iconHtmlFor(group) {
    if (!group || !group.company) return '';
    let MP = null;
    if (typeof ModelPrefix !== 'undefined') MP = ModelPrefix;
    else if (typeof module === 'object' && module && module.exports) {
      try { MP = require('./model_prefix.js'); } catch (e) { MP = null; }
    }
    if (MP && typeof MP.companyIconHtml === 'function') {
      return MP.companyIconHtml(group.company);
    }
    return '';
  }

  /**
   * paintPlanUsage draws grouped provider boxes into `el`. Never sets
   * display:none — the header slot stays occupied.
   */
  function paintPlanUsage(el, view) {
    if (!el) return;
    const v = view || { visible: false, chips: [], groups: [], text: '' };
    el.className = v.className || '';
    el.title = v.title || '';
    el.style.display = 'flex';
    while (el.firstChild) el.removeChild(el.firstChild);

    if (!v.visible) return;

    const groups = Array.isArray(v.groups) ? v.groups : [];
    if (groups.length === 0) {
      if (v.text) el.appendChild(el.ownerDocument.createTextNode(v.text));
      return;
    }

    const doc = el.ownerDocument;
    for (let i = 0; i < groups.length; i++) {
      el.appendChild(paintGroup(doc, groups[i]));
    }
  }

  function paintGroup(doc, g) {
    const el = doc.createElement('span');
    el.className = 'plan-group' + (g.className ? ' ' + g.className : '');
    el.setAttribute('data-provider', g.provider || '');
    if (g.company) el.setAttribute('data-company', g.company);

    const icon = doc.createElement('span');
    icon.className = 'plan-icon';
    const html = iconHtmlFor(g);
    if (html) {
      icon.innerHTML = html;
    } else {
      icon.textContent = g.abbrev || '';
    }
    el.appendChild(icon);

    if (!g.windows || g.windows.length === 0) {
      return el;
    }

    const box = doc.createElement('span');
    box.className = 'plan-box';
    for (let i = 0; i < g.windows.length; i++) {
      box.appendChild(paintWindow(doc, g.windows[i]));
    }
    el.appendChild(box);
    return el;
  }

  function paintWindow(doc, w) {
    const el = doc.createElement('span');
    el.className = 'plan-win' + (w.className ? ' ' + w.className : '');
    if (w.pace) el.setAttribute('data-pace', w.pace);
    if (w.name) el.setAttribute('data-window', w.name);

    const track = doc.createElement('span');
    track.className = 'plan-track';

    const bar = doc.createElement('span');
    bar.className = 'plan-bar';
    bar.setAttribute('aria-hidden', 'true');
    const fill = doc.createElement('span');
    fill.className = 'plan-bar-fill';
    if (typeof w.remainingPercent === 'number' && isFinite(w.remainingPercent)) {
      const pct = Math.max(0, Math.min(100, w.remainingPercent));
      fill.style.width = pct + '%';
    }
    bar.appendChild(fill);
    track.appendChild(bar);

    if (typeof w.remainingTimePercent === 'number' && isFinite(w.remainingTimePercent)) {
      const tri = doc.createElement('span');
      tri.className = 'plan-tri';
      tri.setAttribute('aria-hidden', 'true');
      tri.style.left = Math.max(0, Math.min(100, w.remainingTimePercent)) + '%';
      track.appendChild(tri);
    }

    el.appendChild(track);

    const lab = doc.createElement('span');
    lab.className = 'plan-win-label';
    lab.textContent = w.windowAbbrev || '';
    el.appendChild(lab);
    return el;
  }

  return {
    formatPlanUsage: formatPlanUsage,
    planPollMs: planPollMs,
    PLAN_POLL_MS: PLAN_POLL_MS,
    PLAN_POLL_PENDING_MS: PLAN_POLL_PENDING_MS,
    formatBackend: formatBackend,
    formatWindow: formatWindow,
    paintPlanUsage: paintPlanUsage,
    humanDuration: humanDuration,
    percentText: percentText,
    providerAbbrev: providerAbbrev,
    providerCompany: providerCompany,
    windowAbbrev: windowAbbrev,
    classifyPace: classifyPace,
    applyThresholds: applyThresholds,
    weeklyWaste: weeklyWaste,
    limitSecondsFor: limitSecondsFor,
    showOnBar: showOnBar,
    isExhaustedReason: isExhaustedReason,
    isRockBottomRemaining: isRockBottomRemaining,
    CLASS_EXHAUSTED: CLASS_EXHAUSTED,
    WINDOW_SESSION: WINDOW_SESSION,
    WINDOW_WEEKLY: WINDOW_WEEKLY,
    STATUS_AVAILABLE: STATUS_AVAILABLE,
    STATUS_UNAVAILABLE: STATUS_UNAVAILABLE,
    LOW_PERCENT: LOW_PERCENT,
    CRITICAL_PERCENT: CRITICAL_PERCENT,
    PACE_WARMUP_PERCENT: PACE_WARMUP_PERCENT,
    PACE_AHEAD_RATIO: PACE_AHEAD_RATIO,
    PACE_HOT_RATIO: PACE_HOT_RATIO,
    PACE_UNDER_WASTE: PACE_UNDER_WASTE,
    PACE_LOCKED_WASTE: PACE_LOCKED_WASTE,
    PACE_OK: PACE_OK,
    PACE_AHEAD: PACE_AHEAD,
    PACE_HOT: PACE_HOT,
    PACE_UNDER: PACE_UNDER,
    PACE_LOCKED: PACE_LOCKED,
    SESSION_LIMIT_SECONDS: SESSION_LIMIT_SECONDS,
    WEEKLY_LIMIT_SECONDS: WEEKLY_LIMIT_SECONDS
  };
}));
