// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T390: the cockpit half of plan usage — how much of each backend's
// subscription allowance is left, and when it rolls over. DOM-free so the
// hermetic Node test drives it with fixture payloads.
//
// The daemon serves the picture at GET /api/plan-usage; this turns it into
// the line the owner reads without asking. One rule shapes every branch
// below: a number that was never published is never rendered as a number.
// A blank or a 0% reads as "you have nothing left", which is a different and
// false statement from "nobody publishes this" — and Grok SuperGrok, the
// fleet's own default backend, is permanently in the second case. So an
// unavailable backend says the word out loud, with the provider's reason,
// and 0% is reserved for a backend that really did publish a zero.

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

  // Remaining fractions at which the line changes colour. A subscription
  // window is hours long, so these are "start thinking" and "stop starting
  // new work", not alarms.
  const LOW_PERCENT = 15;
  const CRITICAL_PERCENT = 5;

  const CLASS_CRITICAL = 'plan-crit';
  const CLASS_LOW = 'plan-low';
  const CLASS_STALE = 'plan-stale';

  const MS_PER_SECOND = 1000;
  const SECONDS_PER_MINUTE = 60;
  const MINUTES_PER_HOUR = 60;
  const HOURS_PER_DAY = 24;

  const SEP_WINDOW = ' · ';
  const SEP_BACKEND = '   ';

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
   * formatWindow turns one published allowance window into its rendered
   * parts. resets_at is optional even on an available window: a provider may
   * publish a percentage without a rollover, and half an answer is still an
   * answer.
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
      rollsAtText: null
    };
    if (out.resetsAt) {
      const at = Date.parse(out.resetsAt);
      if (!isNaN(at)) {
        out.rollsInText = humanDuration((at - nowMs) / MS_PER_SECOND);
        out.rollsAtText = new Date(at).toLocaleString();
      }
    }
    return out;
  }

  function pickWindow(windows, name) {
    for (let i = 0; i < windows.length; i++) {
      if (String(windows[i].name || '').toLowerCase() === name) return windows[i];
    }
    return null;
  }

  /**
   * formatBackend renders one provider's row.
   *
   * The available/unavailable split is the whole point of the target, so it
   * is decided here once: a backend is available only when it says so AND
   * published at least one window. Anything else renders the word
   * "unavailable" and the reason the provider gave.
   */
  function formatBackend(b, nowMs) {
    const provider = String((b && b.provider) || '').toLowerCase();
    const windowsIn = Array.isArray(b && b.windows) ? b.windows : [];
    const available = b && b.status === STATUS_AVAILABLE && windowsIn.length > 0;
    const stale = !!(b && b.stale);
    const ageSeconds = (b && typeof b.age_seconds === 'number') ? b.age_seconds : null;

    const row = {
      provider: provider,
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
      lowestRemaining: null,
      className: '',
      text: ''
    };

    if (!available) {
      // The reason is the provider's own words; without one, say plainly
      // that nothing was published rather than leaving a bare verdict.
      row.text = provider + ' ' + STATUS_UNAVAILABLE;
      row.detail = row.reason || 'no plan-remaining published';
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
      let part = w.remainingText + ' ' + w.name;
      if (w.rollsInText) part += ' (' + w.rollsInText + ')';
      parts.push(part);
    }

    row.text = provider + ' ' + parts.join(SEP_WINDOW);
    if (stale && row.ageText) row.text += SEP_WINDOW + 'stale ' + row.ageText;

    if (row.lowestRemaining !== null && row.lowestRemaining <= CRITICAL_PERCENT) {
      row.className = CLASS_CRITICAL;
    } else if (row.lowestRemaining !== null && row.lowestRemaining <= LOW_PERCENT) {
      row.className = CLASS_LOW;
    } else if (stale) {
      row.className = CLASS_STALE;
    }
    return row;
  }

  /**
   * formatPlanUsage turns the GET /api/plan-usage body into the cockpit line.
   *
   * @param {object|null|undefined} snap - the served payload
   * @param {number} [nowMs] - clock, injectable so the oracle is not timing-dependent
   * @returns {{visible:boolean, text?:string, className?:string, title?:string,
   *            rows?:Array, others?:Array, pending?:boolean}}
   */
  function formatPlanUsage(snap, nowMs) {
    const now = (typeof nowMs === 'number') ? nowMs : Date.now();

    // A daemon without the plan-usage wiring is a daemon with nothing to say
    // here; hide rather than accuse it of an outage.
    if (!snap || snap.disabled) return { visible: false };

    if (snap.error) {
      return {
        visible: true,
        text: 'plan usage unavailable: ' + snap.error,
        className: CLASS_STALE,
        title: 'GET /api/plan-usage reported a whole-query failure (🎯T390)',
        rows: [],
        others: []
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
        others: []
      };
    }

    const backends = Array.isArray(snap.backends) ? snap.backends : [];
    if (backends.length === 0) return { visible: false };

    const all = backends.map(function (b) { return formatBackend(b, now); });

    // Clause 1 scopes the visible line to the backends the fleet is actually
    // running on — that is the allowance being spent right now. The rest are
    // real answers too, so they go in the tooltip rather than being dropped.
    // With nothing running at all the line would be empty and the owner would
    // read that as breakage, so an idle fleet shows everything.
    let rows = all.filter(function (r) { return r.running; });
    let others = all.filter(function (r) { return !r.running; });
    if (rows.length === 0) {
      rows = all;
      others = [];
    }

    const text = rows.map(function (r) { return r.text; }).join(SEP_BACKEND);

    let className = '';
    for (let i = 0; i < rows.length; i++) {
      if (rows[i].className === CLASS_CRITICAL) { className = CLASS_CRITICAL; break; }
      if (rows[i].className === CLASS_LOW) className = CLASS_LOW;
      else if (rows[i].className === CLASS_STALE && className === '') className = CLASS_STALE;
    }

    return {
      visible: true,
      text: text,
      className: className,
      title: titleFor(rows, others),
      rows: rows,
      others: others
    };
  }

  /**
   * titleFor is the hover detail: the absolute rollover times the line only
   * had room to give as "in 1h37m", why each unavailable backend is
   * unavailable, and the backends the fleet is not currently running on.
   */
  function titleFor(rows, others) {
    const lines = [];
    for (let i = 0; i < rows.length; i++) lines.push(describe(rows[i]));
    if (others.length) {
      lines.push('');
      lines.push('Not currently running:');
      for (let i = 0; i < others.length; i++) lines.push('  ' + describe(others[i]));
    }
    return lines.join('\n');
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
      bits.push(bit);
    }
    let out = head + ':\n' + bits.join('\n');
    if (r.stale && r.ageText) {
      out += '\n  reading is ' + r.ageText + ' old — shown stale rather than as current';
    }
    return out;
  }

  return {
    formatPlanUsage: formatPlanUsage,
    formatBackend: formatBackend,
    humanDuration: humanDuration,
    percentText: percentText,
    WINDOW_SESSION: WINDOW_SESSION,
    WINDOW_WEEKLY: WINDOW_WEEKLY,
    STATUS_AVAILABLE: STATUS_AVAILABLE,
    STATUS_UNAVAILABLE: STATUS_UNAVAILABLE,
    LOW_PERCENT: LOW_PERCENT,
    CRITICAL_PERCENT: CRITICAL_PERCENT
  };
}));
